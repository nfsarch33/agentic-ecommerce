// File scope: v4.6.0 -- Instagram Shopping full adapter.
//
// Promotes the v3.9.1 EC-4-4 stub to a production-ready adapter
// backed by the Facebook/Instagram Graph API for Commerce.
//
// Implements:
//   - channelport.ChannelOrderListing (CreateListing)
//   - fulfilment.ChannelStatusUpdater (UpdateOrderStatus)
//   - channel.ChannelAdapter (Publish via catalog sync)
//
// Config env vars: EC_INSTAGRAM_APP_ID, EC_INSTAGRAM_APP_SECRET,
//
//	EC_INSTAGRAM_ACCESS_TOKEN
//
// Webhook verification reuses the v3.4.0 FB X-Hub-Signature-256
// HMAC pattern (same ComputeFacebookWebhookSignature helper).
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
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/fulfilment"
	"github.com/nfsarch33/agentic-ecommerce/internal/channelport"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

const InstagramChannelName = "instagram"

const (
	DefaultInstagramBaseURL = "https://graph.facebook.com/v19.0"
	DefaultInstagramTimeout = 15 * time.Second
	MinInstagramSecretBytes = 32
)

var (
	ErrInstagramUnconfigured    = errors.New("instagram: client unconfigured")
	ErrInstagramAuthFailed      = errors.New("instagram: auth failed")
	ErrInstagramRateLimited     = errors.New("instagram: rate limited")
	ErrInstagramInvalidResponse = errors.New("instagram: invalid response")
	ErrInstagramClosed          = errors.New("instagram: client closed")
	ErrInstagramSecretTooShort  = errors.New("instagram: secret too short")
	ErrInstagramSignatureBad    = errors.New("instagram: webhook signature mismatch")
)

// InstagramConfig wires an InstagramAdapter.
type InstagramConfig struct {
	AppID       string
	AppSecret   string
	AccessToken string
	BaseURL     string
	HTTPClient  *http.Client
	Now         func() time.Time
	Metrics     InstagramMetricsHook
}

// InstagramMetricsHook is the small port for Prometheus counters.
type InstagramMetricsHook interface {
	RecordAPICall(tenantID, endpoint, status string, durationSec float64)
	RecordCatalogSync(tenantID, outcome string)
	RecordWebhook(tenantID, eventType, status string)
}

// InstagramAdapter is the v4.6.0 production Instagram Shopping adapter.
type InstagramAdapter struct {
	cfg      InstagramConfig
	tenantID string
	logger   *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewInstagramAdapter constructs the production adapter.
func NewInstagramAdapter(logger *slog.Logger, tenantID string, cfg InstagramConfig) (*InstagramAdapter, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrInstagramUnconfigured)
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, fmt.Errorf("%w: EC_INSTAGRAM_APP_ID required", ErrInstagramUnconfigured)
	}
	if len(cfg.AppSecret) < MinInstagramSecretBytes {
		return nil, fmt.Errorf("%w: got %d bytes, need >= %d", ErrInstagramSecretTooShort, len(cfg.AppSecret), MinInstagramSecretBytes)
	}
	if strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, fmt.Errorf("%w: EC_INSTAGRAM_ACCESS_TOKEN required", ErrInstagramUnconfigured)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultInstagramBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultInstagramTimeout}
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &InstagramAdapter{cfg: cfg, tenantID: tenantID, logger: logger}, nil
}

func (a *InstagramAdapter) Name() string        { return InstagramChannelName }
func (a *InstagramAdapter) ChannelName() string { return InstagramChannelName }
func (a *InstagramAdapter) Close(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

// Publish implements channel.ChannelAdapter via catalog sync.
func (a *InstagramAdapter) Publish(ctx context.Context, payload eventbus.ProductEnrichedPayload) error {
	if err := a.guard(); err != nil {
		return err
	}
	catalogPayload := buildIGCatalogPayload(payload)
	return a.sendCatalogRequest(ctx, catalogPayload)
}

// CreateListing implements channelport.ChannelOrderListing.
func (a *InstagramAdapter) CreateListing(ctx context.Context, req channelport.ListingRequest) error {
	if err := a.guard(); err != nil {
		return err
	}
	catalogPayload := buildIGCatalogFromListing(req)
	return a.sendCatalogRequest(ctx, catalogPayload)
}

// UpdateOrderStatus implements fulfilment.ChannelStatusUpdater.
func (a *InstagramAdapter) UpdateOrderStatus(ctx context.Context, in fulfilment.ChannelStatusUpdate) error {
	if err := a.guard(); err != nil {
		return err
	}
	body := buildIGOrderStatusPayload(in)
	return a.sendOrderUpdate(ctx, in.ExternalOrderID, body)
}

// GetOrders fetches recent orders from IG Commerce API.
func (a *InstagramAdapter) GetOrders(ctx context.Context, since time.Time) ([]IGOrder, error) {
	if err := a.guard(); err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/me/commerce_orders?since=%d", a.baseURL(), since.Unix())
	resp, status, err := a.doGet(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Close()
	if status != http.StatusOK {
		return nil, fmt.Errorf("%w: orders status=%d", ErrInstagramInvalidResponse, status)
	}
	return parseIGOrders(resp)
}

// VerifyWebhook verifies an Instagram Commerce webhook using the
// same FB X-Hub-Signature-256 pattern.
func (a *InstagramAdapter) VerifyWebhook(header string, payload []byte) error {
	return VerifyFacebookWebhook([]byte(a.cfg.AppSecret), header, payload)
}

func (a *InstagramAdapter) guard() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrInstagramClosed
	}
	return nil
}

func (a *InstagramAdapter) baseURL() string {
	return strings.TrimRight(a.cfg.BaseURL, "/")
}

func (a *InstagramAdapter) sendCatalogRequest(ctx context.Context, payload igCatalogPayload) error {
	start := a.cfg.Now()
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("instagram: marshal catalog: %w", err)
	}
	url := fmt.Sprintf("%s/me/catalog/products", a.baseURL())
	resp, status, err := a.doPost(ctx, url, body)
	if err != nil {
		a.recordAPI("catalog_sync", "error", start)
		return err
	}
	defer resp.Close()
	return a.parseIGResponse(status, resp, "catalog_sync", start)
}

func (a *InstagramAdapter) sendOrderUpdate(ctx context.Context, orderID string, body []byte) error {
	start := a.cfg.Now()
	url := fmt.Sprintf("%s/%s", a.baseURL(), orderID)
	resp, status, err := a.doPost(ctx, url, body)
	if err != nil {
		a.recordAPI("order_update", "error", start)
		return err
	}
	defer resp.Close()
	return a.parseIGResponse(status, resp, "order_update", start)
}

func (a *InstagramAdapter) parseIGResponse(status int, body io.ReadCloser, endpoint string, start time.Time) error {
	if status == http.StatusTooManyRequests {
		a.recordAPI(endpoint, "rate_limited", start)
		return ErrInstagramRateLimited
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		a.recordAPI(endpoint, "auth_failed", start)
		return ErrInstagramAuthFailed
	}
	if status >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(body, 512))
		a.recordAPI(endpoint, "error", start)
		return fmt.Errorf("%w: status=%d body=%s", ErrInstagramInvalidResponse, status, strings.TrimSpace(string(snippet)))
	}
	a.recordAPI(endpoint, "ok", start)
	return nil
}

func (a *InstagramAdapter) doPost(ctx context.Context, url string, body []byte) (io.ReadCloser, int, error) {
	proof, err := ComputeFacebookAppSecretProof([]byte(a.cfg.AppSecret), a.cfg.AccessToken)
	if err != nil {
		return nil, 0, err
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	fullURL := url + sep + "access_token=" + a.cfg.AccessToken + "&appsecret_proof=" + proof
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("instagram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("instagram: transport: %w", err)
	}
	return resp.Body, resp.StatusCode, nil
}

func (a *InstagramAdapter) doGet(ctx context.Context, url string) (io.ReadCloser, int, error) {
	proof, err := ComputeFacebookAppSecretProof([]byte(a.cfg.AppSecret), a.cfg.AccessToken)
	if err != nil {
		return nil, 0, err
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	fullURL := url + sep + "access_token=" + a.cfg.AccessToken + "&appsecret_proof=" + proof
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("instagram: build request: %w", err)
	}
	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("instagram: transport: %w", err)
	}
	return resp.Body, resp.StatusCode, nil
}

func (a *InstagramAdapter) recordAPI(endpoint, status string, start time.Time) {
	if a.cfg.Metrics == nil {
		return
	}
	a.cfg.Metrics.RecordAPICall(a.tenantID, endpoint, status, a.cfg.Now().Sub(start).Seconds())
}

// --- payload builders (decomposed per plan) ---

type igCatalogPayload struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Price       string `json:"price"`
	Currency    string `json:"currency"`
	ImageURL    string `json:"image_url,omitempty"`
	ProductID   string `json:"retailer_id"`
	Available   bool   `json:"availability"`
}

func buildIGCatalogPayload(p eventbus.ProductEnrichedPayload) igCatalogPayload {
	return igCatalogPayload{
		Title:       p.EnglishTitle,
		Description: p.EnglishDescription,
		Price:       fmt.Sprintf("%.2f", float64(p.PriceCents)/100),
		Currency:    p.Currency,
		ImageURL:    firstImage(p.Images),
		ProductID:   p.ProductID,
		Available:   p.StockUnits > 0,
	}
}

func buildIGCatalogFromListing(req channelport.ListingRequest) igCatalogPayload {
	return igCatalogPayload{
		Title:       req.Title,
		Description: req.Description,
		Price:       fmt.Sprintf("%.2f", float64(req.PriceAUDCents)/100),
		Currency:    "AUD",
		ImageURL:    req.ImageURL,
		ProductID:   req.ProductID,
		Available:   true,
	}
}

func buildIGOrderStatusPayload(in fulfilment.ChannelStatusUpdate) []byte {
	body, _ := json.Marshal(map[string]string{
		"status":          in.Status,
		"tracking_number": in.TrackingNumber,
	})
	return body
}

func firstImage(imgs []string) string {
	if len(imgs) > 0 {
		return imgs[0]
	}
	return ""
}

// IGOrder is a parsed Instagram Commerce order.
type IGOrder struct {
	OrderID    string    `json:"id"`
	Status     string    `json:"order_status"`
	TotalCents int       `json:"total_amount_cents"`
	CreatedAt  time.Time `json:"created_at"`
}

func parseIGOrders(r io.ReadCloser) ([]IGOrder, error) {
	var resp struct {
		Data []IGOrder `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("%w: orders decode: %v", ErrInstagramInvalidResponse, err)
	}
	return resp.Data, nil
}

// Compile-time guards.
var (
	_ channelport.ChannelOrderListing = (*InstagramAdapter)(nil)
	_ fulfilment.ChannelStatusUpdater = (*InstagramAdapter)(nil)
)
