package webhook_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/nfsarch33/agentic-ecommerce/internal/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPaymentGateway struct {
	verifyErr error
	verifyEvt port.WebhookEvent
}

func (m *mockPaymentGateway) Charge(context.Context, string, string, port.Money, port.PaymentMethod) (port.PaymentResult, error) {
	return port.PaymentResult{}, nil
}

func (m *mockPaymentGateway) Refund(context.Context, string, string, port.Money) (port.RefundResult, error) {
	return port.RefundResult{}, nil
}

func (m *mockPaymentGateway) VerifyWebhook(_ context.Context, _ http.Header, _ []byte) (port.WebhookEvent, error) {
	return m.verifyEvt, m.verifyErr
}

func (m *mockPaymentGateway) GetStatus(context.Context, string, string) (port.PaymentStatus, error) {
	return port.PaymentStatusPending, nil
}

type spyPublisher struct {
	events []eventbus.Event
}

func (s *spyPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	s.events = append(s.events, evt)
	return nil
}

func (s *spyPublisher) Close() error { return nil }

type spyMetrics struct {
	calls []string
}

func (s *spyMetrics) IncWebhookNormalised(provider, outcome string) {
	s.calls = append(s.calls, provider+":"+outcome)
}

func buildNormaliser(t *testing.T, providers map[string]*mockPaymentGateway) (*webhook.PaymentNormaliser, *spyPublisher, *spyMetrics) {
	t.Helper()
	pub := &spyPublisher{}
	metrics := &spyMetrics{}
	gateways := make(map[string]port.MultiPaymentGateway, len(providers))
	for k, v := range providers {
		gateways[k] = v
	}
	n, err := webhook.NewPaymentNormaliser(nil, webhook.PaymentNormaliserConfig{
		Providers: gateways,
		Publisher: pub,
		Now:       func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
		Metrics:   metrics,
	})
	require.NoError(t, err)
	return n, pub, metrics
}

func successGateway(provider string) *mockPaymentGateway {
	return &mockPaymentGateway{
		verifyEvt: port.WebhookEvent{
			EventID: "evt-1", Type: port.WebhookEventChargeSucceeded,
			PaymentID: "pay-123", Provider: provider, RawJSON: []byte(`{}`),
		},
	}
}

func rejectGateway() *mockPaymentGateway {
	return &mockPaymentGateway{verifyErr: port.ErrInvalidWebhookSignature}
}

func TestPaymentNormaliser_StripeVerified(t *testing.T) {
	t.Parallel()
	n, pub, metrics := buildNormaliser(t, map[string]*mockPaymentGateway{"stripe": successGateway("stripe")})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/stripe", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	n.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, pub.events, 1)
	assert.Contains(t, metrics.calls, "stripe:accepted")
}

func TestPaymentNormaliser_StripeRejected(t *testing.T) {
	t.Parallel()
	n, pub, metrics := buildNormaliser(t, map[string]*mockPaymentGateway{"stripe": rejectGateway()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/stripe", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	n.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, pub.events)
	assert.Contains(t, metrics.calls, "stripe:rejected")
}

func TestPaymentNormaliser_AlipayVerified(t *testing.T) {
	t.Parallel()
	n, pub, metrics := buildNormaliser(t, map[string]*mockPaymentGateway{"alipay": successGateway("alipay")})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/alipay", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	n.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, pub.events, 1)
	assert.Contains(t, metrics.calls, "alipay:accepted")
}

func TestPaymentNormaliser_AlipayRejected(t *testing.T) {
	t.Parallel()
	n, _, metrics := buildNormaliser(t, map[string]*mockPaymentGateway{"alipay": rejectGateway()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/alipay", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	n.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, metrics.calls, "alipay:rejected")
}

func TestPaymentNormaliser_WechatVerified(t *testing.T) {
	t.Parallel()
	n, pub, metrics := buildNormaliser(t, map[string]*mockPaymentGateway{"wechat": successGateway("wechat")})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/wechat", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	n.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, pub.events, 1)
	assert.Contains(t, metrics.calls, "wechat:accepted")
}

func TestPaymentNormaliser_WechatRejected(t *testing.T) {
	t.Parallel()
	n, _, metrics := buildNormaliser(t, map[string]*mockPaymentGateway{"wechat": rejectGateway()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/wechat", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	n.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, metrics.calls, "wechat:rejected")
}

func TestPaymentNormaliser_PaypalVerified(t *testing.T) {
	t.Parallel()
	n, pub, metrics := buildNormaliser(t, map[string]*mockPaymentGateway{"paypal": successGateway("paypal")})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/paypal", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	n.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, pub.events, 1)
	assert.Contains(t, metrics.calls, "paypal:accepted")
}

func TestPaymentNormaliser_PaypalRejected(t *testing.T) {
	t.Parallel()
	n, _, metrics := buildNormaliser(t, map[string]*mockPaymentGateway{"paypal": rejectGateway()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/paypal", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	n.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, metrics.calls, "paypal:rejected")
}

func TestPaymentNormaliser_UnknownProvider(t *testing.T) {
	t.Parallel()
	n, _, _ := buildNormaliser(t, map[string]*mockPaymentGateway{"stripe": successGateway("stripe")})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/bitcoin", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	n.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPaymentNormaliser_Idempotency(t *testing.T) {
	t.Parallel()
	n, pub, _ := buildNormaliser(t, map[string]*mockPaymentGateway{"stripe": successGateway("stripe")})
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/stripe", strings.NewReader(`{}`))
	rec1 := httptest.NewRecorder()
	n.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Len(t, pub.events, 1)

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/stripe", strings.NewReader(`{}`))
	rec2 := httptest.NewRecorder()
	n.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Len(t, pub.events, 1)
}
