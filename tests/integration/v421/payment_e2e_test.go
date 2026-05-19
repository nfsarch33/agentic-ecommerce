//go:build v421_smoke

package v421

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/adapter/payment"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stripeServer(t *testing.T, status int, response map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPaymentE2E_StripeSuccess(t *testing.T) {
	t.Parallel()
	srv := stripeServer(t, 200, map[string]any{
		"id": "pi_e2e_1", "status": "succeeded", "amount": 5000, "currency": "aud",
	})
	a, err := payment.NewStripeAdapter(payment.StripeAdapterConfig{
		APIURL: srv.URL, APIKey: "sk_test", WebhookSecret: []byte(strings.Repeat("x", 32)),
	})
	require.NoError(t, err)
	res, err := a.Charge(context.Background(), "tenant-e2e", "order-e2e-1",
		port.Money{Amount: 5000, Currency: "AUD"}, port.PaymentMethodCard)
	require.NoError(t, err)
	assert.Equal(t, port.PaymentStatusSucceeded, res.Status)
	assert.Equal(t, "stripe", res.Provider)
}

func TestPaymentE2E_StripeDeclined(t *testing.T) {
	t.Parallel()
	srv := stripeServer(t, 402, map[string]any{
		"error": map[string]string{"code": "card_declined"},
	})
	a, err := payment.NewStripeAdapter(payment.StripeAdapterConfig{
		APIURL: srv.URL, APIKey: "sk_test", WebhookSecret: []byte(strings.Repeat("x", 32)),
	})
	require.NoError(t, err)
	_, err = a.Charge(context.Background(), "tenant-e2e", "order-e2e-2",
		port.Money{Amount: 5000, Currency: "AUD"}, port.PaymentMethodCard)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrPaymentDeclined)
}

func TestPaymentE2E_AlipaySuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"alipay_trade_create_response": map[string]string{
				"code": "10000", "trade_no": "e2e_alipay_1", "out_trade_no": "order-e2e-3",
			},
		})
	}))
	t.Cleanup(srv.Close)
	privPath, pubPath := generateTestRSAKeysE2E(t)
	a, err := payment.NewAlipayAdapter(payment.AlipayAdapterConfig{
		AppID: "e2e_app", PrivateKeyPath: privPath, PublicKeyPath: pubPath,
		GatewayURL: srv.URL,
	})
	require.NoError(t, err)
	res, err := a.Charge(context.Background(), "tenant-e2e", "order-e2e-3",
		port.Money{Amount: 9900, Currency: "CNY"}, port.PaymentMethodAlipay)
	require.NoError(t, err)
	assert.Equal(t, "alipay", res.Provider)
}

func TestPaymentE2E_AlipayDeclined(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"alipay_trade_create_response": map[string]string{
				"code": "40004", "sub_msg": "Insufficient balance",
			},
		})
	}))
	t.Cleanup(srv.Close)
	privPath, pubPath := generateTestRSAKeysE2E(t)
	a, err := payment.NewAlipayAdapter(payment.AlipayAdapterConfig{
		AppID: "e2e_app", PrivateKeyPath: privPath, PublicKeyPath: pubPath,
		GatewayURL: srv.URL,
	})
	require.NoError(t, err)
	_, err = a.Charge(context.Background(), "tenant-e2e", "order-e2e-4",
		port.Money{Amount: 9900, Currency: "CNY"}, port.PaymentMethodAlipay)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrPaymentDeclined)
}

func TestPaymentE2E_WeChatSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"prepay_id": "wx_e2e_1", "code_url": "weixin://test",
		})
	}))
	t.Cleanup(srv.Close)
	a, err := payment.NewWeChatAdapter(payment.WeChatAdapterConfig{
		AppID: "wx_e2e", MchID: "mch_e2e", APIKeyV3: strings.Repeat("k", 32),
		CertSerial: "cert_e2e", APIURL: srv.URL,
	})
	require.NoError(t, err)
	res, err := a.Charge(context.Background(), "tenant-e2e", "order-e2e-5",
		port.Money{Amount: 8800, Currency: "CNY"}, port.PaymentMethodWeChat)
	require.NoError(t, err)
	assert.Equal(t, "wechat", res.Provider)
}

func TestPaymentE2E_WeChatDeclined(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "PARAM_ERROR"})
	}))
	t.Cleanup(srv.Close)
	a, err := payment.NewWeChatAdapter(payment.WeChatAdapterConfig{
		AppID: "wx_e2e", MchID: "mch_e2e", APIKeyV3: strings.Repeat("k", 32),
		CertSerial: "cert_e2e", APIURL: srv.URL,
	})
	require.NoError(t, err)
	_, err = a.Charge(context.Background(), "tenant-e2e", "order-e2e-6",
		port.Money{Amount: 8800, Currency: "CNY"}, port.PaymentMethodWeChat)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrPaymentDeclined)
}

func TestPaymentE2E_Idempotency(t *testing.T) {
	t.Parallel()
	srv := stripeServer(t, 200, map[string]any{
		"id": "pi_idem_1", "status": "succeeded", "amount": 3000, "currency": "aud",
	})
	a, err := payment.NewStripeAdapter(payment.StripeAdapterConfig{
		APIURL: srv.URL, APIKey: "sk_test", WebhookSecret: []byte(strings.Repeat("x", 32)),
	})
	require.NoError(t, err)
	res1, err := a.Charge(context.Background(), "tenant-idem", "order-idem",
		port.Money{Amount: 3000, Currency: "AUD"}, port.PaymentMethodCard)
	require.NoError(t, err)
	res2, err := a.Charge(context.Background(), "tenant-idem", "order-idem",
		port.Money{Amount: 3000, Currency: "AUD"}, port.PaymentMethodCard)
	require.NoError(t, err)
	assert.Equal(t, res1.PaymentID, res2.PaymentID)
}

func TestPaymentE2E_TenantIsolation(t *testing.T) {
	t.Parallel()
	var lastTenantID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		tenantFromMetadata := r.FormValue("metadata[tenant_id]")
		if lastTenantID == "" {
			lastTenantID = tenantFromMetadata
		} else if tenantFromMetadata != lastTenantID {
			t.Logf("tenant isolation verified: %s != %s", lastTenantID, tenantFromMetadata)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "pi_" + tenantFromMetadata, "status": "succeeded",
			"amount": 1000, "currency": "aud",
		})
	}))
	t.Cleanup(srv.Close)
	a, err := payment.NewStripeAdapter(payment.StripeAdapterConfig{
		APIURL: srv.URL, APIKey: "sk_test", WebhookSecret: []byte(strings.Repeat("x", 32)),
	})
	require.NoError(t, err)
	resA, err := a.Charge(context.Background(), "tenant-A", "order-iso-1",
		port.Money{Amount: 1000, Currency: "AUD"}, port.PaymentMethodCard)
	require.NoError(t, err)
	resB, err := a.Charge(context.Background(), "tenant-B", "order-iso-2",
		port.Money{Amount: 1000, Currency: "AUD"}, port.PaymentMethodCard)
	require.NoError(t, err)
	assert.NotEqual(t, resA.PaymentID, resB.PaymentID)
}
