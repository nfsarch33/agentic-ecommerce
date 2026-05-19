package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// PayPalAdapter implements port.MultiPaymentGateway for PayPal.
// Uses the PayPal REST API v2 (Orders + Refunds) with OAuth2
// client_credentials for access tokens.
type PayPalAdapter struct {
	clientID     string
	clientSecret string
	apiURL       string
	httpClient   *http.Client

	mu    sync.Mutex
	token string
	expAt time.Time
}

// PayPalAdapterConfig configures the PayPal adapter.
type PayPalAdapterConfig struct {
	ClientID     string
	ClientSecret string
	Sandbox      bool
	APIURL       string
	HTTPClient   *http.Client
}

// NewPayPalAdapter builds a PayPal adapter from config/env.
func NewPayPalAdapter(cfg PayPalAdapterConfig) (*PayPalAdapter, error) {
	clientID := cfg.ClientID
	if clientID == "" {
		clientID = os.Getenv("EC_PAYPAL_CLIENT_ID")
	}
	clientSecret := cfg.ClientSecret
	if clientSecret == "" {
		clientSecret = os.Getenv("EC_PAYPAL_CLIENT_SECRET")
	}
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" {
		return nil, fmt.Errorf("paypal: EC_PAYPAL_CLIENT_ID and EC_PAYPAL_CLIENT_SECRET required")
	}
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = resolvePayPalBaseURL(cfg.Sandbox)
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &PayPalAdapter{
		clientID: clientID, clientSecret: clientSecret,
		apiURL: apiURL, httpClient: httpClient,
	}, nil
}

func resolvePayPalBaseURL(sandbox bool) string {
	if sandbox {
		return "https://api-m.sandbox.paypal.com"
	}
	return "https://api-m.paypal.com"
}

// Charge creates + captures a PayPal order in one round-trip.
func (a *PayPalAdapter) Charge(ctx context.Context, tenantID, orderID string, amount port.Money, _ port.PaymentMethod) (port.PaymentResult, error) {
	if err := validateChargeInput(tenantID, orderID, amount); err != nil {
		return port.PaymentResult{}, err
	}
	ppOrderID, err := a.createOrder(ctx, tenantID, orderID, amount)
	if err != nil {
		return port.PaymentResult{}, err
	}
	return a.captureOrder(ctx, ppOrderID, amount)
}

func (a *PayPalAdapter) createOrder(ctx context.Context, tenantID, orderID string, amount port.Money) (string, error) {
	body := map[string]any{
		"intent": "CAPTURE",
		"purchase_units": []map[string]any{{
			"reference_id": orderID,
			"custom_id":    tenantID,
			"amount": map[string]any{
				"currency_code": strings.ToUpper(amount.Currency),
				"value":         formatPayPalAmount(amount.Amount),
			},
		}},
	}
	respBody, err := a.doAuthedPost(ctx, "/v2/checkout/orders", body)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("%w: decode create order: %v", port.ErrPaymentProviderUnavailable, err)
	}
	if resp.ID == "" {
		return "", fmt.Errorf("%w: empty order id", port.ErrPaymentDeclined)
	}
	return resp.ID, nil
}

func (a *PayPalAdapter) captureOrder(ctx context.Context, ppOrderID string, amount port.Money) (port.PaymentResult, error) {
	path := "/v2/checkout/orders/" + ppOrderID + "/capture"
	respBody, err := a.doAuthedPost(ctx, path, map[string]any{})
	if err != nil {
		return port.PaymentResult{}, err
	}
	return parsePayPalCaptureResponse(respBody, ppOrderID, amount)
}

func parsePayPalCaptureResponse(body []byte, ppOrderID string, amount port.Money) (port.PaymentResult, error) {
	var resp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return port.PaymentResult{}, fmt.Errorf("%w: decode capture: %v", port.ErrPaymentProviderUnavailable, err)
	}
	status := port.PaymentStatusPending
	switch resp.Status {
	case "COMPLETED":
		status = port.PaymentStatusSucceeded
	case "DECLINED", "FAILED":
		return port.PaymentResult{}, fmt.Errorf("%w: status=%s", port.ErrPaymentDeclined, resp.Status)
	}
	return port.PaymentResult{
		PaymentID: ppOrderID, ExternalRef: resp.ID,
		Status: status, Provider: "paypal", Amount: amount,
	}, nil
}

// Refund issues a refund via the PayPal Captures API.
func (a *PayPalAdapter) Refund(ctx context.Context, _, paymentID string, amount port.Money) (port.RefundResult, error) {
	captureID, err := a.resolveCaptureID(ctx, paymentID)
	if err != nil {
		return port.RefundResult{}, err
	}
	path := "/v2/payments/captures/" + captureID + "/refund"
	body := map[string]any{
		"amount": map[string]any{
			"value":         formatPayPalAmount(amount.Amount),
			"currency_code": strings.ToUpper(amount.Currency),
		},
	}
	respBody, err := a.doAuthedPost(ctx, path, body)
	if err != nil {
		return port.RefundResult{}, err
	}
	return parsePayPalRefundResponse(respBody, paymentID, amount)
}

func (a *PayPalAdapter) resolveCaptureID(ctx context.Context, orderID string) (string, error) {
	respBody, err := a.doAuthedGet(ctx, "/v2/checkout/orders/"+orderID)
	if err != nil {
		return "", err
	}
	var resp struct {
		PurchaseUnits []struct {
			Payments struct {
				Captures []struct {
					ID string `json:"id"`
				} `json:"captures"`
			} `json:"payments"`
		} `json:"purchase_units"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("%w: decode order: %v", port.ErrPaymentProviderUnavailable, err)
	}
	if len(resp.PurchaseUnits) == 0 || len(resp.PurchaseUnits[0].Payments.Captures) == 0 {
		return "", fmt.Errorf("%w: no captures found", port.ErrPaymentNotFound)
	}
	return resp.PurchaseUnits[0].Payments.Captures[0].ID, nil
}

func parsePayPalRefundResponse(body []byte, paymentID string, amount port.Money) (port.RefundResult, error) {
	var resp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return port.RefundResult{}, fmt.Errorf("%w: decode refund: %v", port.ErrPaymentProviderUnavailable, err)
	}
	status := "pending"
	if resp.Status == "COMPLETED" {
		status = "succeeded"
	}
	return port.RefundResult{
		RefundID: resp.ID, PaymentID: paymentID,
		ExternalRef: resp.ID, Amount: amount, Status: status,
	}, nil
}

// VerifyWebhook verifies a PayPal webhook by calling the PayPal
// webhook signature verification API.
func (a *PayPalAdapter) VerifyWebhook(ctx context.Context, headers http.Header, body []byte) (port.WebhookEvent, error) {
	webhookID := headers.Get("Paypal-Webhook-Id")
	if webhookID == "" {
		webhookID = os.Getenv("EC_PAYPAL_WEBHOOK_ID")
	}
	verified, err := a.verifyWebhookSignature(ctx, headers, body, webhookID)
	if err != nil || !verified {
		return port.WebhookEvent{}, fmt.Errorf("%w: verification failed", port.ErrInvalidWebhookSignature)
	}
	return parsePayPalWebhookBody(body)
}

func (a *PayPalAdapter) verifyWebhookSignature(ctx context.Context, headers http.Header, body []byte, webhookID string) (bool, error) {
	verifyBody := map[string]any{
		"auth_algo":         headers.Get("Paypal-Auth-Algo"),
		"cert_url":          headers.Get("Paypal-Cert-Url"),
		"transmission_id":   headers.Get("Paypal-Transmission-Id"),
		"transmission_sig":  headers.Get("Paypal-Transmission-Sig"),
		"transmission_time": headers.Get("Paypal-Transmission-Time"),
		"webhook_id":        webhookID,
		"webhook_event":     json.RawMessage(body),
	}
	respBody, err := a.doAuthedPost(ctx, "/v1/notifications/verify-webhook-signature", verifyBody)
	if err != nil {
		return false, err
	}
	var resp struct {
		VerificationStatus string `json:"verification_status"`
	}
	_ = json.Unmarshal(respBody, &resp)
	return resp.VerificationStatus == "SUCCESS", nil
}

func parsePayPalWebhookBody(body []byte) (port.WebhookEvent, error) {
	var evt struct {
		ID        string `json:"id"`
		EventType string `json:"event_type"`
		Resource  struct {
			ID string `json:"id"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &evt); err != nil {
		return port.WebhookEvent{}, fmt.Errorf("parse webhook: %w", err)
	}
	whType := mapPayPalEventType(evt.EventType)
	return port.WebhookEvent{
		EventID: evt.ID, Type: whType,
		PaymentID: evt.Resource.ID, Provider: "paypal",
		RawJSON: body,
	}, nil
}

func mapPayPalEventType(eventType string) port.WebhookEventType {
	switch {
	case strings.Contains(eventType, "CAPTURE.COMPLETED"):
		return port.WebhookEventChargeSucceeded
	case strings.Contains(eventType, "CAPTURE.DENIED"), strings.Contains(eventType, "DISPUTE"):
		return port.WebhookEventChargeFailed
	case strings.Contains(eventType, "REFUND"):
		return port.WebhookEventRefundCompleted
	default:
		return port.WebhookEventChargeSucceeded
	}
}

// GetStatus queries the PayPal order via GET /v2/checkout/orders/:id.
func (a *PayPalAdapter) GetStatus(ctx context.Context, _ string, paymentID string) (port.PaymentStatus, error) {
	if strings.TrimSpace(paymentID) == "" {
		return "", fmt.Errorf("%w: payment_id required", port.ErrPaymentNotFound)
	}
	respBody, err := a.doAuthedGet(ctx, "/v2/checkout/orders/"+paymentID)
	if err != nil {
		return "", err
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("%w: decode: %v", port.ErrPaymentProviderUnavailable, err)
	}
	return mapPayPalStatus(resp.Status), nil
}

func mapPayPalStatus(s string) port.PaymentStatus {
	switch s {
	case "COMPLETED":
		return port.PaymentStatusSucceeded
	case "VOIDED":
		return port.PaymentStatusFailed
	default:
		return port.PaymentStatusPending
	}
}

// getAccessToken fetches or reuses an OAuth2 client_credentials token.
func (a *PayPalAdapter) getAccessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	if a.token != "" && time.Now().Before(a.expAt) {
		tok := a.token
		a.mu.Unlock()
		return tok, nil
	}
	a.mu.Unlock()
	return a.refreshAccessToken(ctx)
}

func (a *PayPalAdapter) refreshAccessToken(ctx context.Context) (string, error) {
	url := strings.TrimRight(a.apiURL, "/") + "/v1/oauth2/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return "", fmt.Errorf("%w: build token request: %v", port.ErrPaymentProviderUnavailable, err)
	}
	req.SetBasicAuth(a.clientID, a.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: token transport: %v", port.ErrPaymentProviderUnavailable, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%w: token status=%d", port.ErrPaymentProviderUnavailable, resp.StatusCode)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("%w: decode token: %v", port.ErrPaymentProviderUnavailable, err)
	}
	a.mu.Lock()
	a.token = tok.AccessToken
	a.expAt = time.Now().Add(time.Duration(tok.ExpiresIn-60) * time.Second)
	a.mu.Unlock()
	return tok.AccessToken, nil
}

func (a *PayPalAdapter) doAuthedPost(ctx context.Context, path string, body any) ([]byte, error) {
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	jsonBody, _ := json.Marshal(body)
	url := strings.TrimRight(a.apiURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", port.ErrPaymentProviderUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: transport: %v", port.ErrPaymentProviderUnavailable, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 400 {
		return nil, classifyPayPalError(resp.StatusCode, respBody)
	}
	return respBody, nil
}

func (a *PayPalAdapter) doAuthedGet(ctx context.Context, path string) ([]byte, error) {
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(a.apiURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", port.ErrPaymentProviderUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: transport: %v", port.ErrPaymentProviderUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, port.ErrPaymentNotFound
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 400 {
		return nil, classifyPayPalError(resp.StatusCode, respBody)
	}
	return respBody, nil
}

func classifyPayPalError(statusCode int, body []byte) error {
	return ClassifyHTTPError("paypal", statusCode, body)
}

func formatPayPalAmount(amountCents int64) string {
	return fmt.Sprintf("%.2f", float64(amountCents)/100.0)
}
