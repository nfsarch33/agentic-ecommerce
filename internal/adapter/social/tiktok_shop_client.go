// File scope: v3.3.0 EC-3-1 TikTok Shop seller API HTTP client.
//
// The client wraps net/http behind the social.Client port consumed
// by the EC-3-2 listing agent + EC-3-4 inventory sync. Outbound
// requests are signed via tiktok_shop_signing.go (HMAC-SHA256 over
// the canonical "<timestamp>\n<path>\n<sha256-hex(body)>" form);
// authenticated via the operator-bootstrapped token from
// tiktok_shop_oauth.go.
//
// Decomposition discipline: every public Client method is a thin
// wrapper that builds a request envelope and delegates to roundTrip.
// roundTrip handles the cross-cutting concerns -- token resolution,
// signing, rate-limit detection, response parsing -- so per-method
// cyclomatic complexity stays well under 10. The method bodies
// average ~25 LOC.
//
// Resilience pillar:
//   - Implements lifecycle.Closer.
//   - No goroutines (HTTP is request-scoped).
//   - Rate-limited 429 responses surface ErrTikTokRateLimited; the
//     agent layer applies exponential backoff matching the v3.1.0
//     Taobao adapter pattern (BackoffInitial 1s, BackoffMax 30s).
//   - Prometheus metrics emitted via observability/tiktok_metrics.go.
//   - Tenant awareness: every request carries the tenant_id label.
package social

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TikTokShopConfig wires a TikTokShopClient. Required: HTTPClient
// (or accept the default), TokenManager, ClientID, ClientSecret,
// TenantID. Optional: BaseURL, Now, Logger.
type TikTokShopConfig struct {
	HTTPClient   *http.Client
	TokenManager *TokenManager
	BaseURL      string
	ClientID     string
	ClientSecret []byte
	TenantID     string
	UserAgent    string
	Now          func() time.Time
	MetricsHook  TikTokMetricsHook
}

// TikTokShopClient is the v3.3.0 EC-3-1 TikTok Shop client. It is
// safe for concurrent use by multiple goroutines (the underlying
// http.Client is). Close is idempotent.
type TikTokShopClient struct {
	cfg     TikTokShopConfig
	logger  *slog.Logger
	baseURL string

	mu     sync.Mutex
	closed bool
}

// NewTikTokShopClient constructs a TikTokShopClient.
func NewTikTokShopClient(logger *slog.Logger, cfg TikTokShopConfig) (*TikTokShopClient, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := validateClientConfig(cfg); err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultTikTokBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultTikTokTimeout}
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (compatible; agentic-ecommerce/3.3.0; tiktok-shop)"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &TikTokShopClient{
		cfg:     cfg,
		logger:  logger,
		baseURL: baseURL,
	}, nil
}

func validateClientConfig(cfg TikTokShopConfig) error {
	if cfg.TokenManager == nil {
		return fmt.Errorf("%w: TokenManager required", ErrTikTokUnconfigured)
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return fmt.Errorf("%w: ClientID required (set ECOMMERCE_TIKTOK_CLIENT_ID)", ErrTikTokUnconfigured)
	}
	if err := ensureSecret(cfg.ClientSecret); err != nil {
		return fmt.Errorf("%w: ClientSecret invalid (set ECOMMERCE_TIKTOK_CLIENT_SECRET)", err)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrTikTokUnconfigured)
	}
	return nil
}

// Close marks the client closed. Implements lifecycle.Closer.
func (c *TikTokShopClient) Close(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// ListProducts implements Client.ListProducts.
func (c *TikTokShopClient) ListProducts(ctx context.Context, req TikTokListProductsRequest) (TikTokProductPage, error) {
	if err := c.guard(); err != nil {
		return TikTokProductPage{}, err
	}
	q := url.Values{}
	if req.PageSize > 0 {
		q.Set("page_size", fmt.Sprintf("%d", req.PageSize))
	}
	if req.Page != "" {
		q.Set("page_token", req.Page)
	}
	path := "/api/products/search"
	resp, err := c.roundTrip(ctx, requestEnvelope{
		Method:   http.MethodGet,
		Path:     path,
		Query:    q,
		TenantID: requireTenantID(req.TenantID, c.cfg.TenantID),
		Endpoint: "products.list",
	})
	if err != nil {
		return TikTokProductPage{}, err
	}
	var page tiktokProductsResponse
	if err := decodeJSON(resp, &page); err != nil {
		return TikTokProductPage{}, err
	}
	return page.toPage(), nil
}

// CreateProduct implements Client.CreateProduct.
func (c *TikTokShopClient) CreateProduct(ctx context.Context, payload TikTokProductPayload) (string, error) {
	if err := c.guard(); err != nil {
		return "", err
	}
	body, err := json.Marshal(productPayloadWire(payload))
	if err != nil {
		return "", fmt.Errorf("%w: encode create payload: %v", ErrTikTokInvalidResponse, err)
	}
	resp, err := c.roundTrip(ctx, requestEnvelope{
		Method:   http.MethodPost,
		Path:     "/api/products",
		Body:     body,
		TenantID: requireTenantID(payload.TenantID, c.cfg.TenantID),
		Endpoint: "products.create",
	})
	if err != nil {
		return "", err
	}
	var created tiktokProductCreatedResponse
	if err := decodeJSON(resp, &created); err != nil {
		return "", err
	}
	if created.ProductID == "" {
		return "", fmt.Errorf("%w: missing product_id in create response", ErrTikTokInvalidResponse)
	}
	return created.ProductID, nil
}

// UpdateProduct implements Client.UpdateProduct.
func (c *TikTokShopClient) UpdateProduct(ctx context.Context, remoteID string, payload TikTokProductPayload) error {
	if err := c.guard(); err != nil {
		return err
	}
	if remoteID == "" {
		return fmt.Errorf("%w: remoteID required", ErrTikTokUnconfigured)
	}
	body, err := json.Marshal(productPayloadWire(payload))
	if err != nil {
		return fmt.Errorf("%w: encode update payload: %v", ErrTikTokInvalidResponse, err)
	}
	_, err = c.roundTrip(ctx, requestEnvelope{
		Method:   http.MethodPut,
		Path:     "/api/products/" + url.PathEscape(remoteID),
		Body:     body,
		TenantID: requireTenantID(payload.TenantID, c.cfg.TenantID),
		Endpoint: "products.update",
	})
	return err
}

// DeleteProduct implements Client.DeleteProduct. 404 is treated as
// idempotent success.
func (c *TikTokShopClient) DeleteProduct(ctx context.Context, remoteID string) error {
	if err := c.guard(); err != nil {
		return err
	}
	if remoteID == "" {
		return fmt.Errorf("%w: remoteID required", ErrTikTokUnconfigured)
	}
	_, err := c.roundTrip(ctx, requestEnvelope{
		Method:   http.MethodDelete,
		Path:     "/api/products/" + url.PathEscape(remoteID),
		TenantID: c.cfg.TenantID,
		Endpoint: "products.delete",
		Allow404: true,
	})
	return err
}

// SyncInventory implements Client.SyncInventory.
func (c *TikTokShopClient) SyncInventory(ctx context.Context, update TikTokInventoryUpdate) error {
	if err := c.guard(); err != nil {
		return err
	}
	if update.SKU == "" {
		return fmt.Errorf("%w: SKU required", ErrTikTokUnconfigured)
	}
	body, err := json.Marshal(map[string]any{
		"sku":             update.SKU,
		"product_id":      update.ProductID,
		"delta":           update.Delta,
		"order_id":        update.OrderID,
		"idempotency_key": update.IdempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("%w: encode inventory payload: %v", ErrTikTokInvalidResponse, err)
	}
	_, err = c.roundTrip(ctx, requestEnvelope{
		Method:   http.MethodPost,
		Path:     "/api/inventory/sync",
		Body:     body,
		TenantID: requireTenantID(update.TenantID, c.cfg.TenantID),
		Endpoint: "inventory.sync",
	})
	return err
}

// Exchange satisfies TokenExchanger.Exchange. Posts the bootstrap
// authorization_code to TikTok's OAuth endpoint with the PKCE
// verifier; returns the parsed TikTokToken.
func (c *TikTokShopClient) Exchange(ctx context.Context, req OAuthBootstrapRequest) (TikTokToken, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     c.cfg.ClientID,
		"client_secret": string(c.cfg.ClientSecret),
		"code":          req.AuthorizationCode,
		"code_verifier": req.Verifier,
		"tenant_id":     req.TenantID,
		"shop_id":       req.ShopID,
		"scope":         req.Scope,
	})
	if err != nil {
		return TikTokToken{}, fmt.Errorf("%w: encode oauth payload: %v", ErrTikTokInvalidResponse, err)
	}
	return c.exchangeOAuth(ctx, body, req.TenantID, req.ShopID)
}

// Refresh satisfies TokenExchanger.Refresh.
func (c *TikTokShopClient) Refresh(ctx context.Context, refreshToken, tenantID string) (TikTokToken, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     c.cfg.ClientID,
		"client_secret": string(c.cfg.ClientSecret),
		"refresh_token": refreshToken,
		"tenant_id":     tenantID,
	})
	if err != nil {
		return TikTokToken{}, fmt.Errorf("%w: encode refresh payload: %v", ErrTikTokInvalidResponse, err)
	}
	return c.exchangeOAuth(ctx, body, tenantID, "")
}

func (c *TikTokShopClient) exchangeOAuth(ctx context.Context, body []byte, tenantID, shopID string) (TikTokToken, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/oauth/token", bytes.NewReader(body))
	if err != nil {
		return TikTokToken{}, fmt.Errorf("tiktok: build oauth request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return TikTokToken{}, fmt.Errorf("tiktok: oauth http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return TikTokToken{}, fmt.Errorf("%w: status %d", ErrTikTokAuthFailed, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return TikTokToken{}, fmt.Errorf("%w: oauth status %d: %s", ErrTikTokInvalidResponse, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload tiktokOAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return TikTokToken{}, fmt.Errorf("%w: decode oauth response: %v", ErrTikTokInvalidResponse, err)
	}
	expires := c.cfg.Now().Add(time.Duration(payload.ExpiresInSec) * time.Second)
	return TikTokToken{
		TenantID:     tenantID,
		ShopID:       shopID,
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		ExpiresAt:    expires,
		Scope:        payload.Scope,
	}, nil
}

// requestEnvelope packages everything roundTrip needs so each
// public method body stays low-cyclomatic.
type requestEnvelope struct {
	Method   string
	Path     string
	Query    url.Values
	Body     []byte
	TenantID string
	Endpoint string
	Allow404 bool
}

// roundTrip is the cross-cutting concern hub: it resolves the
// access token, signs the request, fires the HTTP call, surfaces
// rate-limit + auth + error categories, and emits metrics.
func (c *TikTokShopClient) roundTrip(ctx context.Context, env requestEnvelope) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tok, err := c.cfg.TokenManager.AccessToken(ctx, env.TenantID)
	if err != nil {
		c.recordMetric(env, "auth_failed", 0)
		return nil, fmt.Errorf("%w: %v", ErrTikTokAuthFailed, err)
	}
	httpReq, err := c.buildSignedRequest(ctx, env, tok)
	if err != nil {
		c.recordMetric(env, "build_failed", 0)
		return nil, err
	}
	start := c.cfg.Now()
	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		c.recordMetric(env, "transport_error", c.cfg.Now().Sub(start).Seconds())
		return nil, fmt.Errorf("tiktok: %s %s: %w", env.Method, env.Path, err)
	}
	defer resp.Body.Close()
	dur := c.cfg.Now().Sub(start).Seconds()
	body, status, err := readAndCategorise(resp, env.Allow404)
	c.recordMetric(env, status, dur)
	return body, err
}

// buildSignedRequest builds the http.Request and signs it. Split
// out so roundTrip stays linear.
func (c *TikTokShopClient) buildSignedRequest(ctx context.Context, env requestEnvelope, tok TikTokToken) (*http.Request, error) {
	fullURL := c.baseURL + env.Path
	if len(env.Query) > 0 {
		fullURL += "?" + env.Query.Encode()
	}
	var bodyReader io.Reader
	if len(env.Body) > 0 {
		bodyReader = bytes.NewReader(env.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, env.Method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("tiktok: build %s %s: %w", env.Method, env.Path, err)
	}
	timestamp := c.cfg.Now().Unix()
	signature, err := ComputeTikTokSignature(TikTokSignRequest{
		Secret:    c.cfg.ClientSecret,
		Timestamp: timestamp,
		Path:      canonicalRequestPath(env.Path),
		Body:      env.Body,
	})
	if err != nil {
		return nil, fmt.Errorf("tiktok: sign request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	httpReq.Header.Set("X-Tts-Access-Token", tok.AccessToken)
	httpReq.Header.Set("X-Tts-Timestamp", fmt.Sprintf("%d", timestamp))
	httpReq.Header.Set("X-Tts-Sign", signature)
	httpReq.Header.Set("X-Tts-Tenant", env.TenantID)
	return httpReq, nil
}

// readAndCategorise reads the body and maps the status code to a
// typed sentinel. Status returned is a Prometheus-friendly label.
func readAndCategorise(resp *http.Response, allow404 bool) ([]byte, string, error) {
	switch {
	case resp.StatusCode == http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return nil, "read_error", fmt.Errorf("%w: read body: %v", ErrTikTokInvalidResponse, err)
		}
		return body, "ok", nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, "rate_limited", fmt.Errorf("%w: status %d", ErrTikTokRateLimited, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, "auth_failed", fmt.Errorf("%w: status %d", ErrTikTokAuthFailed, resp.StatusCode)
	case allow404 && resp.StatusCode == http.StatusNotFound:
		return nil, "not_found_ok", nil
	default:
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, "invalid_response", fmt.Errorf("%w: status %d body=%s", ErrTikTokInvalidResponse, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

func decodeJSON(raw []byte, out any) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: empty body", ErrTikTokInvalidResponse)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: decode body: %v", ErrTikTokInvalidResponse, err)
	}
	return nil
}

func (c *TikTokShopClient) guard() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrTikTokClosed
	}
	return nil
}

func (c *TikTokShopClient) recordMetric(env requestEnvelope, status string, durationSeconds float64) {
	if c.cfg.MetricsHook == nil {
		return
	}
	c.cfg.MetricsHook.RecordAPICall(env.TenantID, env.Endpoint, status, durationSeconds)
	if errors.Is(errors.New(status), nil) {
		// no-op; status is an arbitrary label, not an error
	}
}

func requireTenantID(req, fallback string) string {
	if req != "" {
		return req
	}
	return fallback
}

// --- wire shapes ------------------------------------------------------------

// tiktokProductsResponse mirrors the shop API's product list
// envelope.
type tiktokProductsResponse struct {
	Code int `json:"code"`
	Data struct {
		Products  []tiktokProductWire `json:"products"`
		NextPage  string              `json:"next_page,omitempty"`
		TotalSeen int                 `json:"total_seen"`
	} `json:"data"`
}

type tiktokProductWire struct {
	ProductID  string `json:"product_id"`
	TenantID   string `json:"tenant_id"`
	Title      string `json:"title"`
	CategoryID string `json:"category_id"`
	Status     string `json:"status"`
	UpdatedAt  string `json:"updated_at"`
	Stock      int    `json:"stock"`
	PriceCents int    `json:"price_cents"`
	Currency   string `json:"currency"`
}

func (r tiktokProductsResponse) toPage() TikTokProductPage {
	out := make([]TikTokProduct, 0, len(r.Data.Products))
	for _, p := range r.Data.Products {
		ts, _ := time.Parse(time.RFC3339, p.UpdatedAt)
		out = append(out, TikTokProduct{
			ID:         p.ProductID,
			TenantID:   p.TenantID,
			Title:      p.Title,
			CategoryID: p.CategoryID,
			Status:     p.Status,
			UpdatedAt:  ts.UTC(),
			Stock:      p.Stock,
			PriceCents: p.PriceCents,
			Currency:   p.Currency,
		})
	}
	return TikTokProductPage{Products: out, NextPage: r.Data.NextPage, TotalSeen: r.Data.TotalSeen}
}

// tiktokProductCreatedResponse mirrors the shop API's create
// envelope.
type tiktokProductCreatedResponse struct {
	Code int `json:"code"`
	Data struct {
		ProductID string `json:"product_id"`
	} `json:"data"`
	ProductID string `json:"product_id,omitempty"` // some shop APIs flatten; accept both
}

// productPayloadWire builds the JSON shape TikTok Shop expects for
// create + update.
func productPayloadWire(p TikTokProductPayload) map[string]any {
	return map[string]any{
		"tenant_id":         p.TenantID,
		"external_id":       p.ExternalID,
		"title":             p.Title,
		"description":       p.Description,
		"category_id":       p.CategoryID,
		"brand":             p.BrandName,
		"price_cents":       p.PriceCents,
		"currency":          p.Currency,
		"stock":             p.StockUnits,
		"shipping_template": p.ShippingTemplate,
		"images":            p.Images,
		"video_sku_url":     p.VideoSKUURL,
		"seller_sku":        p.SellerSKU,
		"warehouse_id":      p.WarehouseID,
	}
}

// tiktokOAuthResponse mirrors the shop API's token exchange
// envelope.
type tiktokOAuthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresInSec int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// UnmarshalJSON pulls the product id out of either flattened or
// nested response shapes.
func (r *tiktokProductCreatedResponse) UnmarshalJSON(raw []byte) error {
	type alias tiktokProductCreatedResponse
	var a alias
	if err := json.Unmarshal(raw, &a); err != nil {
		return err
	}
	*r = tiktokProductCreatedResponse(a)
	if r.Data.ProductID == "" && r.ProductID != "" {
		r.Data.ProductID = r.ProductID
	}
	if r.ProductID == "" {
		r.ProductID = r.Data.ProductID
	}
	return nil
}
