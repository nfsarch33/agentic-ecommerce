package payment_test

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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/payment"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestRSAKeys(t *testing.T) (privPath, pubPath string) {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	dir := t.TempDir()
	privPath = filepath.Join(dir, "private.pem")
	pubPath = filepath.Join(dir, "public.pem")

	privBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	require.NoError(t, err)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	require.NoError(t, os.WriteFile(privPath, privPEM, 0600))

	pubBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	require.NoError(t, err)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})
	require.NoError(t, os.WriteFile(pubPath, pubPEM, 0644))

	return privPath, pubPath
}

func alipayTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func alipayAdapter(t *testing.T, gatewayURL string) *payment.AlipayAdapter {
	t.Helper()
	privPath, pubPath := generateTestRSAKeys(t)
	a, err := payment.NewAlipayAdapter(payment.AlipayAdapterConfig{
		AppID:          "test_app_id",
		PrivateKeyPath: privPath,
		PublicKeyPath:  pubPath,
		GatewayURL:     gatewayURL,
	})
	require.NoError(t, err)
	return a
}

func TestAlipayCharge_Success(t *testing.T) {
	t.Parallel()
	srv := alipayTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"alipay_trade_create_response": map[string]string{
				"code":         "10000",
				"trade_no":     "2026051022001",
				"out_trade_no": "order-1",
			},
		})
	})
	a := alipayAdapter(t, srv.URL)
	res, err := a.Charge(context.Background(), "tenant-a", "order-1",
		port.Money{Amount: 9900, Currency: "CNY"}, port.PaymentMethodAlipay)
	require.NoError(t, err)
	assert.Equal(t, "order-1", res.PaymentID)
	assert.Equal(t, "2026051022001", res.ExternalRef)
	assert.Equal(t, "alipay", res.Provider)
}

func TestAlipayTradeQuery_Success(t *testing.T) {
	t.Parallel()
	srv := alipayTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"alipay_trade_query_response": map[string]string{
				"code":         "10000",
				"trade_status": "TRADE_SUCCESS",
			},
		})
	})
	a := alipayAdapter(t, srv.URL)
	status, err := a.GetStatus(context.Background(), "tenant-a", "2026051022001")
	require.NoError(t, err)
	assert.Equal(t, port.PaymentStatusSucceeded, status)
}

func TestAlipayRefund_Success(t *testing.T) {
	t.Parallel()
	srv := alipayTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"alipay_trade_refund_response": map[string]string{
				"code":     "10000",
				"trade_no": "2026051022001",
			},
		})
	})
	a := alipayAdapter(t, srv.URL)
	res, err := a.Refund(context.Background(), "tenant-a", "2026051022001",
		port.Money{Amount: 5000, Currency: "CNY"})
	require.NoError(t, err)
	assert.Equal(t, "2026051022001", res.RefundID)
	assert.Equal(t, "succeeded", res.Status)
}

func TestAlipayWebhookVerify_Valid(t *testing.T) {
	t.Parallel()
	privPath, pubPath := generateTestRSAKeys(t)

	privPEM, err := os.ReadFile(privPath)
	require.NoError(t, err)
	block, _ := pem.Decode(privPEM)
	privKeyRaw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	privKey := privKeyRaw.(*rsa.PrivateKey)

	params := url.Values{
		"notify_id":    {"n_123"},
		"trade_no":     {"2026051022001"},
		"trade_status": {"TRADE_SUCCESS"},
		"app_id":       {"test_app_id"},
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params.Get(k))
	}
	signStr := strings.Join(parts, "&")
	hash := sha256.Sum256([]byte(signStr))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])
	require.NoError(t, err)
	params.Set("sign", base64.StdEncoding.EncodeToString(sig))
	params.Set("sign_type", "RSA2")

	srv := alipayTestServer(t, func(http.ResponseWriter, *http.Request) {})
	a, err := payment.NewAlipayAdapter(payment.AlipayAdapterConfig{
		AppID: "test_app_id", PrivateKeyPath: privPath,
		PublicKeyPath: pubPath, GatewayURL: srv.URL,
	})
	require.NoError(t, err)

	evt, verifyErr := a.VerifyWebhook(context.Background(), http.Header{}, []byte(params.Encode()))
	require.NoError(t, verifyErr)
	assert.Equal(t, "n_123", evt.EventID)
	assert.Equal(t, "alipay", evt.Provider)
	assert.Equal(t, port.WebhookEventChargeSucceeded, evt.Type)
}

func TestAlipayWebhookVerify_InvalidSignature(t *testing.T) {
	t.Parallel()
	srv := alipayTestServer(t, func(http.ResponseWriter, *http.Request) {})
	a := alipayAdapter(t, srv.URL)
	params := url.Values{
		"notify_id":    {"n_bad"},
		"trade_no":     {"bad_trade"},
		"trade_status": {"TRADE_SUCCESS"},
		"sign":         {base64.StdEncoding.EncodeToString([]byte("invalid"))},
		"sign_type":    {"RSA2"},
	}
	_, err := a.VerifyWebhook(context.Background(), http.Header{}, []byte(params.Encode()))
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrInvalidWebhookSignature)
}

func TestAlipayAdapter_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ port.MultiPaymentGateway = (*payment.AlipayAdapter)(nil)
}

func TestAlipayCharge_ValidationError(t *testing.T) {
	t.Parallel()
	srv := alipayTestServer(t, func(http.ResponseWriter, *http.Request) {})
	a := alipayAdapter(t, srv.URL)
	_, err := a.Charge(context.Background(), "", "order-1",
		port.Money{Amount: 100, Currency: "CNY"}, port.PaymentMethodAlipay)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrPaymentDeclined)
}

func TestAlipayGetStatus_EmptyPaymentID(t *testing.T) {
	t.Parallel()
	srv := alipayTestServer(t, func(http.ResponseWriter, *http.Request) {})
	a := alipayAdapter(t, srv.URL)
	_, err := a.GetStatus(context.Background(), "tenant-a", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrPaymentNotFound)
}

func init() {
	_ = fmt.Sprintf
}
