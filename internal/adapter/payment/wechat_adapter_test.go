package payment_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/payment"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWeChatAPIKeyV3 = "01234567890123456789012345678901"

func wechatTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func wechatAdapter(t *testing.T, apiURL string) *payment.WeChatAdapter {
	t.Helper()
	a, err := payment.NewWeChatAdapter(payment.WeChatAdapterConfig{
		AppID:      "wx_test_app",
		MchID:      "mch_test_123",
		APIKeyV3:   testWeChatAPIKeyV3,
		CertSerial: "cert_serial_test",
		APIURL:     apiURL,
	})
	require.NoError(t, err)
	return a
}

func TestWeChatCharge_Success(t *testing.T) {
	t.Parallel()
	srv := wechatTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/pay/transactions/native" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"prepay_id": "wx_prepay_123",
				"code_url":  "weixin://wxpay/bizpayurl?pr=abc",
			})
			return
		}
		http.NotFound(w, r)
	})
	a := wechatAdapter(t, srv.URL)
	res, err := a.Charge(context.Background(), "tenant-a", "order-1",
		port.Money{Amount: 8800, Currency: "CNY"}, port.PaymentMethodWeChat)
	require.NoError(t, err)
	assert.Equal(t, "order-1", res.PaymentID)
	assert.Equal(t, "wx_prepay_123", res.ExternalRef)
	assert.Equal(t, "wechat", res.Provider)
	assert.Equal(t, port.PaymentStatusPending, res.Status)
}

func TestWeChatOrderQuery_Success(t *testing.T) {
	t.Parallel()
	srv := wechatTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"trade_state": "SUCCESS",
			})
			return
		}
		http.NotFound(w, r)
	})
	a := wechatAdapter(t, srv.URL)
	status, err := a.GetStatus(context.Background(), "tenant-a", "tx_123")
	require.NoError(t, err)
	assert.Equal(t, port.PaymentStatusSucceeded, status)
}

func TestWeChatRefund_Success(t *testing.T) {
	t.Parallel()
	srv := wechatTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/refund/domestic/refunds" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"refund_id": "wx_ref_456",
				"status":    "SUCCESS",
			})
			return
		}
		http.NotFound(w, r)
	})
	a := wechatAdapter(t, srv.URL)
	res, err := a.Refund(context.Background(), "tenant-a", "tx_123",
		port.Money{Amount: 5000, Currency: "CNY"})
	require.NoError(t, err)
	assert.Equal(t, "wx_ref_456", res.RefundID)
	assert.Equal(t, "succeeded", res.Status)
}

func TestWeChatWebhookDecryptVerify_Valid(t *testing.T) {
	t.Parallel()

	key := []byte(testWeChatAPIKeyV3)
	nonce := "test_nonce12"
	aad := "transaction"
	plaintext := []byte(`{"transaction_id":"wx_tx_789","out_trade_no":"order-1","trade_state":"SUCCESS"}`)

	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	ciphertext := gcm.Seal(nil, []byte(nonce), plaintext, []byte(aad))
	ciphertextB64 := base64.StdEncoding.EncodeToString(ciphertext)

	notifBody, _ := json.Marshal(map[string]any{
		"id":            "evt_wechat_1",
		"event_type":    "TRANSACTION.SUCCESS",
		"resource_type": "encrypt-resource",
		"resource": map[string]string{
			"algorithm":       "AEAD_AES_256_GCM",
			"ciphertext":      ciphertextB64,
			"nonce":           nonce,
			"associated_data": aad,
		},
	})

	ts := "1715328000"
	nonceHeader := "test_header_nonce"
	message := ts + "\n" + nonceHeader + "\n" + string(notifBody) + "\n"
	msgHash := sha256.Sum256([]byte(message))
	sigB64 := base64.StdEncoding.EncodeToString(msgHash[:])

	srv := wechatTestServer(t, func(http.ResponseWriter, *http.Request) {})
	a := wechatAdapter(t, srv.URL)

	headers := http.Header{}
	headers.Set("Wechatpay-Timestamp", ts)
	headers.Set("Wechatpay-Nonce", nonceHeader)
	headers.Set("Wechatpay-Signature", sigB64)

	evt, err := a.VerifyWebhook(context.Background(), headers, notifBody)
	require.NoError(t, err)
	assert.Equal(t, "evt_wechat_1", evt.EventID)
	assert.Equal(t, "wechat", evt.Provider)
	assert.Equal(t, "wx_tx_789", evt.PaymentID)
	assert.Equal(t, port.WebhookEventChargeSucceeded, evt.Type)
}

func TestWeChatWebhookVerify_MissingSignature(t *testing.T) {
	t.Parallel()
	srv := wechatTestServer(t, func(http.ResponseWriter, *http.Request) {})
	a := wechatAdapter(t, srv.URL)
	_, err := a.VerifyWebhook(context.Background(), http.Header{}, []byte(`{}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrInvalidWebhookSignature)
}

func TestWeChatAdapter_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ port.MultiPaymentGateway = (*payment.WeChatAdapter)(nil)
}

func TestWeChatCharge_ValidationError(t *testing.T) {
	t.Parallel()
	srv := wechatTestServer(t, func(http.ResponseWriter, *http.Request) {})
	a := wechatAdapter(t, srv.URL)
	_, err := a.Charge(context.Background(), "", "order-1",
		port.Money{Amount: 100, Currency: "CNY"}, port.PaymentMethodWeChat)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrPaymentDeclined)
}

func TestWeChatGetStatus_EmptyPaymentID(t *testing.T) {
	t.Parallel()
	srv := wechatTestServer(t, func(http.ResponseWriter, *http.Request) {})
	a := wechatAdapter(t, srv.URL)
	_, err := a.GetStatus(context.Background(), "tenant-a", "  ")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrPaymentNotFound)
}

func TestResolveWeChatAPIURL_Sandbox(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		wantURL string
	}{
		{"empty defaults to sandbox", "", "https://api.mch.weixin.qq.com/sandboxnew"},
		{"true is sandbox", "true", "https://api.mch.weixin.qq.com/sandboxnew"},
		{"1 is sandbox", "1", "https://api.mch.weixin.qq.com/sandboxnew"},
		{"false is production", "false", "https://api.mch.weixin.qq.com"},
		{"0 is production", "0", "https://api.mch.weixin.qq.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EC_WECHAT_SANDBOX", tc.envVal)
			got := payment.ResolveWeChatAPIURL()
			assert.Equal(t, tc.wantURL, got)
		})
	}
}

func TestNewWeChatAdapter_UsesSandboxURLByDefault(t *testing.T) {
	t.Setenv("EC_WECHAT_SANDBOX", "true")
	srv := wechatTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"prepay_id": "wx_sb_prepay", "code_url": "weixin://wxpay/bizpayurl?pr=sb",
		})
	})
	a, err := payment.NewWeChatAdapter(payment.WeChatAdapterConfig{
		AppID: "wx_sb", MchID: "mch_sb", APIKeyV3: testWeChatAPIKeyV3,
		CertSerial: "cert_sb", APIURL: srv.URL,
	})
	require.NoError(t, err)
	res, err := a.Charge(context.Background(), "t1", "order-sb",
		port.Money{Amount: 100, Currency: "CNY"}, port.PaymentMethodWeChat)
	require.NoError(t, err)
	assert.Equal(t, "order-sb", res.PaymentID)
}
