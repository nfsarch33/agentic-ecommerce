// File scope: v3.4.0 EC-4-2 Facebook Shop META Commerce Manager
// HTTP client (Graph API v21).
//
// The client wraps net/http behind the social.FacebookClient port
// consumed by the EC-4-3 channel router + future EC-7-4 status
// propagation. Outbound requests are signed via
// facebook_shop_signing.go (HMAC-SHA256 appsecret_proof on every
// Graph API call); authenticated via the operator-bootstrapped
// long-lived page token from facebook_shop_oauth.go.
//
// Decomposition discipline: every public Client method is a thin
// wrapper that builds a request envelope and delegates to roundTrip.
// roundTrip handles the cross-cutting concerns -- token resolution,
// appsecret_proof signing, rate-limit detection, response parsing
// -- so per-method cyclomatic complexity stays well under 8. The
// method bodies average ~25 LOC. CreateProductBatch chunks under
// MaxFacebookBatchSize but the chunking helper splits out so the
// public method body remains simple.
//
// Resilience pillar:
//   - Implements lifecycle.Closer.
//   - No goroutines (HTTP is request-scoped).
//   - Rate-limited 429 responses surface ErrFacebookRateLimited; the
//     agent layer applies exponential backoff matching the v3.1.0
//     Taobao adapter pattern (BackoffInitial 1s, BackoffMax 30s).
//   - Prometheus metrics emitted via FacebookMetricsHook.
//   - Tenant awareness: every request carries the tenant_id label.
package social

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// FacebookShopConfig wires a FacebookShopClient. Required:
// TokenManager, AppID, AppSecret, CatalogueID, TenantID. Optional:
// HTTPClient, BaseURL, Now, Logger, MetricsHook.
type FacebookShopConfig struct {
	HTTPClient   *http.Client
	TokenManager *FacebookTokenManager
	BaseURL      string
	AppID        string
	AppSecret    []byte
	CatalogueID  string
	TenantID     string
	UserAgent    string
	Now          func() time.Time
	MetricsHook  FacebookMetricsHook
}

// FacebookShopClient is the v3.4.0 EC-4-2 Facebook Shop client. It
// is safe for concurrent use by multiple goroutines (the underlying
// http.Client is). Close is idempotent.
type FacebookShopClient struct {
	cfg     FacebookShopConfig
	logger  *slog.Logger
	baseURL string

	mu     sync.Mutex
	closed bool
}

// NewFacebookShopClient constructs a FacebookShopClient.
func NewFacebookShopClient(logger *slog.Logger, cfg FacebookShopConfig) (*FacebookShopClient, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := validateFacebookConfig(cfg); err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultFacebookBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultFacebookTimeout}
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (compatible; agentic-ecommerce/3.4.0; facebook-shop)"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &FacebookShopClient{
		cfg:     cfg,
		logger:  logger,
		baseURL: baseURL,
	}, nil
}

func validateFacebookConfig(cfg FacebookShopConfig) error {
	if cfg.TokenManager == nil {
		return fmt.Errorf("%w: TokenManager required", ErrFacebookUnconfigured)
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return fmt.Errorf("%w: AppID required (set ECOMMERCE_FACEBOOK_APP_ID)", ErrFacebookUnconfigured)
	}
	if err := ensureFacebookSecret(cfg.AppSecret); err != nil {
		return fmt.Errorf("%w: AppSecret invalid (set ECOMMERCE_FACEBOOK_APP_SECRET)", err)
	}
	if strings.TrimSpace(cfg.CatalogueID) == "" {
		return fmt.Errorf("%w: CatalogueID required", ErrFacebookUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrFacebookUnconfigured)
	}
	return nil
}

// Close marks the client closed. Implements lifecycle.Closer.
func (c *FacebookShopClient) Close(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// CreateProduct implements FacebookClient.CreateProduct.
func (c *FacebookShopClient) CreateProduct(ctx context.Context, payload FacebookProductPayload) (FacebookProductCreated, error) {
	if err := c.guard(); err != nil {
		return FacebookProductCreated{}, err
	}
	if payload.RetailerID == "" {
		return FacebookProductCreated{}, fmt.Errorf("%w: RetailerID required", ErrFacebookUnconfigured)
	}
	body, err := json.Marshal(facebookProductWire(payload))
	if err != nil {
		return FacebookProductCreated{}, fmt.Errorf("%w: encode create payload: %v", ErrFacebookInvalidResponse, err)
	}
	resp, err := c.roundTrip(ctx, facebookEnvelope{
		Method:   http.MethodPost,
		Path:     "/" + url.PathEscape(c.cfg.CatalogueID) + "/products",
		Body:     body,
		TenantID: requireTenantID(payload.TenantID, c.cfg.TenantID),
		Endpoint: "catalog.products.create",
	})
	if err != nil {
		return FacebookProductCreated{}, err
	}
	var created facebookProductCreatedWire
	if err := decodeFacebookJSON(resp, &created); err != nil {
		return FacebookProductCreated{}, err
	}
	if created.ID == "" {
		return FacebookProductCreated{}, fmt.Errorf("%w: missing id in create response", ErrFacebookInvalidResponse)
	}
	return FacebookProductCreated{
		RemoteID:   created.ID,
		RetailerID: payload.RetailerID,
		OccurredAt: c.cfg.Now().UTC(),
	}, nil
}

// CreateProductBatch implements FacebookClient.CreateProductBatch.
// Operator passes up to 100 payloads; under the hood the client
// chunks at MaxFacebookBatchSize so the wire request honours META's
// per-batch contract.
func (c *FacebookShopClient) CreateProductBatch(ctx context.Context, payloads []FacebookProductPayload) ([]FacebookBatchResult, error) {
	if err := c.guard(); err != nil {
		return nil, err
	}
	if len(payloads) == 0 {
		return nil, fmt.Errorf("%w: payloads required", ErrFacebookUnconfigured)
	}
	if len(payloads) > 100 {
		return nil, fmt.Errorf("%w: %d payloads > 100 acceptance cap", ErrFacebookBatchTooLarge, len(payloads))
	}
	results := make([]FacebookBatchResult, 0, len(payloads))
	for _, chunk := range chunkPayloads(payloads, MaxFacebookBatchSize) {
		out, err := c.createProductBatchChunk(ctx, chunk)
		if err != nil {
			return results, err
		}
		results = append(results, out...)
	}
	return results, nil
}

// createProductBatchChunk posts a single META `/batch` envelope of
// up to MaxFacebookBatchSize items. The wire shape uses the META
// `batch=[{method,relative_url,body}, ...]` convention so a single
// HTTP call handles many product writes.
func (c *FacebookShopClient) createProductBatchChunk(ctx context.Context, chunk []FacebookProductPayload) ([]FacebookBatchResult, error) {
	body, err := json.Marshal(buildBatchEnvelope(c.cfg.CatalogueID, chunk))
	if err != nil {
		return nil, fmt.Errorf("%w: encode batch payload: %v", ErrFacebookInvalidResponse, err)
	}
	resp, err := c.roundTrip(ctx, facebookEnvelope{
		Method:   http.MethodPost,
		Path:     "/",
		Body:     body,
		TenantID: c.cfg.TenantID,
		Endpoint: "catalog.products.batch",
	})
	if err != nil {
		return nil, err
	}
	return parseBatchResponse(resp, chunk)
}

// SyncInventory implements FacebookClient.SyncInventory.
func (c *FacebookShopClient) SyncInventory(ctx context.Context, update FacebookInventoryUpdate) error {
	if err := c.guard(); err != nil {
		return err
	}
	if update.RetailerID == "" {
		return fmt.Errorf("%w: RetailerID required", ErrFacebookUnconfigured)
	}
	body, err := json.Marshal(map[string]any{
		"retailer_id":     update.RetailerID,
		"inventory":       update.StockUnits,
		"availability":    update.Availability,
		"idempotency_key": update.IdempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("%w: encode inventory payload: %v", ErrFacebookInvalidResponse, err)
	}
	_, err = c.roundTrip(ctx, facebookEnvelope{
		Method:   http.MethodPost,
		Path:     "/" + url.PathEscape(c.cfg.CatalogueID) + "/items_batch",
		Body:     body,
		TenantID: requireTenantID(update.TenantID, c.cfg.TenantID),
		Endpoint: "catalog.inventory.sync",
	})
	return err
}

// PushOrderStatus implements FacebookClient.PushOrderStatus.
func (c *FacebookShopClient) PushOrderStatus(ctx context.Context, push FacebookOrderStatusPush) error {
	if err := c.guard(); err != nil {
		return err
	}
	if push.OrderID == "" {
		return fmt.Errorf("%w: OrderID required", ErrFacebookUnconfigured)
	}
	body, err := json.Marshal(map[string]any{
		"order_id":        push.OrderID,
		"status":          push.Status,
		"tracking_number": push.TrackingNumber,
		"tracking_url":    push.TrackingURL,
	})
	if err != nil {
		return fmt.Errorf("%w: encode status payload: %v", ErrFacebookInvalidResponse, err)
	}
	_, err = c.roundTrip(ctx, facebookEnvelope{
		Method:   http.MethodPost,
		Path:     "/" + url.PathEscape(push.OrderID) + "/shipments",
		Body:     body,
		TenantID: requireTenantID(push.TenantID, c.cfg.TenantID),
		Endpoint: "commerce.orders.status",
	})
	return err
}

// Exchange satisfies FacebookTokenExchanger.Exchange. Posts the
// short-lived token + app credentials to the Graph API to receive
// a long-lived page token.
func (c *FacebookShopClient) Exchange(ctx context.Context, req FacebookOAuthBootstrapRequest) (FacebookToken, error) {
	q := url.Values{}
	q.Set("grant_type", "fb_exchange_token")
	q.Set("client_id", c.cfg.AppID)
	q.Set("client_secret", string(c.cfg.AppSecret))
	q.Set("fb_exchange_token", req.ShortLivedToken)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/oauth/access_token?"+q.Encode(), nil)
	if err != nil {
		return FacebookToken{}, fmt.Errorf("facebook: build oauth request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return FacebookToken{}, fmt.Errorf("facebook: oauth http: %w", err)
	}
	defer resp.Body.Close()
	return c.decodeExchangeResponse(req, resp)
}

// decodeExchangeResponse splits the response decoding out so
// Exchange stays low-cyclomatic.
func (c *FacebookShopClient) decodeExchangeResponse(req FacebookOAuthBootstrapRequest, resp *http.Response) (FacebookToken, error) {
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return FacebookToken{}, fmt.Errorf("%w: status %d", ErrFacebookAuthFailed, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return FacebookToken{}, fmt.Errorf("%w: oauth status %d: %s", ErrFacebookInvalidResponse, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload facebookOAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return FacebookToken{}, fmt.Errorf("%w: decode oauth response: %v", ErrFacebookInvalidResponse, err)
	}
	expires := c.cfg.Now().Add(time.Duration(payload.ExpiresInSec) * time.Second)
	return FacebookToken{
		TenantID:    req.TenantID,
		PageID:      req.PageID,
		AccessToken: payload.AccessToken,
		ExpiresAt:   expires,
		Scopes:      req.Scopes,
	}, nil
}

// facebookEnvelope packages everything roundTrip needs so each
// public method body stays low-cyclomatic.
type facebookEnvelope struct {
	Method   string
	Path     string
	Body     []byte
	TenantID string
	Endpoint string
}

// roundTrip is the cross-cutting concern hub: it resolves the
// access token, signs the request via appsecret_proof, fires the
// HTTP call, surfaces rate-limit + auth + error categories, and
// emits metrics.
func (c *FacebookShopClient) roundTrip(ctx context.Context, env facebookEnvelope) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tok, err := c.cfg.TokenManager.AccessToken(ctx, env.TenantID)
	if err != nil {
		c.recordAPI(env, "auth_failed", 0)
		return nil, fmt.Errorf("%w: %v", ErrFacebookAuthFailed, err)
	}
	httpReq, err := c.buildSignedRequest(ctx, env, tok)
	if err != nil {
		c.recordAPI(env, "build_failed", 0)
		return nil, err
	}
	start := c.cfg.Now()
	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		c.recordAPI(env, "transport_error", c.cfg.Now().Sub(start).Seconds())
		return nil, fmt.Errorf("facebook: %s %s: %w", env.Method, env.Path, err)
	}
	defer resp.Body.Close()
	dur := c.cfg.Now().Sub(start).Seconds()
	body, status, err := readAndCategoriseFacebook(resp)
	c.recordAPI(env, status, dur)
	return body, err
}

// buildSignedRequest builds the http.Request and attaches the
// appsecret_proof query parameter. Split out so roundTrip stays
// linear.
func (c *FacebookShopClient) buildSignedRequest(ctx context.Context, env facebookEnvelope, tok FacebookToken) (*http.Request, error) {
	proof, err := ComputeFacebookAppSecretProof(c.cfg.AppSecret, tok.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("facebook: appsecret_proof: %w", err)
	}
	q := url.Values{}
	q.Set("access_token", tok.AccessToken)
	q.Set("appsecret_proof", proof)
	fullURL := c.baseURL + env.Path + "?" + q.Encode()
	var bodyReader io.Reader
	if len(env.Body) > 0 {
		bodyReader = bytes.NewReader(env.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, env.Method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("facebook: build %s %s: %w", env.Method, env.Path, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	httpReq.Header.Set("X-Tenant", env.TenantID)
	return httpReq, nil
}

// readAndCategoriseFacebook reads the body and maps the status code
// to a typed sentinel. Status returned is a Prometheus-friendly
// label.
func readAndCategoriseFacebook(resp *http.Response) ([]byte, string, error) {
	switch {
	case resp.StatusCode == http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return nil, "read_error", fmt.Errorf("%w: read body: %v", ErrFacebookInvalidResponse, err)
		}
		return body, "ok", nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, "rate_limited", fmt.Errorf("%w: status %d", ErrFacebookRateLimited, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, "auth_failed", fmt.Errorf("%w: status %d", ErrFacebookAuthFailed, resp.StatusCode)
	default:
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, "graph_error", fmt.Errorf("%w: status %d body=%s", ErrFacebookGraphAPIError, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

func decodeFacebookJSON(raw []byte, out any) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: empty body", ErrFacebookInvalidResponse)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: decode body: %v", ErrFacebookInvalidResponse, err)
	}
	return nil
}

func (c *FacebookShopClient) guard() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrFacebookClosed
	}
	return nil
}

func (c *FacebookShopClient) recordAPI(env facebookEnvelope, status string, durationSeconds float64) {
	if c.cfg.MetricsHook == nil {
		return
	}
	c.cfg.MetricsHook.RecordAPICall(env.TenantID, env.Endpoint, status, durationSeconds)
}

// chunkPayloads splits a slice into runs of `size`.
func chunkPayloads(in []FacebookProductPayload, size int) [][]FacebookProductPayload {
	if size <= 0 {
		return [][]FacebookProductPayload{in}
	}
	out := make([][]FacebookProductPayload, 0, (len(in)+size-1)/size)
	for i := 0; i < len(in); i += size {
		end := i + size
		if end > len(in) {
			end = len(in)
		}
		out = append(out, in[i:end])
	}
	return out
}

// --- wire shapes ------------------------------------------------------------

// facebookProductCreatedWire mirrors the Graph API create envelope.
type facebookProductCreatedWire struct {
	ID string `json:"id"`
}

// facebookOAuthResponse mirrors the Graph API token exchange
// envelope.
type facebookOAuthResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresInSec int    `json:"expires_in"`
}

// facebookProductWire builds the JSON shape the Graph API expects
// for /<catalog>/products.
func facebookProductWire(p FacebookProductPayload) map[string]any {
	out := map[string]any{
		"retailer_id":  p.RetailerID,
		"name":         p.Name,
		"description":  p.Description,
		"category":     p.CategoryID,
		"brand":        p.BrandName,
		"price":        formatFacebookPrice(p.PriceCents, p.Currency),
		"currency":     p.Currency,
		"availability": p.Availability,
		"condition":    p.Condition,
		"image_url":    p.ImageURL,
		"url":          p.URL,
	}
	if len(p.AdditionalImages) > 0 {
		out["additional_image_urls"] = p.AdditionalImages
	}
	if p.GTIN != "" {
		out["gtin"] = p.GTIN
	}
	if p.MPN != "" {
		out["mpn"] = p.MPN
	}
	return out
}

// formatFacebookPrice formats price as "<units>.<cents> <currency>"
// per META's expected price string. Uses integer math; safe for any
// AUD/CNY/USD amount under int max.
func formatFacebookPrice(cents int, currency string) string {
	if currency == "" {
		currency = "AUD"
	}
	units := cents / 100
	frac := cents % 100
	if frac < 0 {
		frac = -frac
	}
	return fmt.Sprintf("%d.%02d %s", units, frac, currency)
}

// buildBatchEnvelope constructs the META `/batch` body for a chunk
// of product payloads. The shape is
// {"batch":[{"method":"POST","relative_url":"<cat>/products","body":"json"},...]}.
func buildBatchEnvelope(catalogID string, chunk []FacebookProductPayload) map[string]any {
	relURL := url.PathEscape(catalogID) + "/products"
	items := make([]map[string]any, 0, len(chunk))
	for _, p := range chunk {
		body, _ := json.Marshal(facebookProductWire(p))
		items = append(items, map[string]any{
			"method":       "POST",
			"relative_url": relURL,
			"body":         string(body),
		})
	}
	return map[string]any{
		"batch": items,
	}
}

// parseBatchResponse decodes the META `/batch` response envelope
// (an ordered array matching the request items) and aligns each
// per-item outcome with the input retailer id.
func parseBatchResponse(raw []byte, chunk []FacebookProductPayload) ([]FacebookBatchResult, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty batch body", ErrFacebookInvalidResponse)
	}
	var items []facebookBatchItemWire
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%w: decode batch body: %v", ErrFacebookInvalidResponse, err)
	}
	if len(items) != len(chunk) {
		return nil, fmt.Errorf("%w: batch length mismatch (got %d want %d)", ErrFacebookInvalidResponse, len(items), len(chunk))
	}
	out := make([]FacebookBatchResult, 0, len(items))
	for i, item := range items {
		out = append(out, item.toResult(chunk[i].RetailerID))
	}
	return out, nil
}

// facebookBatchItemWire mirrors a single META /batch sub-response
// envelope: {"code":200,"body":"<json string>"} on success or with
// a structured error on failure.
type facebookBatchItemWire struct {
	Code int    `json:"code"`
	Body string `json:"body"`
}

// toResult maps a single batch sub-response into the canonical
// FacebookBatchResult.
func (item facebookBatchItemWire) toResult(retailerID string) FacebookBatchResult {
	if item.Code != http.StatusOK {
		return FacebookBatchResult{
			RetailerID: retailerID,
			Error:      fmt.Errorf("%w: status %d body=%s", ErrFacebookGraphAPIError, item.Code, strings.TrimSpace(item.Body)),
		}
	}
	var inner facebookProductCreatedWire
	if err := json.Unmarshal([]byte(item.Body), &inner); err != nil {
		return FacebookBatchResult{
			RetailerID: retailerID,
			Error:      fmt.Errorf("%w: decode batch sub-body: %v", ErrFacebookInvalidResponse, err),
		}
	}
	return FacebookBatchResult{
		RetailerID: retailerID,
		RemoteID:   inner.ID,
	}
}
