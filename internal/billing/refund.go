package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Refund-related sentinel errors.
var (
	ErrRefundFailed        = errors.New("billing refund failed")
	ErrRefundNotFound      = errors.New("billing refund not found")
	ErrRefundAlreadyExists = errors.New("billing refund already exists")
)

// RefundRequest is the input to StripeRefunder.Refund.
type RefundRequest struct {
	TenantID        string `json:"tenant_id"`
	PaymentIntentID string `json:"payment_intent_id"`
	AmountCents     int    `json:"amount_cents"`
	Reason          string `json:"reason,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

// RefundResult is the output of a successful Stripe refund.
type RefundResult struct {
	RefundID        string `json:"refund_id"`
	PaymentIntentID string `json:"payment_intent_id"`
	AmountCents     int    `json:"amount_cents"`
	Status          string `json:"status"`
	Currency        string `json:"currency"`
}

// StripeRefunder issues refunds via the Stripe Refunds API.
type StripeRefunder struct {
	apiURL     string
	apiKey     string
	httpClient *http.Client
}

// StripeRefunderConfig configures a StripeRefunder.
type StripeRefunderConfig struct {
	APIURL     string
	APIKey     string
	HTTPClient *http.Client
}

// NewStripeRefunder constructs the refund adapter. The API key is
// read from STRIPE_SECRET_KEY env var if not set in config.
func NewStripeRefunder(cfg StripeRefunderConfig) (*StripeRefunder, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("STRIPE_SECRET_KEY")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("%w: STRIPE_SECRET_KEY required", ErrRefundFailed)
	}
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = "https://api.stripe.com"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &StripeRefunder{apiURL: apiURL, apiKey: apiKey, httpClient: httpClient}, nil
}

// Refund issues a refund against the given payment intent via
// POST /v1/refunds. Returns the Stripe refund object on success.
func (s *StripeRefunder) Refund(ctx context.Context, req RefundRequest) (RefundResult, error) {
	if err := validateRefundRequest(req); err != nil {
		return RefundResult{}, err
	}
	form := fmt.Sprintf("payment_intent=%s&amount=%d", req.PaymentIntentID, req.AmountCents)
	if req.Reason != "" {
		form += "&reason=" + req.Reason
	}
	url := strings.TrimRight(s.apiURL, "/") + "/v1/refunds"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(form))
	if err != nil {
		return RefundResult{}, fmt.Errorf("%w: build request: %v", ErrRefundFailed, err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	if req.IdempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return RefundResult{}, fmt.Errorf("%w: transport: %v", ErrRefundFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return RefundResult{}, fmt.Errorf("%w: status=%d body=%s", ErrRefundFailed, resp.StatusCode, string(body))
	}
	return parseStripeRefundResponse(resp.Body)
}

func validateRefundRequest(req RefundRequest) error {
	if strings.TrimSpace(req.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id required", ErrRefundFailed)
	}
	if strings.TrimSpace(req.PaymentIntentID) == "" {
		return fmt.Errorf("%w: payment_intent_id required", ErrRefundFailed)
	}
	if req.AmountCents <= 0 {
		return fmt.Errorf("%w: amount must be positive", ErrRefundFailed)
	}
	return nil
}

type stripeRefundResponse struct {
	ID            string `json:"id"`
	PaymentIntent string `json:"payment_intent"`
	Amount        int    `json:"amount"`
	Status        string `json:"status"`
	Currency      string `json:"currency"`
}

func parseStripeRefundResponse(r io.Reader) (RefundResult, error) {
	var body stripeRefundResponse
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return RefundResult{}, fmt.Errorf("%w: decode: %v", ErrRefundFailed, err)
	}
	return RefundResult{
		RefundID:        body.ID,
		PaymentIntentID: body.PaymentIntent,
		AmountCents:     body.Amount,
		Status:          body.Status,
		Currency:        body.Currency,
	}, nil
}
