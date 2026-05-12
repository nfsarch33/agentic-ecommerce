package carrier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/httpclient"
)

// DefaultDHLTimeout is the per-call deadline applied if the caller
// does not supply a context timeout.
const DefaultDHLTimeout = 15 * time.Second

// DHLTokenSource is the small port the DHL client consumes to obtain
// an OAuth2 client_credentials access token. Production wires the
// existing v3.4.0 EC-4-2 facebook_shop_oauth.go-shaped credentials
// adapter; tests pass a static-string implementation.
type DHLTokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// staticTokenSource is a tiny in-package fallback used when the
// caller passes ClientID + ClientSecret + an OAuthURL. Mirrors the
// FB Shop OAuth pattern but kept private so production composition
// can swap in the existing adapter.
type staticTokenSource struct {
	mu          sync.Mutex
	httpClient  *http.Client
	oauthURL    string
	clientID    string
	clientSec   string
	cached      string
	cachedUntil time.Time
}

func (s *staticTokenSource) AccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != "" && time.Now().UTC().Before(s.cachedUntil) {
		return s.cached, nil
	}
	body := strings.NewReader("grant_type=client_credentials&client_id=" + s.clientID + "&client_secret=" + s.clientSec)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.oauthURL, body)
	if err != nil {
		return "", fmt.Errorf("dhl: build oauth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: DHL oauth: %v", ErrCarrierUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("%w: DHL oauth status=%d", ErrCarrierUnavailable, resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("dhl: decode oauth: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("%w: DHL oauth empty token", ErrCarrierUnavailable)
	}
	s.cached = out.AccessToken
	if out.ExpiresIn > 0 {
		s.cachedUntil = time.Now().UTC().Add(time.Duration(out.ExpiresIn-30) * time.Second)
	} else {
		s.cachedUntil = time.Now().UTC().Add(5 * time.Minute)
	}
	return out.AccessToken, nil
}

// DHLConfig wires a DHLClient.
type DHLConfig struct {
	BaseURL      string
	OAuthURL     string
	ClientID     string
	ClientSecret string
	TokenSource  DHLTokenSource
	HTTPClient   *http.Client
	Now          func() time.Time
}

// DHLClient is the v3.8.0 EC-7-3 DHL Express adapter.
// v5.3.0: uses internal/httpclient for shared transport.
type DHLClient struct {
	cfg    DHLConfig
	tokens DHLTokenSource
	hc     *httpclient.Client
}

// NewDHLClient constructs the adapter. Either TokenSource OR
// (OAuthURL + ClientID + ClientSecret) must be provided so the client
// can obtain access tokens.
func NewDHLClient(cfg DHLConfig) (*DHLClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("%w: DHL BaseURL required", ErrCarrierClientUnconfigured)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultDHLTimeout}
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	tokens := cfg.TokenSource
	if tokens == nil {
		if strings.TrimSpace(cfg.OAuthURL) == "" {
			return nil, fmt.Errorf("%w: DHL OAuthURL or TokenSource required", ErrCarrierClientUnconfigured)
		}
		tokens = &staticTokenSource{
			httpClient: cfg.HTTPClient,
			oauthURL:   cfg.OAuthURL,
			clientID:   cfg.ClientID,
			clientSec:  cfg.ClientSecret,
		}
	}
	hc, err := httpclient.New(httpclient.Config{
		BaseURL:    cfg.BaseURL,
		Timeout:    DefaultDHLTimeout,
		HTTPClient: cfg.HTTPClient,
		MaxRetries: defaultCarrierMaxRetries,
		RetryDelay: defaultCarrierRetryDelay,
		RequestHooks: []httpclient.RequestHook{
			httpclient.JSONRequestHook(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: DHL httpclient: %v", ErrCarrierClientUnconfigured, err)
	}
	return &DHLClient{cfg: cfg, tokens: tokens, hc: hc}, nil
}

// Name returns the carrier identifier.
func (c *DHLClient) Name() string { return CarrierDHL }

// Quote calls the DHL quote endpoint.
func (c *DHLClient) Quote(ctx context.Context, req QuoteRequest) (Quote, error) {
	if err := validateQuoteRequest(req); err != nil {
		return Quote{}, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Quote{}, fmt.Errorf("dhl: marshal quote: %w", err)
	}
	respBody, status, err := c.do(ctx, http.MethodPost, "/express/quotes", body)
	if err != nil {
		return Quote{}, err
	}
	if status >= 500 {
		return Quote{}, fmt.Errorf("%w: DHL quote status=%d", ErrCarrierUnavailable, status)
	}
	if status >= 400 {
		return Quote{}, fmt.Errorf("%w: DHL quote status=%d", ErrLabelGenerationFailed, status)
	}
	return parseDHLQuote(bytes.NewReader(respBody))
}

// CreateLabel calls the DHL label endpoint.
func (c *DHLClient) CreateLabel(ctx context.Context, req LabelRequest) (Label, error) {
	if err := validateLabelRequest(req); err != nil {
		return Label{}, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Label{}, fmt.Errorf("dhl: marshal label: %w", err)
	}
	respBody, status, err := c.do(ctx, http.MethodPost, "/express/labels", body)
	if err != nil {
		return Label{}, err
	}
	if status >= 500 {
		return Label{}, fmt.Errorf("%w: DHL label status=%d", ErrCarrierUnavailable, status)
	}
	if status >= 400 {
		return Label{}, fmt.Errorf("%w: DHL label status=%d", ErrLabelGenerationFailed, status)
	}
	return parseDHLLabel(bytes.NewReader(respBody), c.cfg.Now())
}

func (c *DHLClient) do(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	token, err := c.tokens.AccessToken(ctx)
	if err != nil {
		return nil, 0, err
	}
	respBody, status, err := c.hc.DoWithHooks(ctx, method, path, body, httpclient.BearerAuthHook(func() string {
		return token
	}))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: DHL transport: %v", ErrCarrierUnavailable, err)
	}
	return respBody, status, nil
}

type dhlQuoteResponse struct {
	CostAUDCents int `json:"cost_aud_cents"`
	ETADays      int `json:"eta_days"`
}

type dhlLabelResponse struct {
	TrackingNumber string `json:"tracking_number"`
	LabelPDFURL    string `json:"label_pdf_url"`
	CostAUDCents   int    `json:"cost_aud_cents"`
	ETADays        int    `json:"eta_days"`
}

func parseDHLQuote(r io.Reader) (Quote, error) {
	var body dhlQuoteResponse
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return Quote{}, fmt.Errorf("%w: DHL quote decode: %v", ErrLabelGenerationFailed, err)
	}
	if body.CostAUDCents <= 0 || body.ETADays <= 0 {
		return Quote{}, fmt.Errorf("%w: DHL quote empty", ErrLabelGenerationFailed)
	}
	return Quote{Carrier: CarrierDHL, CostAUDCents: body.CostAUDCents, ETADays: body.ETADays}, nil
}

func parseDHLLabel(r io.Reader, now time.Time) (Label, error) {
	var body dhlLabelResponse
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return Label{}, fmt.Errorf("%w: DHL label decode: %v", ErrLabelGenerationFailed, err)
	}
	if strings.TrimSpace(body.TrackingNumber) == "" || strings.TrimSpace(body.LabelPDFURL) == "" {
		return Label{}, fmt.Errorf("%w: DHL label fields missing", ErrLabelGenerationFailed)
	}
	return Label{
		Carrier:        CarrierDHL,
		TrackingNumber: body.TrackingNumber,
		LabelPDFURL:    body.LabelPDFURL,
		CostAUDCents:   body.CostAUDCents,
		ETADays:        body.ETADays,
		GeneratedAt:    now,
	}, nil
}
