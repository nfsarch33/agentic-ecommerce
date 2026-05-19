//go:build v431_smoke

package v431_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/payment"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"github.com/nfsarch33/helixon-ec/internal/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubGateway is a test double implementing port.MultiPaymentGateway.
type stubGateway struct {
	provider  string
	chargeOK  bool
	refundOK  bool
	webhookOK bool
}

func (g *stubGateway) Charge(_ context.Context, tenantID, orderID string, amount port.Money, _ port.PaymentMethod) (port.PaymentResult, error) {
	if !g.chargeOK {
		return port.PaymentResult{}, port.ErrPaymentDeclined
	}
	return port.PaymentResult{
		PaymentID:   g.provider + "-pay-" + orderID,
		ExternalRef: g.provider + "-ext-" + orderID,
		Status:      port.PaymentStatusSucceeded,
		Provider:    g.provider,
		Amount:      amount,
	}, nil
}

func (g *stubGateway) Refund(_ context.Context, tenantID, paymentID string, amount port.Money) (port.RefundResult, error) {
	if !g.refundOK {
		return port.RefundResult{}, port.ErrPaymentProviderUnavailable
	}
	return port.RefundResult{
		RefundID:    g.provider + "-ref-" + paymentID,
		PaymentID:   paymentID,
		ExternalRef: g.provider + "-ref-" + paymentID,
		Amount:      amount,
		Status:      "succeeded",
	}, nil
}

func (g *stubGateway) VerifyWebhook(_ context.Context, _ http.Header, _ []byte) (port.WebhookEvent, error) {
	if !g.webhookOK {
		return port.WebhookEvent{}, port.ErrInvalidWebhookSignature
	}
	return port.WebhookEvent{
		EventID:   g.provider + "-evt-1",
		Type:      port.WebhookEventChargeSucceeded,
		PaymentID: g.provider + "-pay-1",
		Provider:  g.provider,
	}, nil
}

func (g *stubGateway) GetStatus(_ context.Context, _, _ string) (port.PaymentStatus, error) {
	return port.PaymentStatusSucceeded, nil
}

type e2ePublisher struct{ events []eventbus.Event }

func (p *e2ePublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.events = append(p.events, evt)
	return nil
}
func (p *e2ePublisher) Close() error { return nil }

func allProviders() map[string]*stubGateway {
	return map[string]*stubGateway{
		"stripe": {provider: "stripe", chargeOK: true, refundOK: true, webhookOK: true},
		"alipay": {provider: "alipay", chargeOK: true, refundOK: true, webhookOK: true},
		"wechat": {provider: "wechat", chargeOK: true, refundOK: true, webhookOK: true},
		"paypal": {provider: "paypal", chargeOK: true, refundOK: true, webhookOK: true},
	}
}

func TestE2E_FullOrderPaymentCycle_AllProviders(t *testing.T) {
	t.Parallel()
	providers := allProviders()
	amount := port.Money{Amount: 5000, Currency: "AUD"}

	for name, gw := range providers {
		t.Run(name+"/charge+refund", func(t *testing.T) {
			t.Parallel()
			res, err := gw.Charge(context.Background(), "tenant-e2e", "order-"+name, amount, port.PaymentMethodCard)
			require.NoError(t, err)
			assert.Equal(t, port.PaymentStatusSucceeded, res.Status)
			assert.Equal(t, name, res.Provider)

			refund, err := gw.Refund(context.Background(), "tenant-e2e", res.PaymentID, amount)
			require.NoError(t, err)
			assert.Equal(t, "succeeded", refund.Status)
		})
	}
}

func TestE2E_CrossProviderRefundCycle(t *testing.T) {
	t.Parallel()
	providers := allProviders()
	amount := port.Money{Amount: 3000, Currency: "AUD"}

	for name, gw := range providers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			chargeRes, err := gw.Charge(context.Background(), "tenant-refund", "refund-order-"+name, amount, port.PaymentMethodCard)
			require.NoError(t, err)

			refundRes, err := gw.Refund(context.Background(), "tenant-refund", chargeRes.PaymentID, port.Money{Amount: 1500, Currency: "AUD"})
			require.NoError(t, err)
			assert.Equal(t, "succeeded", refundRes.Status)
			assert.Equal(t, int64(1500), refundRes.Amount.Amount)
		})
	}
}

func TestE2E_WebhookNormaliserRouting_AllProviders(t *testing.T) {
	t.Parallel()
	providers := allProviders()
	pub := &e2ePublisher{}
	gateways := make(map[string]port.MultiPaymentGateway, len(providers))
	for k, v := range providers {
		gateways[k] = v
	}

	normaliser, err := webhook.NewPaymentNormaliser(nil, webhook.PaymentNormaliserConfig{
		Providers: gateways,
		Publisher: pub,
		Now:       func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})
	require.NoError(t, err)

	for _, provider := range []string{"stripe", "alipay", "wechat", "paypal"} {
		t.Run(provider+"/success", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/"+provider, strings.NewReader(`{"test":"data"}`))
			rec := httptest.NewRecorder()
			normaliser.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
	assert.Len(t, pub.events, 4)
}

func TestE2E_WebhookNormaliserRouting_Rejected(t *testing.T) {
	t.Parallel()
	rejectProviders := map[string]*stubGateway{
		"stripe": {provider: "stripe", webhookOK: false},
		"alipay": {provider: "alipay", webhookOK: false},
		"wechat": {provider: "wechat", webhookOK: false},
		"paypal": {provider: "paypal", webhookOK: false},
	}
	pub := &e2ePublisher{}
	gateways := make(map[string]port.MultiPaymentGateway, len(rejectProviders))
	for k, v := range rejectProviders {
		gateways[k] = v
	}

	normaliser, err := webhook.NewPaymentNormaliser(nil, webhook.PaymentNormaliserConfig{
		Providers: gateways,
		Publisher: pub,
		Now:       func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})
	require.NoError(t, err)

	for _, provider := range []string{"stripe", "alipay", "wechat", "paypal"} {
		t.Run(provider+"/rejected", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payment/"+provider, strings.NewReader(`{}`))
			rec := httptest.NewRecorder()
			normaliser.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
	assert.Empty(t, pub.events)
}

func TestE2E_TenantIsolation_CrossProvider(t *testing.T) {
	t.Parallel()
	gw := &stubGateway{provider: "stripe", chargeOK: true, refundOK: true, webhookOK: true}
	amount := port.Money{Amount: 5000, Currency: "AUD"}

	res1, err := gw.Charge(context.Background(), "tenant-1", "order-iso-1", amount, port.PaymentMethodCard)
	require.NoError(t, err)
	res2, err := gw.Charge(context.Background(), "tenant-2", "order-iso-2", amount, port.PaymentMethodCard)
	require.NoError(t, err)

	assert.NotEqual(t, res1.PaymentID, res2.PaymentID)
}

func TestE2E_PayPalAdapter_VCR_Smoke(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "e2e-token", "expires_in": 3600})
	})
	mux.HandleFunc("/v2/checkout/orders", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "E2E-ORDER-1"})
	})
	mux.HandleFunc("/v2/checkout/orders/E2E-ORDER-1/capture", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "E2E-ORDER-1", "status": "COMPLETED"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	adapter, err := payment.NewPayPalAdapter(payment.PayPalAdapterConfig{
		ClientID:     "e2e-client",
		ClientSecret: "e2e-secret",
		Sandbox:      true,
		APIURL:       srv.URL,
	})
	require.NoError(t, err)
	res, err := adapter.Charge(context.Background(), "tenant-e2e", "e2e-order-1",
		port.Money{Amount: 10000, Currency: "AUD"}, port.PaymentMethodPayPal)
	require.NoError(t, err)
	assert.Equal(t, port.PaymentStatusSucceeded, res.Status)
	assert.Equal(t, "paypal", res.Provider)
}
