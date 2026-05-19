package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/billing"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

// StripeAdapter implements port.MultiPaymentGateway for Stripe.
// It wraps the existing v2.5.0 billing.StripeRefunder for refunds
// and the v3.3.0 billing.WebhookVerifier for webhook verification.
type StripeAdapter struct {
	apiURL     string
	apiKey     string
	refunder   *billing.StripeRefunder
	verifier   *billing.WebhookVerifier
	httpClient *http.Client
}

// StripeAdapterConfig configures the Stripe payment adapter.
type StripeAdapterConfig struct {
	APIURL        string
	APIKey        string
	WebhookSecret []byte
	HTTPClient    *http.Client
}

// NewStripeAdapter constructs a Stripe adapter. Reuses the v2.5.0
// StripeRefunder and v3.3.0 WebhookVerifier to avoid duplicating
// Stripe integration logic.
func NewStripeAdapter(cfg StripeAdapterConfig) (*StripeAdapter, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("STRIPE_SECRET_KEY")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("stripe: STRIPE_SECRET_KEY required")
	}
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = "https://api.stripe.com"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	refunder, err := billing.NewStripeRefunder(billing.StripeRefunderConfig{
		APIURL: apiURL, APIKey: apiKey, HTTPClient: httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("stripe: init refunder: %w", err)
	}
	var verifier *billing.WebhookVerifier
	if len(cfg.WebhookSecret) >= billing.MinWebhookSecretBytes {
		v, vErr := billing.NewWebhookVerifier(billing.WebhookConfig{Secret: cfg.WebhookSecret})
		if vErr != nil {
			return nil, fmt.Errorf("stripe: init verifier: %w", vErr)
		}
		verifier = v
	}
	return &StripeAdapter{
		apiURL: apiURL, apiKey: apiKey,
		refunder: refunder, verifier: verifier,
		httpClient: httpClient,
	}, nil
}

// Charge creates a PaymentIntent via POST /v1/payment_intents.
func (a *StripeAdapter) Charge(ctx context.Context, tenantID, orderID string, amount port.Money, method port.PaymentMethod) (port.PaymentResult, error) {
	if err := validateChargeInput(tenantID, orderID, amount); err != nil {
		return port.PaymentResult{}, err
	}
	body, err := a.createPaymentIntent(ctx, tenantID, orderID, amount, method)
	if err != nil {
		return port.PaymentResult{}, err
	}
	return parseStripeChargeResponse(body)
}

func (a *StripeAdapter) createPaymentIntent(ctx context.Context, tenantID, orderID string, amount port.Money, method port.PaymentMethod) ([]byte, error) {
	form := fmt.Sprintf(
		"amount=%d&currency=%s&metadata[tenant_id]=%s&metadata[order_id]=%s&payment_method_types[]=%s&confirm=true",
		amount.Amount, strings.ToLower(amount.Currency), tenantID, orderID, mapPaymentMethod(method),
	)
	url := strings.TrimRight(a.apiURL, "/") + "/v1/payment_intents"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(form))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", port.ErrPaymentProviderUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Idempotency-Key", tenantID+":"+orderID)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: transport: %v", port.ErrPaymentProviderUnavailable, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return nil, classifyStripeError(resp.StatusCode, respBody)
	}
	return respBody, nil
}

func parseStripeChargeResponse(body []byte) (port.PaymentResult, error) {
	var pi struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Amount int64  `json:"amount"`
		Cur    string `json:"currency"`
	}
	if err := json.Unmarshal(body, &pi); err != nil {
		return port.PaymentResult{}, fmt.Errorf("%w: decode: %v", port.ErrPaymentProviderUnavailable, err)
	}
	status := port.PaymentStatusPending
	if pi.Status == "succeeded" {
		status = port.PaymentStatusSucceeded
	} else if pi.Status == "requires_action" || pi.Status == "requires_payment_method" {
		status = port.PaymentStatusFailed
	}
	return port.PaymentResult{
		PaymentID: pi.ID, ExternalRef: pi.ID,
		Status: status, Provider: "stripe",
		Amount: port.Money{Amount: pi.Amount, Currency: strings.ToUpper(pi.Cur)},
	}, nil
}

func classifyStripeError(statusCode int, body []byte) error {
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &apiErr)
	switch apiErr.Error.Code {
	case "card_declined":
		return fmt.Errorf("%w: %s", port.ErrPaymentDeclined, string(body))
	case "insufficient_funds":
		return fmt.Errorf("%w: %s", port.ErrInsufficientFunds, string(body))
	}
	return ClassifyHTTPError("stripe", statusCode, body)
}

// Refund delegates to the existing v4.1.0 billing.StripeRefunder.
func (a *StripeAdapter) Refund(ctx context.Context, tenantID, paymentID string, amount port.Money) (port.RefundResult, error) {
	res, err := a.refunder.Refund(ctx, billing.RefundRequest{
		TenantID:        tenantID,
		PaymentIntentID: paymentID,
		AmountCents:     int(amount.Amount),
		IdempotencyKey:  tenantID + ":" + paymentID,
	})
	if err != nil {
		return port.RefundResult{}, err
	}
	return port.RefundResult{
		RefundID: res.RefundID, PaymentID: res.PaymentIntentID,
		ExternalRef: res.RefundID, Status: res.Status,
		Amount: port.Money{Amount: int64(res.AmountCents), Currency: strings.ToUpper(res.Currency)},
	}, nil
}

// VerifyWebhook reuses the v3.3.0 verify-then-parse pattern.
func (a *StripeAdapter) VerifyWebhook(_ context.Context, headers http.Header, body []byte) (port.WebhookEvent, error) {
	if a.verifier == nil {
		return port.WebhookEvent{}, fmt.Errorf("%w: webhook verifier not configured", port.ErrInvalidWebhookSignature)
	}
	sig := headers.Get("Stripe-Signature")
	if err := a.verifier.Verify(sig, body); err != nil {
		return port.WebhookEvent{}, fmt.Errorf("%w: %v", port.ErrInvalidWebhookSignature, err)
	}
	return parseStripeWebhookBody(body)
}

func parseStripeWebhookBody(body []byte) (port.WebhookEvent, error) {
	var evt struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID string `json:"id"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &evt); err != nil {
		return port.WebhookEvent{}, fmt.Errorf("parse webhook: %w", err)
	}
	whType := port.WebhookEventChargeSucceeded
	switch {
	case strings.Contains(evt.Type, "failed"):
		whType = port.WebhookEventChargeFailed
	case strings.Contains(evt.Type, "refund"):
		whType = port.WebhookEventRefundCompleted
	}
	return port.WebhookEvent{
		EventID: evt.ID, Type: whType,
		PaymentID: evt.Data.Object.ID, Provider: "stripe",
		RawJSON: body,
	}, nil
}

// GetStatus queries the PaymentIntent via GET /v1/payment_intents/:id.
func (a *StripeAdapter) GetStatus(ctx context.Context, tenantID, paymentID string) (port.PaymentStatus, error) {
	if strings.TrimSpace(paymentID) == "" {
		return "", fmt.Errorf("%w: payment_id required", port.ErrPaymentNotFound)
	}
	url := strings.TrimRight(a.apiURL, "/") + "/v1/payment_intents/" + paymentID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", port.ErrPaymentProviderUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", port.ErrPaymentProviderUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return "", port.ErrPaymentNotFound
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	var pi struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &pi); err != nil {
		return "", fmt.Errorf("%w: decode: %v", port.ErrPaymentProviderUnavailable, err)
	}
	return mapStripeStatus(pi.Status), nil
}

func mapStripeStatus(s string) port.PaymentStatus {
	switch s {
	case "succeeded":
		return port.PaymentStatusSucceeded
	case "canceled":
		return port.PaymentStatusFailed
	default:
		return port.PaymentStatusPending
	}
}

func mapPaymentMethod(m port.PaymentMethod) string {
	switch m {
	case port.PaymentMethodAlipay:
		return "alipay"
	case port.PaymentMethodWeChat:
		return "wechat_pay"
	default:
		return "card"
	}
}

func validateChargeInput(tenantID, orderID string, amount port.Money) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: tenant_id required", port.ErrPaymentDeclined)
	}
	if strings.TrimSpace(orderID) == "" {
		return fmt.Errorf("%w: order_id required", port.ErrPaymentDeclined)
	}
	if amount.Amount <= 0 {
		return fmt.Errorf("%w: amount must be positive", port.ErrPaymentDeclined)
	}
	if strings.TrimSpace(amount.Currency) == "" {
		return fmt.Errorf("%w: currency required", port.ErrPaymentDeclined)
	}
	return nil
}
