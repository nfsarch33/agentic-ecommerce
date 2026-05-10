package payment_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/payment"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func paypalTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func paypalAdapter(t *testing.T, apiURL string) *payment.PayPalAdapter {
	t.Helper()
	a, err := payment.NewPayPalAdapter(payment.PayPalAdapterConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Sandbox:      true,
		APIURL:       apiURL,
	})
	require.NoError(t, err)
	return a
}

func paypalMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-token",
			"expires_in":   3600,
		})
	})
	return mux
}

func TestPayPalCreateOrder_Success(t *testing.T) {
	t.Parallel()
	mux := paypalMux(t)
	mux.HandleFunc("/v2/checkout/orders", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "PP-ORDER-123"})
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/v2/checkout/orders/PP-ORDER-123/capture", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "PP-ORDER-123", "status": "COMPLETED",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	a := paypalAdapter(t, srv.URL)
	res, err := a.Charge(context.Background(), "tenant-a", "order-1",
		port.Money{Amount: 5000, Currency: "AUD"}, port.PaymentMethodPayPal)
	require.NoError(t, err)
	assert.Equal(t, "PP-ORDER-123", res.PaymentID)
	assert.Equal(t, port.PaymentStatusSucceeded, res.Status)
	assert.Equal(t, "paypal", res.Provider)
}

func TestPayPalCapture_Declined(t *testing.T) {
	t.Parallel()
	mux := paypalMux(t)
	mux.HandleFunc("/v2/checkout/orders", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "PP-ORDER-FAIL"})
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/v2/checkout/orders/PP-ORDER-FAIL/capture", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(422)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "INSTRUMENT_DECLINED"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	a := paypalAdapter(t, srv.URL)
	_, err := a.Charge(context.Background(), "tenant-a", "order-1",
		port.Money{Amount: 5000, Currency: "AUD"}, port.PaymentMethodPayPal)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrPaymentDeclined)
}

func TestPayPalRefund_Success(t *testing.T) {
	t.Parallel()
	mux := paypalMux(t)
	mux.HandleFunc("/v2/checkout/orders/PP-ORDER-123", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"purchase_units": []map[string]any{{
				"payments": map[string]any{
					"captures": []map[string]any{{"id": "CAP-456"}},
				},
			}},
		})
	})
	mux.HandleFunc("/v2/payments/captures/CAP-456/refund", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "REF-789", "status": "COMPLETED",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	a := paypalAdapter(t, srv.URL)
	res, err := a.Refund(context.Background(), "tenant-a", "PP-ORDER-123",
		port.Money{Amount: 2500, Currency: "AUD"})
	require.NoError(t, err)
	assert.Equal(t, "REF-789", res.RefundID)
	assert.Equal(t, "succeeded", res.Status)
}

func TestPayPalWebhookVerify_Valid(t *testing.T) {
	t.Parallel()
	mux := paypalMux(t)
	mux.HandleFunc("/v1/notifications/verify-webhook-signature", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"verification_status": "SUCCESS"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	a := paypalAdapter(t, srv.URL)

	body := []byte(`{"id":"WH-1","event_type":"PAYMENT.CAPTURE.COMPLETED","resource":{"id":"CAP-123"}}`)
	headers := http.Header{}
	headers.Set("Paypal-Auth-Algo", "SHA256withRSA")
	headers.Set("Paypal-Cert-Url", "https://example.com/cert")
	headers.Set("Paypal-Transmission-Id", "tx-1")
	headers.Set("Paypal-Transmission-Sig", "sig123")
	headers.Set("Paypal-Transmission-Time", "2026-05-10T10:00:00Z")
	headers.Set("Paypal-Webhook-Id", "WH-ID-1")

	evt, err := a.VerifyWebhook(context.Background(), headers, body)
	require.NoError(t, err)
	assert.Equal(t, "WH-1", evt.EventID)
	assert.Equal(t, "paypal", evt.Provider)
	assert.Equal(t, port.WebhookEventChargeSucceeded, evt.Type)
}

func TestPayPalWebhookVerify_InvalidSignature(t *testing.T) {
	t.Parallel()
	mux := paypalMux(t)
	mux.HandleFunc("/v1/notifications/verify-webhook-signature", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"verification_status": "FAILURE"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	a := paypalAdapter(t, srv.URL)

	headers := http.Header{}
	headers.Set("Paypal-Webhook-Id", "WH-ID-1")
	_, err := a.VerifyWebhook(context.Background(), headers, []byte(`{}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrInvalidWebhookSignature)
}

func TestPayPalAdapter_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ port.MultiPaymentGateway = (*payment.PayPalAdapter)(nil)
}
