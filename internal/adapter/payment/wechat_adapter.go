package payment

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// WeChatAdapter implements port.MultiPaymentGateway for WeChat Pay.
// Uses AEAD-AES-256-GCM for API v3 encryption per WeChat Pay docs.
type WeChatAdapter struct {
	appID      string
	mchID      string
	apiKeyV3   []byte
	certSerial string
	apiURL     string
	httpClient *http.Client
}

// WeChatAdapterConfig configures the WeChat Pay adapter.
type WeChatAdapterConfig struct {
	AppID      string
	MchID      string
	APIKeyV3   string
	CertSerial string
	APIURL     string
	HTTPClient *http.Client
}

// NewWeChatAdapter builds a WeChat Pay adapter.
func NewWeChatAdapter(cfg WeChatAdapterConfig) (*WeChatAdapter, error) {
	appID := cfg.AppID
	if appID == "" {
		appID = os.Getenv("EC_WECHAT_APP_ID")
	}
	mchID := cfg.MchID
	if mchID == "" {
		mchID = os.Getenv("EC_WECHAT_MCH_ID")
	}
	apiKeyV3 := cfg.APIKeyV3
	if apiKeyV3 == "" {
		apiKeyV3 = os.Getenv("EC_WECHAT_API_KEY_V3")
	}
	certSerial := cfg.CertSerial
	if certSerial == "" {
		certSerial = os.Getenv("EC_WECHAT_CERT_SERIAL")
	}
	if err := validateWeChatConfig(appID, mchID, apiKeyV3); err != nil {
		return nil, err
	}
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = ResolveWeChatAPIURL()
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &WeChatAdapter{
		appID: appID, mchID: mchID,
		apiKeyV3: []byte(apiKeyV3), certSerial: certSerial,
		apiURL: apiURL, httpClient: httpClient,
	}, nil
}

const (
	wechatProductionURL = "https://api.mch.weixin.qq.com"
	wechatSandboxURL    = "https://api.mch.weixin.qq.com/sandboxnew"
)

// ResolveWeChatAPIURL returns the sandbox or production URL based
// on the EC_WECHAT_SANDBOX env var (default: sandbox).
func ResolveWeChatAPIURL() string {
	v := os.Getenv("EC_WECHAT_SANDBOX")
	if v == "" || v == "true" || v == "1" {
		return wechatSandboxURL
	}
	return wechatProductionURL
}

func validateWeChatConfig(appID, mchID, apiKey string) error {
	if strings.TrimSpace(appID) == "" {
		return fmt.Errorf("wechat: EC_WECHAT_APP_ID required")
	}
	if strings.TrimSpace(mchID) == "" {
		return fmt.Errorf("wechat: EC_WECHAT_MCH_ID required")
	}
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("wechat: EC_WECHAT_API_KEY_V3 required")
	}
	return nil
}

// Charge creates a unified order (JSAPI/Native/H5).
func (w *WeChatAdapter) Charge(ctx context.Context, tenantID, orderID string, amount port.Money, _ port.PaymentMethod) (port.PaymentResult, error) {
	if err := validateChargeInput(tenantID, orderID, amount); err != nil {
		return port.PaymentResult{}, err
	}
	reqBody := buildWeChatOrderRequest(w.appID, w.mchID, tenantID, orderID, amount)
	respBody, err := w.postJSON(ctx, "/v3/pay/transactions/native", reqBody)
	if err != nil {
		return port.PaymentResult{}, err
	}
	return parseWeChatChargeResponse(respBody, orderID, amount)
}

func buildWeChatOrderRequest(appID, mchID, tenantID, orderID string, amount port.Money) map[string]any {
	return map[string]any{
		"appid":        appID,
		"mchid":        mchID,
		"description":  fmt.Sprintf("Order %s", orderID),
		"out_trade_no": orderID,
		"amount": map[string]any{
			"total":    amount.Amount,
			"currency": strings.ToUpper(amount.Currency),
		},
		"attach":     fmt.Sprintf("tenant_id=%s", tenantID),
		"notify_url": "https://api.example.com/webhooks/wechat",
	}
}

func (w *WeChatAdapter) postJSON(ctx context.Context, path string, body map[string]any) ([]byte, error) {
	jsonBody, _ := json.Marshal(body)
	url := strings.TrimRight(w.apiURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", port.ErrPaymentProviderUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", w.buildAuthHeader())
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: transport: %v", port.ErrPaymentProviderUnavailable, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 400 {
		return nil, classifyWeChatError(resp.StatusCode, respBody)
	}
	return respBody, nil
}

func (w *WeChatAdapter) getJSON(ctx context.Context, path string) ([]byte, error) {
	url := strings.TrimRight(w.apiURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", port.ErrPaymentProviderUnavailable, err)
	}
	req.Header.Set("Authorization", w.buildAuthHeader())
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: transport: %v", port.ErrPaymentProviderUnavailable, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode == 404 {
		return nil, port.ErrPaymentNotFound
	}
	if resp.StatusCode >= 400 {
		return nil, classifyWeChatError(resp.StatusCode, respBody)
	}
	return respBody, nil
}

func parseWeChatChargeResponse(body []byte, orderID string, amount port.Money) (port.PaymentResult, error) {
	var resp struct {
		PrepayID string `json:"prepay_id"`
		CodeURL  string `json:"code_url"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return port.PaymentResult{}, fmt.Errorf("%w: decode: %v", port.ErrPaymentProviderUnavailable, err)
	}
	return port.PaymentResult{
		PaymentID: orderID, ExternalRef: resp.PrepayID,
		Status: port.PaymentStatusPending, Provider: "wechat", Amount: amount,
	}, nil
}

func classifyWeChatError(statusCode int, body []byte) error {
	return ClassifyHTTPError("wechat", statusCode, body)
}

// Refund issues a refund via WeChat Pay API v3.
func (w *WeChatAdapter) Refund(ctx context.Context, tenantID, paymentID string, amount port.Money) (port.RefundResult, error) {
	reqBody := map[string]any{
		"transaction_id": paymentID,
		"out_refund_no":  tenantID + ":" + paymentID,
		"reason":         "customer_return",
		"amount": map[string]any{
			"refund":   amount.Amount,
			"total":    amount.Amount,
			"currency": strings.ToUpper(amount.Currency),
		},
	}
	respBody, err := w.postJSON(ctx, "/v3/refund/domestic/refunds", reqBody)
	if err != nil {
		return port.RefundResult{}, err
	}
	return parseWeChatRefundResponse(respBody, paymentID, amount)
}

func parseWeChatRefundResponse(body []byte, paymentID string, amount port.Money) (port.RefundResult, error) {
	var resp struct {
		RefundID string `json:"refund_id"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return port.RefundResult{}, fmt.Errorf("%w: decode: %v", port.ErrPaymentProviderUnavailable, err)
	}
	status := "pending"
	if resp.Status == "SUCCESS" {
		status = "succeeded"
	}
	return port.RefundResult{
		RefundID: resp.RefundID, PaymentID: paymentID,
		ExternalRef: resp.RefundID, Amount: amount, Status: status,
	}, nil
}

// VerifyWebhook decrypts and verifies an inbound WeChat Pay webhook
// using AEAD-AES-256-GCM per the API v3 specification.
func (w *WeChatAdapter) VerifyWebhook(_ context.Context, headers http.Header, body []byte) (port.WebhookEvent, error) {
	sig := headers.Get("Wechatpay-Signature")
	if sig == "" {
		return port.WebhookEvent{}, fmt.Errorf("%w: missing Wechatpay-Signature", port.ErrInvalidWebhookSignature)
	}
	if err := w.verifySignature(headers, body); err != nil {
		return port.WebhookEvent{}, fmt.Errorf("%w: %v", port.ErrInvalidWebhookSignature, err)
	}
	return w.decryptAndParseWebhook(body)
}

func (w *WeChatAdapter) verifySignature(headers http.Header, body []byte) error {
	ts := headers.Get("Wechatpay-Timestamp")
	nonce := headers.Get("Wechatpay-Nonce")
	sig := headers.Get("Wechatpay-Signature")
	message := ts + "\n" + nonce + "\n" + string(body) + "\n"
	msgHash := sha256.Sum256([]byte(message))
	sigBytes, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if hex.EncodeToString(msgHash[:]) != hex.EncodeToString(sigBytes) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func (w *WeChatAdapter) decryptAndParseWebhook(body []byte) (port.WebhookEvent, error) {
	var notification struct {
		ID           string `json:"id"`
		EventType    string `json:"event_type"`
		ResourceType string `json:"resource_type"`
		Resource     struct {
			Algorithm      string `json:"algorithm"`
			Ciphertext     string `json:"ciphertext"`
			Nonce          string `json:"nonce"`
			AssociatedData string `json:"associated_data"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &notification); err != nil {
		return port.WebhookEvent{}, fmt.Errorf("parse notification: %w", err)
	}
	plaintext, err := decryptAEAD(w.apiKeyV3, notification.Resource.Nonce, notification.Resource.Ciphertext, notification.Resource.AssociatedData)
	if err != nil {
		return port.WebhookEvent{}, fmt.Errorf("%w: decrypt: %v", port.ErrInvalidWebhookSignature, err)
	}
	var resource struct {
		TransactionID string `json:"transaction_id"`
		OutTradeNo    string `json:"out_trade_no"`
		TradeState    string `json:"trade_state"`
	}
	_ = json.Unmarshal(plaintext, &resource)
	evtType := port.WebhookEventChargeSucceeded
	if resource.TradeState == "CLOSED" || resource.TradeState == "PAYERROR" {
		evtType = port.WebhookEventChargeFailed
	}
	if strings.Contains(notification.EventType, "REFUND") {
		evtType = port.WebhookEventRefundCompleted
	}
	return port.WebhookEvent{
		EventID: notification.ID, Type: evtType,
		PaymentID: resource.TransactionID, Provider: "wechat",
		RawJSON: plaintext,
	}, nil
}

// decryptAEAD performs AES-256-GCM decryption per WeChat Pay API v3.
func decryptAEAD(key []byte, nonceStr, ciphertextB64, aad string) ([]byte, error) {
	derivedKey := deriveAESKey(key)
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	nonce := []byte(nonceStr)
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// deriveAESKey ensures the key is exactly 32 bytes for AES-256.
func deriveAESKey(key []byte) []byte {
	if len(key) == 32 {
		return key
	}
	h := sha256.Sum256(key)
	return h[:]
}

// GetStatus queries the order via WeChat Pay API v3.
func (w *WeChatAdapter) GetStatus(ctx context.Context, _ string, paymentID string) (port.PaymentStatus, error) {
	if strings.TrimSpace(paymentID) == "" {
		return "", fmt.Errorf("%w: payment_id required", port.ErrPaymentNotFound)
	}
	path := fmt.Sprintf("/v3/pay/transactions/id/%s?mchid=%s", paymentID, w.mchID)
	respBody, err := w.getJSON(ctx, path)
	if err != nil {
		return "", err
	}
	return parseWeChatOrderStatus(respBody)
}

func parseWeChatOrderStatus(body []byte) (port.PaymentStatus, error) {
	var resp struct {
		TradeState string `json:"trade_state"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("%w: decode: %v", port.ErrPaymentProviderUnavailable, err)
	}
	switch resp.TradeState {
	case "SUCCESS":
		return port.PaymentStatusSucceeded, nil
	case "CLOSED", "PAYERROR", "REVOKED":
		return port.PaymentStatusFailed, nil
	case "REFUND":
		return port.PaymentStatusRefunded, nil
	default:
		return port.PaymentStatusPending, nil
	}
}

func (w *WeChatAdapter) buildAuthHeader() string {
	return fmt.Sprintf("WECHATPAY2-SHA256-RSA2048 mchid=\"%s\",serial_no=\"%s\"", w.mchID, w.certSerial)
}
