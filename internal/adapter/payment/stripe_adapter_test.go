package payment_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/payment"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stripeTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func stripeAdapter(t *testing.T, apiURL string) *payment.StripeAdapter {
	t.Helper()
	secret := strings.Repeat("a", 32)
	a, err := payment.NewStripeAdapter(payment.StripeAdapterConfig{
		APIURL:        apiURL,
		APIKey:        "sk_test_fake",
		WebhookSecret: []byte(secret),
	})
	require.NoError(t, err)
	return a
}

func TestStripeCharge_Success(t *testing.T) {
	t.Parallel()
	srv := stripeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/payment_intents" && r.Method == http.MethodPost {
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "pi_test_123", "status": "succeeded",
				"amount": 2500, "currency": "aud",
			})
			return
		}
		http.NotFound(w, r)
	})
	a := stripeAdapter(t, srv.URL)
	res, err := a.Charge(context.Background(), "tenant-a", "order-1",
		port.Money{Amount: 2500, Currency: "AUD"}, port.PaymentMethodCard)
	require.NoError(t, err)
	assert.Equal(t, "pi_test_123", res.PaymentID)
	assert.Equal(t, port.PaymentStatusSucceeded, res.Status)
	assert.Equal(t, "stripe", res.Provider)
}

func TestStripeCharge_Declined(t *testing.T) {
	t.Parallel()
	srv := stripeTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(402)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"code": "card_declined"},
		})
	})
	a := stripeAdapter(t, srv.URL)
	_, err := a.Charge(context.Background(), "tenant-a", "order-1",
		port.Money{Amount: 2500, Currency: "AUD"}, port.PaymentMethodCard)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrPaymentDeclined)
}

func TestStripeRefund_Success(t *testing.T) {
	t.Parallel()
	srv := stripeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/refunds" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "re_test_456", "payment_intent": "pi_test_123",
				"amount": 1000, "status": "succeeded", "currency": "aud",
			})
			return
		}
		http.NotFound(w, r)
	})
	a := stripeAdapter(t, srv.URL)
	res, err := a.Refund(context.Background(), "tenant-a", "pi_test_123",
		port.Money{Amount: 1000, Currency: "AUD"})
	require.NoError(t, err)
	assert.Equal(t, "re_test_456", res.RefundID)
	assert.Equal(t, int64(1000), res.Amount.Amount)
}

func TestStripeWebhookVerify_Valid(t *testing.T) {
	t.Parallel()
	secret := strings.Repeat("a", 32)
	a, err := payment.NewStripeAdapter(payment.StripeAdapterConfig{
		APIURL:        "https://api.stripe.com",
		APIKey:        "sk_test_fake",
		WebhookSecret: []byte(secret),
	})
	require.NoError(t, err)

	body := []byte(`{"id":"evt_1","type":"payment_intent.succeeded","data":{"object":{"id":"pi_123"}}}`)
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	sig := fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))

	headers := http.Header{}
	headers.Set("Stripe-Signature", sig)
	evt, err := a.VerifyWebhook(context.Background(), headers, body)
	require.NoError(t, err)
	assert.Equal(t, "evt_1", evt.EventID)
	assert.Equal(t, "stripe", evt.Provider)
	assert.Equal(t, "pi_123", evt.PaymentID)
}

func TestStripeWebhookVerify_InvalidSignature(t *testing.T) {
	t.Parallel()
	a := stripeAdapter(t, "https://api.stripe.com")
	headers := http.Header{}
	headers.Set("Stripe-Signature", "t=0,v1=badsig")
	_, err := a.VerifyWebhook(context.Background(), headers, []byte(`{}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrInvalidWebhookSignature)
}

func TestStripeGetStatus_Success(t *testing.T) {
	t.Parallel()
	srv := stripeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/payment_intents/") {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "succeeded"})
			return
		}
		http.NotFound(w, r)
	})
	a := stripeAdapter(t, srv.URL)
	status, err := a.GetStatus(context.Background(), "tenant-a", "pi_test_123")
	require.NoError(t, err)
	assert.Equal(t, port.PaymentStatusSucceeded, status)
}

func TestStripeAdapter_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ port.MultiPaymentGateway = (*payment.StripeAdapter)(nil)
}
