package payment_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/payment"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGopayBridge_ReturnsStubError(t *testing.T) {
	t.Parallel()
	bridge := payment.NewGopayBridge()

	t.Run("Charge", func(t *testing.T) {
		t.Parallel()
		_, err := bridge.Charge(context.Background(), "t1", "o1",
			port.Money{Amount: 100, Currency: "AUD"}, port.PaymentMethodAlipay)
		require.Error(t, err)
		assert.ErrorIs(t, err, payment.ErrGopayNotAdopted)
	})

	t.Run("Refund", func(t *testing.T) {
		t.Parallel()
		_, err := bridge.Refund(context.Background(), "t1", "p1",
			port.Money{Amount: 100, Currency: "AUD"})
		require.Error(t, err)
		assert.ErrorIs(t, err, payment.ErrGopayNotAdopted)
	})

	t.Run("VerifyWebhook", func(t *testing.T) {
		t.Parallel()
		_, err := bridge.VerifyWebhook(context.Background(), http.Header{}, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, payment.ErrGopayNotAdopted)
	})

	t.Run("GetStatus", func(t *testing.T) {
		t.Parallel()
		_, err := bridge.GetStatus(context.Background(), "t1", "p1")
		require.Error(t, err)
		assert.ErrorIs(t, err, payment.ErrGopayNotAdopted)
	})
}

func TestGopayBridge_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ port.MultiPaymentGateway = (*payment.GopayBridge)(nil)
}
