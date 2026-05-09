package china

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Default1688BaseURL is the production 1688 search-API base URL. The
// real 1688 search HTML endpoint is rendered server-side; the
// adapter shape here matches the JSON-shaped backend that 1688's
// search UI calls (this is the same path our future operator-driven
// cassettes will record).
const Default1688BaseURL = "https://search.1688.com"

// Default1688RateInterval is 1 request every 2 seconds per the EC-1-1
// spec. The token bucket has size 1; bursts are not allowed.
const Default1688RateInterval = 2 * time.Second

// Default1688Timeout is the per-request HTTP timeout.
const Default1688Timeout = 15 * time.Second

// Config1688 controls Client1688 wiring.
type Config1688 struct {
	BaseURL       string
	SessionCookie string
	HTTPClient    *http.Client
	RateInterval  time.Duration
	UserAgent     string
}

// Client1688 is the v3.1.0 EC-1-1 1688 adapter. It implements the
// Client interface using a JSON-over-HTTP backend; the live URL can
// be overridden at construction so unit tests substitute httptest
// servers without monkey-patching.
type Client1688 struct {
	cfg    Config1688
	logger *slog.Logger

	mu         sync.Mutex
	closed     bool
	lastReqAt  time.Time
	rateClosed chan struct{}
}

// New1688Client constructs a Client1688 from cfg. SessionCookie is
// MANDATORY in production but tests may pass a sentinel (the test
// httptest server does not enforce it).
func New1688Client(logger *slog.Logger, cfg Config1688) (*Client1688, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = Default1688BaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: Default1688Timeout}
	}
	if cfg.RateInterval <= 0 {
		cfg.RateInterval = Default1688RateInterval
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (compatible; agentic-ecommerce/3.1.0)"
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.SessionCookie == "" {
		return nil, fmt.Errorf("%w: 1688 session cookie missing (set ECOMMERCE_1688_SESSION_COOKIE)", ErrAdapterUnconfigured)
	}
	return &Client1688{
		cfg:        cfg,
		logger:     logger,
		rateClosed: make(chan struct{}),
	}, nil
}

// Source returns the adapter's Source identifier.
func (c *Client1688) Source() Source { return Source1688 }

// Close marks the adapter closed. Implements lifecycle.Closer.
func (c *Client1688) Close(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.rateClosed)
	return nil
}

// Search performs a keyword search against 1688 and returns the
// parsed product list. Rate-limited per RateInterval; honours ctx.
func (c *Client1688) Search(ctx context.Context, req SearchRequest) ([]Product, error) {
	if strings.TrimSpace(req.Keyword) == "" {
		return nil, ErrInvalidQuery
	}
	if err := c.waitRate(ctx); err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/search?keyword=%s&max=%d", strings.TrimRight(c.cfg.BaseURL, "/"), urlEncode(req.Keyword), maxResultsOrDefault(req.MaxResults))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("1688: build request: %w", err)
	}
	httpReq.Header.Set("Cookie", c.cfg.SessionCookie)
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	httpReq.Header.Set("Accept", "application/json")
	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("1688: search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: status %d", Err1688RateLimited, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("1688: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload search1688Response
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("1688: decode: %w", err)
	}
	return payload.toProducts(time.Now().UTC()), nil
}

// ProductDetail fetches a single 1688 product by ExternalID.
func (c *Client1688) ProductDetail(ctx context.Context, req ProductDetailRequest) (Product, error) {
	if req.ExternalID == "" {
		return Product{}, ErrInvalidQuery
	}
	if err := c.waitRate(ctx); err != nil {
		return Product{}, err
	}
	url := fmt.Sprintf("%s/detail?id=%s", strings.TrimRight(c.cfg.BaseURL, "/"), urlEncode(req.ExternalID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return Product{}, fmt.Errorf("1688: build detail request: %w", err)
	}
	httpReq.Header.Set("Cookie", c.cfg.SessionCookie)
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return Product{}, fmt.Errorf("1688: detail: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return Product{}, fmt.Errorf("%w: status %d", Err1688RateLimited, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return Product{}, fmt.Errorf("1688: detail status %d", resp.StatusCode)
	}
	var detail product1688JSON
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return Product{}, fmt.Errorf("1688: detail decode: %w", err)
	}
	return detail.toProduct(time.Now().UTC()), nil
}

// waitRate blocks until at least RateInterval has elapsed since the
// last request, or ctx expires, or the client is closed.
func (c *Client1688) waitRate(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("1688: client closed")
	}
	wait := c.cfg.RateInterval - time.Since(c.lastReqAt)
	if wait <= 0 || c.lastReqAt.IsZero() {
		c.lastReqAt = time.Now()
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.rateClosed:
		return fmt.Errorf("1688: client closed")
	case <-timer.C:
		c.mu.Lock()
		c.lastReqAt = time.Now()
		c.mu.Unlock()
		return nil
	}
}

// search1688Response is the JSON shape returned by the search endpoint.
type search1688Response struct {
	Products []product1688JSON `json:"products"`
}

type product1688JSON struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	Category         string  `json:"category"`
	SubCategory      string  `json:"sub_category"`
	PriceCNY         float64 `json:"price_cny"`
	MOQ              int     `json:"moq"`
	LeadTimeDays     int     `json:"lead_time_days"`
	SupplierID       string  `json:"supplier_id"`
	SupplierName     string  `json:"supplier_name"`
	SupplierRating   float64 `json:"supplier_rating"`
	SupplierVerified bool    `json:"supplier_verified_gold"`
	ReviewCount      int     `json:"review_count"`
	MonthlySales     int     `json:"monthly_sales"`
	URL              string  `json:"url"`
}

func (p product1688JSON) toProduct(now time.Time) Product {
	return Product{
		ExternalID:       p.ID,
		Source:           Source1688,
		Title:            p.Title,
		Category:         p.Category,
		SubCategory:      p.SubCategory,
		PriceCNYCents:    int(p.PriceCNY * 100),
		MOQ:              p.MOQ,
		LeadTimeDays:     p.LeadTimeDays,
		SupplierID:       p.SupplierID,
		SupplierName:     p.SupplierName,
		SupplierRating:   p.SupplierRating,
		SupplierVerified: p.SupplierVerified,
		ReviewCount:      p.ReviewCount,
		MonthlySalesUnit: p.MonthlySales,
		URL:              p.URL,
		FetchedAt:        now,
	}
}

func (r search1688Response) toProducts(now time.Time) []Product {
	out := make([]Product, 0, len(r.Products))
	for _, p := range r.Products {
		out = append(out, p.toProduct(now))
	}
	return out
}

func maxResultsOrDefault(n int) int {
	if n <= 0 {
		return 50
	}
	if n > 200 {
		return 200
	}
	return n
}

// urlEncode is a stdlib-only minimal URL escape for our keyword
// parameter. Iterates UTF-8 BYTES (not runes) so unicode keywords
// encode as %E8%80%B3 etc. The encoding mirrors what net/url's
// QueryEscape produces for the unreserved set + space-as-plus, but
// keeps the output predictable for httptest cassette matching.
func urlEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'),
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
