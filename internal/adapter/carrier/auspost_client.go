package carrier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/httpclient"
)

// DefaultAusPostTimeout is the per-call deadline applied if the caller
// does not supply a context timeout. Chosen to mirror the v3.3.0
// EC-3-1 TikTok Shop client default.
const DefaultAusPostTimeout = 15 * time.Second

// AusPostConfig wires an AusPostClient.
//
// HTTPClient defaults to http.DefaultClient with the package timeout
// applied via per-call context.WithTimeout (so a shared client with
// keep-alive can still be reused across tenants).
type AusPostConfig struct {
	BaseURL    string
	APIKey     string
	APISecret  string // shared HMAC secret per AusPost developer portal
	HTTPClient *http.Client
	Now        func() time.Time
}

// AusPostClient is the v3.8.0 EC-7-3 Australia Post adapter.
// v5.3.0: uses internal/httpclient for shared transport.
type AusPostClient struct {
	cfg AusPostConfig
	hc  *httpclient.Client
}

// NewAusPostClient constructs the adapter.
func NewAusPostClient(cfg AusPostConfig) (*AusPostClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("%w: AusPost BaseURL required", ErrCarrierClientUnconfigured)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultAusPostTimeout}
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	apiKey := cfg.APIKey
	apiSecret := cfg.APISecret
	hc, err := httpclient.New(httpclient.Config{
		BaseURL:    cfg.BaseURL,
		Timeout:    DefaultAusPostTimeout,
		HTTPClient: cfg.HTTPClient,
		MaxRetries: defaultCarrierMaxRetries,
		RetryDelay: defaultCarrierRetryDelay,
		RequestHooks: []httpclient.RequestHook{
			httpclient.JSONRequestHook(),
			func(req *http.Request) error {
				req.Header.Set("X-AusPost-Key", apiKey)
				return nil
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: AusPost httpclient: %v", ErrCarrierClientUnconfigured, err)
	}
	_ = apiSecret
	return &AusPostClient{cfg: cfg, hc: hc}, nil
}

// Name returns the carrier identifier. Stable across runs so metric
// labels do not churn.
func (c *AusPostClient) Name() string { return CarrierAusPost }

// Quote calls the AusPost quote endpoint and returns the carrier's
// price + ETA. Cyclomatic stays at 5.
func (c *AusPostClient) Quote(ctx context.Context, req QuoteRequest) (Quote, error) {
	if err := validateQuoteRequest(req); err != nil {
		return Quote{}, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Quote{}, fmt.Errorf("auspost: marshal quote: %w", err)
	}
	respBody, status, err := c.do(ctx, http.MethodPost, "/v3/shipping/quotes", body)
	if err != nil {
		return Quote{}, err
	}
	if status >= 500 {
		return Quote{}, fmt.Errorf("%w: AusPost quote status=%d", ErrCarrierUnavailable, status)
	}
	if status >= 400 {
		return Quote{}, fmt.Errorf("%w: AusPost quote status=%d", ErrLabelGenerationFailed, status)
	}
	return parseAusPostQuote(bytes.NewReader(respBody))
}

// CreateLabel calls the AusPost label endpoint and returns the
// carrier-issued tracking number + PDF URL + locked cost + ETA.
func (c *AusPostClient) CreateLabel(ctx context.Context, req LabelRequest) (Label, error) {
	if err := validateLabelRequest(req); err != nil {
		return Label{}, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Label{}, fmt.Errorf("auspost: marshal label: %w", err)
	}
	respBody, status, err := c.do(ctx, http.MethodPost, "/v3/shipping/labels", body)
	if err != nil {
		return Label{}, err
	}
	if status >= 500 {
		return Label{}, fmt.Errorf("%w: AusPost label status=%d", ErrCarrierUnavailable, status)
	}
	if status >= 400 {
		return Label{}, fmt.Errorf("%w: AusPost label status=%d", ErrLabelGenerationFailed, status)
	}
	return parseAusPostLabel(bytes.NewReader(respBody), c.cfg.Now())
}

// do is the shared HTTP transport: context, signing, dispatch.
func (c *AusPostClient) do(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	respBody, status, err := c.hc.DoWithHooks(ctx, method, path, body, func(req *http.Request) error {
		req.Header.Set("X-AusPost-Signature", signAusPost(c.cfg.APISecret, method, path, body))
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("%w: AusPost transport: %v", ErrCarrierUnavailable, err)
	}
	return respBody, status, nil
}

// signAusPost is the v3.3.0 stdlib HMAC pattern: sha256 over the
// canonical request line {METHOD}\n{PATH}\n{BODY}.
func signAusPost(secret, method, path string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(method + "\n" + path + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func validateQuoteRequest(req QuoteRequest) error {
	if strings.TrimSpace(req.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id required", ErrInvalidShippingAddress)
	}
	if strings.TrimSpace(req.DestPost) == "" {
		return fmt.Errorf("%w: dest_post required", ErrInvalidShippingAddress)
	}
	if req.WeightGrams <= 0 {
		return fmt.Errorf("%w: weight must be positive", ErrInvalidShippingAddress)
	}
	return nil
}

func validateLabelRequest(req LabelRequest) error {
	if strings.TrimSpace(req.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id required", ErrInvalidShippingAddress)
	}
	if strings.TrimSpace(req.OrderID) == "" {
		return fmt.Errorf("%w: order_id required", ErrInvalidShippingAddress)
	}
	if strings.TrimSpace(req.DestPost) == "" {
		return fmt.Errorf("%w: dest_post required", ErrInvalidShippingAddress)
	}
	if req.WeightGrams <= 0 {
		return fmt.Errorf("%w: weight must be positive", ErrInvalidShippingAddress)
	}
	return nil
}

// auspostQuoteResponse is the wire shape consumed by parseAusPostQuote.
type auspostQuoteResponse struct {
	CostAUDCents int `json:"cost_aud_cents"`
	ETADays      int `json:"eta_days"`
}

// auspostLabelResponse is the wire shape consumed by parseAusPostLabel.
type auspostLabelResponse struct {
	TrackingNumber string `json:"tracking_number"`
	LabelPDFURL    string `json:"label_pdf_url"`
	CostAUDCents   int    `json:"cost_aud_cents"`
	ETADays        int    `json:"eta_days"`
}

func parseAusPostQuote(r io.Reader) (Quote, error) {
	var body auspostQuoteResponse
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return Quote{}, fmt.Errorf("%w: AusPost quote decode: %v", ErrLabelGenerationFailed, err)
	}
	if body.CostAUDCents <= 0 || body.ETADays <= 0 {
		return Quote{}, fmt.Errorf("%w: AusPost quote empty", ErrLabelGenerationFailed)
	}
	return Quote{Carrier: CarrierAusPost, CostAUDCents: body.CostAUDCents, ETADays: body.ETADays}, nil
}

func parseAusPostLabel(r io.Reader, now time.Time) (Label, error) {
	var body auspostLabelResponse
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return Label{}, fmt.Errorf("%w: AusPost label decode: %v", ErrLabelGenerationFailed, err)
	}
	if strings.TrimSpace(body.TrackingNumber) == "" || strings.TrimSpace(body.LabelPDFURL) == "" {
		return Label{}, fmt.Errorf("%w: AusPost label fields missing", ErrLabelGenerationFailed)
	}
	return Label{
		Carrier:        CarrierAusPost,
		TrackingNumber: body.TrackingNumber,
		LabelPDFURL:    body.LabelPDFURL,
		CostAUDCents:   body.CostAUDCents,
		ETADays:        body.ETADays,
		GeneratedAt:    now,
	}, nil
}

// VerifyAusPostHMAC is exported so the EC-7-4 webhook handler can
// reuse the same signing function for inbound verify-then-parse
// without copy-pasting the algorithm.
func VerifyAusPostHMAC(secret, method, path string, body []byte, signature string) bool {
	expected := signAusPost(secret, method, path, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// Compile-time interface assertions: both clients satisfy the
// CarrierClient port that fulfilment.ShippingLabelGenerator consumes.
var _ interface {
	Name() string
	Quote(ctx context.Context, req QuoteRequest) (Quote, error)
	CreateLabel(ctx context.Context, req LabelRequest) (Label, error)
} = (*AusPostClient)(nil)
