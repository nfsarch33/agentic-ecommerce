package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// AlipayAdapter implements port.MultiPaymentGateway for Alipay.
// Uses RSA-SHA256 signing per the Alipay Open Platform specification.
type AlipayAdapter struct {
	appID      string
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	gatewayURL string
	httpClient *http.Client
}

// AlipayAdapterConfig configures the Alipay adapter.
type AlipayAdapterConfig struct {
	AppID          string
	PrivateKeyPath string
	PublicKeyPath  string
	GatewayURL     string
	HTTPClient     *http.Client
}

// NewAlipayAdapter builds an Alipay adapter from env/config.
func NewAlipayAdapter(cfg AlipayAdapterConfig) (*AlipayAdapter, error) {
	appID := cfg.AppID
	if appID == "" {
		appID = os.Getenv("EC_ALIPAY_APP_ID")
	}
	if strings.TrimSpace(appID) == "" {
		return nil, fmt.Errorf("alipay: EC_ALIPAY_APP_ID required")
	}
	privKey, err := loadPrivateKey(cfg.PrivateKeyPath, "EC_ALIPAY_PRIVATE_KEY_PATH")
	if err != nil {
		return nil, fmt.Errorf("alipay: %w", err)
	}
	pubKey, err := loadPublicKey(cfg.PublicKeyPath, "EC_ALIPAY_PUBLIC_KEY_PATH")
	if err != nil {
		return nil, fmt.Errorf("alipay: %w", err)
	}
	gatewayURL := cfg.GatewayURL
	if gatewayURL == "" {
		gatewayURL = "https://openapi.alipay.com/gateway.do"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &AlipayAdapter{
		appID: appID, privateKey: privKey, publicKey: pubKey,
		gatewayURL: gatewayURL, httpClient: httpClient,
	}, nil
}

// Charge creates a trade via alipay.trade.create.
func (a *AlipayAdapter) Charge(ctx context.Context, tenantID, orderID string, amount port.Money, _ port.PaymentMethod) (port.PaymentResult, error) {
	if err := validateChargeInput(tenantID, orderID, amount); err != nil {
		return port.PaymentResult{}, err
	}
	bizContent := map[string]any{
		"out_trade_no":    orderID,
		"total_amount":    formatAlipayAmount(amount.Amount, amount.Currency),
		"subject":         fmt.Sprintf("Order %s", orderID),
		"product_code":    "QUICK_MSECURITY_PAY",
		"passback_params": fmt.Sprintf("tenant_id=%s", tenantID),
	}
	respBody, err := a.callAPI(ctx, "alipay.trade.create", bizContent)
	if err != nil {
		return port.PaymentResult{}, err
	}
	return parseAlipayChargeResponse(respBody, amount)
}

func (a *AlipayAdapter) callAPI(ctx context.Context, method string, bizContent map[string]any) ([]byte, error) {
	bizJSON, _ := json.Marshal(bizContent)
	params := buildAlipayBaseParams(a.appID, method)
	params["biz_content"] = string(bizJSON)
	signed, err := a.signParams(params)
	if err != nil {
		return nil, fmt.Errorf("%w: sign: %v", port.ErrPaymentProviderUnavailable, err)
	}
	params["sign"] = signed
	return a.doPost(ctx, params)
}

func (a *AlipayAdapter) doPost(ctx context.Context, params map[string]string) ([]byte, error) {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.gatewayURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", port.ErrPaymentProviderUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: transport: %v", port.ErrPaymentProviderUnavailable, err)
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 8192))
}

func parseAlipayChargeResponse(body []byte, amount port.Money) (port.PaymentResult, error) {
	var resp struct {
		Response struct {
			Code       string `json:"code"`
			TradeNo    string `json:"trade_no"`
			OutTradeNo string `json:"out_trade_no"`
		} `json:"alipay_trade_create_response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return port.PaymentResult{}, fmt.Errorf("%w: decode: %v", port.ErrPaymentProviderUnavailable, err)
	}
	if resp.Response.Code != "10000" {
		return port.PaymentResult{}, fmt.Errorf("%w: code=%s", port.ErrPaymentDeclined, resp.Response.Code)
	}
	return port.PaymentResult{
		PaymentID: resp.Response.OutTradeNo, ExternalRef: resp.Response.TradeNo,
		Status: port.PaymentStatusPending, Provider: "alipay", Amount: amount,
	}, nil
}

// Refund issues a refund via alipay.trade.refund.
func (a *AlipayAdapter) Refund(ctx context.Context, tenantID, paymentID string, amount port.Money) (port.RefundResult, error) {
	bizContent := map[string]any{
		"trade_no":       paymentID,
		"refund_amount":  formatAlipayAmount(amount.Amount, amount.Currency),
		"refund_reason":  "customer_return",
		"out_request_no": tenantID + ":" + paymentID,
	}
	respBody, err := a.callAPI(ctx, "alipay.trade.refund", bizContent)
	if err != nil {
		return port.RefundResult{}, err
	}
	return parseAlipayRefundResponse(respBody, paymentID, amount)
}

func parseAlipayRefundResponse(body []byte, paymentID string, amount port.Money) (port.RefundResult, error) {
	var resp struct {
		Response struct {
			Code    string `json:"code"`
			TradeNo string `json:"trade_no"`
		} `json:"alipay_trade_refund_response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return port.RefundResult{}, fmt.Errorf("%w: decode: %v", port.ErrPaymentProviderUnavailable, err)
	}
	if resp.Response.Code != "10000" {
		return port.RefundResult{}, fmt.Errorf("%w: code=%s", port.ErrPaymentDeclined, resp.Response.Code)
	}
	return port.RefundResult{
		RefundID: resp.Response.TradeNo, PaymentID: paymentID,
		ExternalRef: resp.Response.TradeNo, Amount: amount, Status: "succeeded",
	}, nil
}

// VerifyWebhook verifies an async notification via Alipay RSA public
// key signature verification.
func (a *AlipayAdapter) VerifyWebhook(_ context.Context, _ http.Header, body []byte) (port.WebhookEvent, error) {
	params, err := url.ParseQuery(string(body))
	if err != nil {
		return port.WebhookEvent{}, fmt.Errorf("%w: parse body: %v", port.ErrInvalidWebhookSignature, err)
	}
	sign := params.Get("sign")
	if sign == "" {
		return port.WebhookEvent{}, fmt.Errorf("%w: missing sign param", port.ErrInvalidWebhookSignature)
	}
	if err := a.verifyRSASignature(params, sign); err != nil {
		return port.WebhookEvent{}, fmt.Errorf("%w: %v", port.ErrInvalidWebhookSignature, err)
	}
	return buildAlipayWebhookEvent(params, body), nil
}

func (a *AlipayAdapter) verifyRSASignature(params url.Values, sign string) error {
	signBytes, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return fmt.Errorf("decode sign: %w", err)
	}
	toVerify := buildAlipaySignString(params)
	hash := sha256.Sum256([]byte(toVerify))
	return rsa.VerifyPKCS1v15(a.publicKey, crypto.SHA256, hash[:], signBytes)
}

func buildAlipayWebhookEvent(params url.Values, body []byte) port.WebhookEvent {
	evtType := port.WebhookEventChargeSucceeded
	if params.Get("trade_status") == "TRADE_CLOSED" {
		evtType = port.WebhookEventChargeFailed
	}
	if params.Get("refund_status") == "REFUND_SUCCESS" {
		evtType = port.WebhookEventRefundCompleted
	}
	return port.WebhookEvent{
		EventID: params.Get("notify_id"), Type: evtType,
		PaymentID: params.Get("trade_no"), Provider: "alipay",
		RawJSON: body,
	}
}

// GetStatus queries the trade via alipay.trade.query.
func (a *AlipayAdapter) GetStatus(ctx context.Context, _ string, paymentID string) (port.PaymentStatus, error) {
	if strings.TrimSpace(paymentID) == "" {
		return "", fmt.Errorf("%w: payment_id required", port.ErrPaymentNotFound)
	}
	bizContent := map[string]any{"trade_no": paymentID}
	respBody, err := a.callAPI(ctx, "alipay.trade.query", bizContent)
	if err != nil {
		return "", err
	}
	return parseAlipayTradeQueryStatus(respBody)
}

func parseAlipayTradeQueryStatus(body []byte) (port.PaymentStatus, error) {
	var resp struct {
		Response struct {
			Code        string `json:"code"`
			TradeStatus string `json:"trade_status"`
		} `json:"alipay_trade_query_response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("%w: decode: %v", port.ErrPaymentProviderUnavailable, err)
	}
	if resp.Response.Code != "10000" {
		return "", fmt.Errorf("%w: code=%s", port.ErrPaymentNotFound, resp.Response.Code)
	}
	switch resp.Response.TradeStatus {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		return port.PaymentStatusSucceeded, nil
	case "TRADE_CLOSED":
		return port.PaymentStatusFailed, nil
	default:
		return port.PaymentStatusPending, nil
	}
}

// signParams produces an RSA-SHA256 signature over the sorted
// key=value string (Alipay Open Platform format).
func (a *AlipayAdapter) signParams(params map[string]string) (string, error) {
	toSign := buildSignString(params)
	hash := sha256.Sum256([]byte(toSign))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func buildSignString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "sign" || k == "sign_type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if params[k] != "" {
			parts = append(parts, k+"="+params[k])
		}
	}
	return strings.Join(parts, "&")
}

func buildAlipaySignString(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "sign" || k == "sign_type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := params.Get(k)
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, "&")
}

func buildAlipayBaseParams(appID, method string) map[string]string {
	return map[string]string{
		"app_id":    appID,
		"method":    method,
		"charset":   "utf-8",
		"sign_type": "RSA2",
		"timestamp": time.Now().UTC().Format("2006-01-02 15:04:05"),
		"version":   "1.0",
		"format":    "JSON",
	}
}

func formatAlipayAmount(amountCents int64, _ string) string {
	yuan := float64(amountCents) / 100.0
	return fmt.Sprintf("%.2f", yuan)
}

func loadPrivateKey(path, envVar string) (*rsa.PrivateKey, error) {
	if path == "" {
		path = os.Getenv(envVar)
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%s required", envVar)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		pk, err2 := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return pk, nil
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	return rsaKey, nil
}

func loadPublicKey(path, envVar string) (*rsa.PublicKey, error) {
	if path == "" {
		path = os.Getenv(envVar)
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%s required", envVar)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not RSA")
	}
	return rsaPub, nil
}
