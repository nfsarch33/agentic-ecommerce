package china

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultTaobaoBaseURL is the production Taobao Open Platform base
// URL. The shape this adapter consumes is the JSON-over-HTTPS API
// (not the consumer-facing HTML pages).
const DefaultTaobaoBaseURL = "https://eco.taobao.com"

// DefaultTaobaoTimeout is the per-request HTTP timeout.
const DefaultTaobaoTimeout = 15 * time.Second

// DefaultTaobaoBackoffInitial is the first retry delay after a 429.
const DefaultTaobaoBackoffInitial = 100 * time.Millisecond

// DefaultTaobaoBackoffMax is the maximum retry delay; the backoff
// grows exponentially (initial * 2^n) until clamped here.
const DefaultTaobaoBackoffMax = 5 * time.Second

// DefaultTaobaoMaxRetries is the maximum number of 429 retries before
// the error is propagated.
const DefaultTaobaoMaxRetries = 3

// ConfigTaobao controls TaobaoClient wiring.
type ConfigTaobao struct {
	BaseURL        string
	SessionCookie  string
	HTTPClient     *http.Client
	BackoffInitial time.Duration
	BackoffMax     time.Duration
	MaxRetries     int
	UserAgent      string

	// Sleep is injectable so tests do not actually wait the full
	// backoff windows. Defaults to time.Sleep.
	Sleep func(d time.Duration)
}

// TaobaoClient is the v3.1.0 EC-1-2 Taobao adapter implementing
// Client. Exponential backoff on 429 follows the spec.
type TaobaoClient struct {
	cfg    ConfigTaobao
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewTaobaoClient constructs a TaobaoClient.
func NewTaobaoClient(logger *slog.Logger, cfg ConfigTaobao) (*TaobaoClient, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultTaobaoBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultTaobaoTimeout}
	}
	if cfg.BackoffInitial <= 0 {
		cfg.BackoffInitial = DefaultTaobaoBackoffInitial
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = DefaultTaobaoBackoffMax
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultTaobaoMaxRetries
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (compatible; agentic-ecommerce/3.1.0)"
	}
	if cfg.Sleep == nil {
		cfg.Sleep = time.Sleep
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.SessionCookie == "" {
		return nil, fmt.Errorf("%w: taobao session cookie missing (set ECOMMERCE_TAOBAO_SESSION_COOKIE)", ErrAdapterUnconfigured)
	}
	return &TaobaoClient{cfg: cfg, logger: logger}, nil
}

// Source returns the adapter's Source identifier.
func (c *TaobaoClient) Source() Source { return SourceTaobao }

// Close marks the client closed.
func (c *TaobaoClient) Close(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// Search performs a keyword search against Taobao.
func (c *TaobaoClient) Search(ctx context.Context, req SearchRequest) ([]Product, error) {
	if strings.TrimSpace(req.Keyword) == "" {
		return nil, ErrInvalidQuery
	}
	url := fmt.Sprintf("%s/search?q=%s&max=%d", strings.TrimRight(c.cfg.BaseURL, "/"), urlEncode(req.Keyword), maxResultsOrDefault(req.MaxResults))
	body, err := c.doWithBackoff(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("taobao: search: %w", err)
	}
	defer body.Close()
	var resp searchTaobaoResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("taobao: decode: %w", err)
	}
	out := make([]Product, 0, len(resp.Products))
	for _, p := range resp.Products {
		mapped, err := p.toProduct(time.Now().UTC())
		if err != nil && !errors.Is(err, ErrTaobaoCategoryUnknown) {
			return nil, err
		}
		out = append(out, mapped)
	}
	return out, nil
}

// ProductDetail fetches a single product with its review summary.
func (c *TaobaoClient) ProductDetail(ctx context.Context, req ProductDetailRequest) (Product, error) {
	if req.ExternalID == "" {
		return Product{}, ErrInvalidQuery
	}
	url := fmt.Sprintf("%s/detail?id=%s", strings.TrimRight(c.cfg.BaseURL, "/"), urlEncode(req.ExternalID))
	body, err := c.doWithBackoff(ctx, url)
	if err != nil {
		return Product{}, fmt.Errorf("taobao: detail: %w", err)
	}
	defer body.Close()
	var detail productTaobaoJSON
	if err := json.NewDecoder(body).Decode(&detail); err != nil {
		return Product{}, fmt.Errorf("taobao: decode: %w", err)
	}
	return detail.toProduct(time.Now().UTC())
}

// doWithBackoff executes the GET with exponential backoff on 429.
// Returns the response body the caller must close, or an error.
func (c *TaobaoClient) doWithBackoff(ctx context.Context, url string) (io.ReadCloser, error) {
	backoff := c.cfg.BackoffInitial
	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		body, status, err := c.do(ctx, url)
		if err != nil {
			return nil, err
		}
		if status == http.StatusOK {
			return body, nil
		}
		// Drain and close so the connection can be reused.
		_, _ = io.Copy(io.Discard, body)
		_ = body.Close()
		if status != http.StatusTooManyRequests {
			return nil, fmt.Errorf("taobao: status %d", status)
		}
		lastErr = fmt.Errorf("%w: 429", Err1688RateLimited) // shared sentinel name not pretty -- keep code simple by reusing the 1688 sentinel? No -- keep separate.
		_ = lastErr                                         // placate linter even though we replace below
		// Use a Taobao-specific framing.
		lastErr = fmt.Errorf("taobao: 429 attempt %d: %w", attempt, errTaobaoRateLimited)
		if attempt == c.cfg.MaxRetries {
			break
		}
		// Sleep with backoff, honouring ctx.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		c.cfg.Sleep(backoff)
		backoff *= 2
		if backoff > c.cfg.BackoffMax {
			backoff = c.cfg.BackoffMax
		}
	}
	return nil, lastErr
}

// do performs a single GET. The caller is responsible for closing
// the returned body.
func (c *TaobaoClient) do(ctx context.Context, url string) (io.ReadCloser, int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, 0, fmt.Errorf("taobao: client closed")
	}
	c.mu.Unlock()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("taobao: build request: %w", err)
	}
	httpReq.Header.Set("Cookie", c.cfg.SessionCookie)
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	httpReq.Header.Set("Accept", "application/json")
	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("taobao: do: %w", err)
	}
	return resp.Body, resp.StatusCode, nil
}

// errTaobaoRateLimited is the package-internal sentinel raised by
// doWithBackoff when 429s exhaust the retry budget.
var errTaobaoRateLimited = errors.New("taobao: rate limited after retries")

// ErrTaobaoRateLimited is the public sentinel callers should
// errors.Is against.
var ErrTaobaoRateLimited = errTaobaoRateLimited

// taobaoCategoryMap is the v3.1.0 baseline mapping covering the top-
// level categories the EC-1-2 spec requires (>= top-10 categories).
var taobaoCategoryMap = map[string]string{
	"3c":          "electronics",
	"audio":       "electronics",
	"home":        "home_goods",
	"kitchen":     "kitchen",
	"fashion":     "fashion",
	"beauty":      "beauty",
	"fitness":     "fitness",
	"outdoor":     "outdoor",
	"toys":        "toys",
	"pets":        "pets",
	"baby":        "baby",
	"books":       "books",
	"groceries":   "groceries",
	"accessories": "accessories",
}

type searchTaobaoResponse struct {
	Products []productTaobaoJSON `json:"products"`
}

type productTaobaoJSON struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	NativeCategory string  `json:"native_category"`
	PriceCNY       float64 `json:"price_cny"`
	MOQ            int     `json:"moq"`
	LeadTimeDays   int     `json:"lead_time_days"`
	SellerID       string  `json:"seller_id"`
	SellerName     string  `json:"seller_name"`
	SellerRating   float64 `json:"seller_rating"`
	SellerLevel    string  `json:"seller_level"`
	ReviewCount    int     `json:"review_count"`
	MonthlySales   int     `json:"monthly_sales"`
	URL            string  `json:"url"`
}

func (p productTaobaoJSON) toProduct(now time.Time) (Product, error) {
	mapped, ok := taobaoCategoryMap[strings.ToLower(strings.TrimSpace(p.NativeCategory))]
	var category string
	var err error
	if ok {
		category = mapped
	} else {
		category = "unknown"
		err = fmt.Errorf("%w: native=%q", ErrTaobaoCategoryUnknown, p.NativeCategory)
	}
	return Product{
		ExternalID:       p.ID,
		Source:           SourceTaobao,
		Title:            p.Title,
		Category:         category,
		PriceCNYCents:    int(p.PriceCNY * 100),
		MOQ:              p.MOQ,
		LeadTimeDays:     p.LeadTimeDays,
		SupplierID:       p.SellerID,
		SupplierName:     p.SellerName,
		SupplierRating:   p.SellerRating,
		SupplierVerified: strings.EqualFold(p.SellerLevel, "tmall_gold") || strings.EqualFold(p.SellerLevel, "gold"),
		ReviewCount:      p.ReviewCount,
		MonthlySalesUnit: p.MonthlySales,
		URL:              p.URL,
		FetchedAt:        now,
	}, err
}

// SupportedCategories returns the canonical list of mapped Taobao
// categories. Useful for tests + ops surfaces ("which top-N
// categories are wired?").
func SupportedCategories() []string {
	out := make([]string, 0, len(taobaoCategoryMap))
	seen := map[string]struct{}{}
	for _, v := range taobaoCategoryMap {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
