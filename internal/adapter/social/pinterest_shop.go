// File scope: v4.6.0 -- Pinterest Shopping full adapter.
//
// Promotes the v3.9.1 EC-4-4 stub to a production-ready adapter
// backed by Pinterest API v5 for Shopping (catalogs, product pins,
// conversions tracking).
//
// Implements:
//   - channelport.ChannelOrderListing (CreateListing)
//   - fulfilment.ChannelStatusUpdater (UpdateOrderStatus)
//   - channel.ChannelAdapter (Publish via product pin creation)
//
// Config env vars: EC_PINTEREST_APP_ID, EC_PINTEREST_APP_SECRET,
//
//	EC_PINTEREST_ACCESS_TOKEN
package social

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/agent/fulfilment"
	"github.com/nfsarch33/helixon-ec/internal/channelport"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

const PinterestChannelName = "pinterest"

const (
	DefaultPinterestBaseURL = "https://api.pinterest.com/v5"
	DefaultPinterestTimeout = 15 * time.Second
	MinPinterestSecretBytes = 32
)

var (
	ErrPinterestUnconfigured    = errors.New("pinterest: client unconfigured")
	ErrPinterestAuthFailed      = errors.New("pinterest: auth failed")
	ErrPinterestRateLimited     = errors.New("pinterest: rate limited")
	ErrPinterestInvalidResponse = errors.New("pinterest: invalid response")
	ErrPinterestClosed          = errors.New("pinterest: client closed")
	ErrPinterestSecretTooShort  = errors.New("pinterest: secret too short")
	ErrPinterestSignatureBad    = errors.New("pinterest: webhook signature mismatch")
)

// PinterestConfig wires a PinterestAdapter.
type PinterestConfig struct {
	AppID       string
	AppSecret   string
	AccessToken string
	BaseURL     string
	HTTPClient  *http.Client
	Now         func() time.Time
	Metrics     PinterestMetricsHook
}

// PinterestMetricsHook is the small port for Prometheus counters.
type PinterestMetricsHook interface {
	RecordAPICall(tenantID, endpoint, status string, durationSec float64)
	RecordCatalogFeed(tenantID, outcome string)
	RecordWebhook(tenantID, eventType, status string)
}

// PinterestAdapter is the v4.6.0 production Pinterest Shopping adapter.
type PinterestAdapter struct {
	cfg      PinterestConfig
	tenantID string
	logger   *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewPinterestAdapter constructs the production adapter.
func NewPinterestAdapter(logger *slog.Logger, tenantID string, cfg PinterestConfig) (*PinterestAdapter, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrPinterestUnconfigured)
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, fmt.Errorf("%w: EC_PINTEREST_APP_ID required", ErrPinterestUnconfigured)
	}
	if len(cfg.AppSecret) < MinPinterestSecretBytes {
		return nil, fmt.Errorf("%w: got %d bytes, need >= %d", ErrPinterestSecretTooShort, len(cfg.AppSecret), MinPinterestSecretBytes)
	}
	if strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, fmt.Errorf("%w: EC_PINTEREST_ACCESS_TOKEN required", ErrPinterestUnconfigured)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultPinterestBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultPinterestTimeout}
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PinterestAdapter{cfg: cfg, tenantID: tenantID, logger: logger}, nil
}

func (a *PinterestAdapter) Name() string        { return PinterestChannelName }
func (a *PinterestAdapter) ChannelName() string { return PinterestChannelName }
func (a *PinterestAdapter) Close(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

// Publish implements channel.ChannelAdapter via product pin creation.
func (a *PinterestAdapter) Publish(ctx context.Context, payload eventbus.ProductEnrichedPayload) error {
	if err := a.guard(); err != nil {
		return err
	}
	pinPayload := buildPinPayload(payload)
	return a.sendPinRequest(ctx, pinPayload)
}

// CreateListing implements channelport.ChannelOrderListing.
func (a *PinterestAdapter) CreateListing(ctx context.Context, req channelport.ListingRequest) error {
	if err := a.guard(); err != nil {
		return err
	}
	pinPayload := buildPinFromListing(req)
	return a.sendPinRequest(ctx, pinPayload)
}

// UpdateOrderStatus implements fulfilment.ChannelStatusUpdater
// via Pinterest Conversions API for order tracking.
func (a *PinterestAdapter) UpdateOrderStatus(ctx context.Context, in fulfilment.ChannelStatusUpdate) error {
	if err := a.guard(); err != nil {
		return err
	}
	convPayload := buildConversionPayload(in)
	return a.sendConversion(ctx, convPayload)
}

// VerifyWebhook verifies a Pinterest webhook (HMAC-SHA256).
func (a *PinterestAdapter) VerifyWebhook(signature string, payload []byte) error {
	mac := hmac.New(sha256.New, []byte(a.cfg.AppSecret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return ErrPinterestSignatureBad
	}
	return nil
}

func (a *PinterestAdapter) guard() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrPinterestClosed
	}
	return nil
}

func (a *PinterestAdapter) baseURL() string {
	return strings.TrimRight(a.cfg.BaseURL, "/")
}

func (a *PinterestAdapter) sendPinRequest(ctx context.Context, payload pinPayload) error {
	start := a.cfg.Now()
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pinterest: marshal pin: %w", err)
	}
	url := a.baseURL() + "/pins"
	resp, status, err := a.doPost(ctx, url, body)
	if err != nil {
		a.recordAPI("create_pin", "error", start)
		return err
	}
	defer resp.Close()
	return a.parsePinResponse(status, resp, "create_pin", start)
}

func (a *PinterestAdapter) sendConversion(ctx context.Context, payload conversionPayload) error {
	start := a.cfg.Now()
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pinterest: marshal conversion: %w", err)
	}
	url := a.baseURL() + "/ad_accounts/conversions"
	resp, status, err := a.doPost(ctx, url, body)
	if err != nil {
		a.recordAPI("conversion", "error", start)
		return err
	}
	defer resp.Close()
	return a.parsePinResponse(status, resp, "conversion", start)
}

func (a *PinterestAdapter) parsePinResponse(status int, body io.ReadCloser, endpoint string, start time.Time) error {
	if status == http.StatusTooManyRequests {
		a.recordAPI(endpoint, "rate_limited", start)
		return ErrPinterestRateLimited
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		a.recordAPI(endpoint, "auth_failed", start)
		return ErrPinterestAuthFailed
	}
	if status >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(body, 512))
		a.recordAPI(endpoint, "error", start)
		return fmt.Errorf("%w: status=%d body=%s", ErrPinterestInvalidResponse, status, strings.TrimSpace(string(snippet)))
	}
	a.recordAPI(endpoint, "ok", start)
	return nil
}

func (a *PinterestAdapter) doPost(ctx context.Context, url string, body []byte) (io.ReadCloser, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("pinterest: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.AccessToken)
	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("pinterest: transport: %w", err)
	}
	return resp.Body, resp.StatusCode, nil
}

func (a *PinterestAdapter) recordAPI(endpoint, status string, start time.Time) {
	if a.cfg.Metrics == nil {
		return
	}
	a.cfg.Metrics.RecordAPICall(a.tenantID, endpoint, status, a.cfg.Now().Sub(start).Seconds())
}

// --- payload builders (decomposed per plan) ---

type pinPayload struct {
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Link        string       `json:"link,omitempty"`
	MediaSource *pinMediaSrc `json:"media_source,omitempty"`
	ProductPin  *productPin  `json:"product_metadata,omitempty"`
}

type pinMediaSrc struct {
	SourceType string `json:"source_type"`
	URL        string `json:"url"`
}

type productPin struct {
	ProductID string `json:"item_id"`
	Price     string `json:"price"`
	Currency  string `json:"currency"`
	Available bool   `json:"availability"`
}

func buildPinPayload(p eventbus.ProductEnrichedPayload) pinPayload {
	pin := pinPayload{
		Title:       p.EnglishTitle,
		Description: p.EnglishDescription,
		ProductPin: &productPin{
			ProductID: p.ProductID,
			Price:     fmt.Sprintf("%.2f", float64(p.PriceCents)/100),
			Currency:  p.Currency,
			Available: p.StockUnits > 0,
		},
	}
	if img := firstImage(p.Images); img != "" {
		pin.MediaSource = &pinMediaSrc{SourceType: "image_url", URL: img}
	}
	return pin
}

func buildPinFromListing(req channelport.ListingRequest) pinPayload {
	pin := pinPayload{
		Title:       req.Title,
		Description: req.Description,
		ProductPin: &productPin{
			ProductID: req.ProductID,
			Price:     fmt.Sprintf("%.2f", float64(req.PriceAUDCents)/100),
			Currency:  "AUD",
			Available: true,
		},
	}
	if req.ImageURL != "" {
		pin.MediaSource = &pinMediaSrc{SourceType: "image_url", URL: req.ImageURL}
	}
	return pin
}

type conversionPayload struct {
	EventName string `json:"event_name"`
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	Tracking  string `json:"tracking_number,omitempty"`
}

func buildConversionPayload(in fulfilment.ChannelStatusUpdate) conversionPayload {
	return conversionPayload{
		EventName: "checkout",
		OrderID:   in.ExternalOrderID,
		Status:    in.Status,
		Tracking:  in.TrackingNumber,
	}
}

// Compile-time guards.
var (
	_ channelport.ChannelOrderListing = (*PinterestAdapter)(nil)
	_ fulfilment.ChannelStatusUpdater = (*PinterestAdapter)(nil)
)
