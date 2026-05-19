package payment_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/adapter/payment"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllAdapters_ChargeValidation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "dummy"})
	}))
	t.Cleanup(srv.Close)

	stripeA := stripeAdapter(t, srv.URL)
	alipayA := alipayAdapter(t, srv.URL)
	wechatA := wechatAdapter(t, srv.URL)
	paypalA := paypalAdapter(t, srv.URL)

	adapters := []struct {
		name    string
		gateway port.MultiPaymentGateway
	}{
		{"stripe", stripeA},
		{"alipay", alipayA},
		{"wechat", wechatA},
		{"paypal", paypalA},
	}

	validationCases := []struct {
		caseName string
		tenantID string
		orderID  string
		amount   port.Money
		wantErr  error
	}{
		{"empty_tenant", "", "order-1", port.Money{Amount: 100, Currency: "AUD"}, port.ErrPaymentDeclined},
		{"empty_order", "tenant-a", "", port.Money{Amount: 100, Currency: "AUD"}, port.ErrPaymentDeclined},
		{"zero_amount", "tenant-a", "order-1", port.Money{Amount: 0, Currency: "AUD"}, port.ErrPaymentDeclined},
		{"negative_amount", "tenant-a", "order-1", port.Money{Amount: -5, Currency: "AUD"}, port.ErrPaymentDeclined},
		{"empty_currency", "tenant-a", "order-1", port.Money{Amount: 100, Currency: ""}, port.ErrPaymentDeclined},
	}

	for _, adapter := range adapters {
		for _, tc := range validationCases {
			t.Run(adapter.name+"/"+tc.caseName, func(t *testing.T) {
				t.Parallel()
				_, err := adapter.gateway.Charge(
					context.Background(), tc.tenantID, tc.orderID, tc.amount, port.PaymentMethodCard,
				)
				require.Error(t, err)
				assert.ErrorIs(t, err, tc.wantErr)
			})
		}
	}
}

func TestAllAdapters_GetStatusEmptyPaymentID(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	adapters := []struct {
		name    string
		gateway port.MultiPaymentGateway
	}{
		{"stripe", stripeAdapter(t, srv.URL)},
		{"alipay", alipayAdapter(t, srv.URL)},
		{"wechat", wechatAdapter(t, srv.URL)},
		{"paypal", paypalAdapter(t, srv.URL)},
	}

	for _, adapter := range adapters {
		t.Run(adapter.name, func(t *testing.T) {
			t.Parallel()
			_, err := adapter.gateway.GetStatus(context.Background(), "tenant-a", "")
			require.Error(t, err)
			assert.ErrorIs(t, err, port.ErrPaymentNotFound)
		})
	}
}

func TestAllAdapters_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ port.MultiPaymentGateway = (*payment.StripeAdapter)(nil)
	var _ port.MultiPaymentGateway = (*payment.AlipayAdapter)(nil)
	var _ port.MultiPaymentGateway = (*payment.WeChatAdapter)(nil)
	var _ port.MultiPaymentGateway = (*payment.PayPalAdapter)(nil)
}
