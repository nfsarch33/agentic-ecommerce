package port_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

var _ port.MultiPaymentGateway = (*fakeMultiPaymentGateway)(nil)

type fakeMultiPaymentGateway struct{}

func (fakeMultiPaymentGateway) Charge(_ context.Context, _, _ string, _ port.Money, _ port.PaymentMethod) (port.PaymentResult, error) {
	return port.PaymentResult{
		PaymentID:   "pay_test_123",
		ExternalRef: "ext_123",
		Status:      port.PaymentStatusSucceeded,
		Amount:      port.Money{Amount: 1999, Currency: "AUD"},
		Provider:    "stripe",
	}, nil
}

func (fakeMultiPaymentGateway) Refund(_ context.Context, _, _ string, _ port.Money) (port.RefundResult, error) {
	return port.RefundResult{
		RefundID:    "ref_test_123",
		PaymentID:   "pay_test_123",
		ExternalRef: "ext_ref_123",
		Amount:      port.Money{Amount: 1999, Currency: "AUD"},
		Status:      "succeeded",
	}, nil
}

func (fakeMultiPaymentGateway) VerifyWebhook(_ context.Context, _ http.Header, _ []byte) (port.WebhookEvent, error) {
	return port.WebhookEvent{
		EventID:   "evt_test_123",
		Type:      port.WebhookEventChargeSucceeded,
		PaymentID: "pay_test_123",
		Provider:  "stripe",
	}, nil
}

func (fakeMultiPaymentGateway) GetStatus(_ context.Context, _, _ string) (port.PaymentStatus, error) {
	return port.PaymentStatusSucceeded, nil
}

func TestMultiPaymentGatewayCompileContract(t *testing.T) {
	t.Parallel()
	var gw port.MultiPaymentGateway = fakeMultiPaymentGateway{}

	ctx := context.Background()
	amount := port.Money{Amount: 2500, Currency: "AUD"}

	res, err := gw.Charge(ctx, "tenant-a", "order-1", amount, port.PaymentMethodCard)
	if err != nil {
		t.Fatalf("Charge: %v", err)
	}
	if res.Status != port.PaymentStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", res.Status)
	}

	refund, err := gw.Refund(ctx, "tenant-a", res.PaymentID, amount)
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if refund.PaymentID != res.PaymentID {
		t.Fatalf("refund.PaymentID = %q, want %q", refund.PaymentID, res.PaymentID)
	}

	evt, err := gw.VerifyWebhook(ctx, http.Header{}, []byte(`{}`))
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
	if evt.Provider == "" {
		t.Fatal("webhook event provider should not be empty")
	}

	status, err := gw.GetStatus(ctx, "tenant-a", res.PaymentID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status != port.PaymentStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", status)
	}
}

func TestPaymentMethodEnumValues(t *testing.T) {
	t.Parallel()
	methods := []port.PaymentMethod{
		port.PaymentMethodCard,
		port.PaymentMethodAlipay,
		port.PaymentMethodWeChat,
		port.PaymentMethodPayPal,
	}
	seen := make(map[port.PaymentMethod]bool)
	for _, m := range methods {
		if m == "" {
			t.Fatal("empty payment method")
		}
		if seen[m] {
			t.Fatalf("duplicate payment method: %q", m)
		}
		seen[m] = true
	}
}

func TestPaymentStatusEnumValues(t *testing.T) {
	t.Parallel()
	statuses := []port.PaymentStatus{
		port.PaymentStatusPending,
		port.PaymentStatusSucceeded,
		port.PaymentStatusFailed,
		port.PaymentStatusRefunded,
	}
	seen := make(map[port.PaymentStatus]bool)
	for _, s := range statuses {
		if s == "" {
			t.Fatal("empty payment status")
		}
		if seen[s] {
			t.Fatalf("duplicate status: %q", s)
		}
		seen[s] = true
	}
}

func TestPaymentSentinelErrors(t *testing.T) {
	t.Parallel()
	errs := []error{
		port.ErrPaymentDeclined,
		port.ErrInsufficientFunds,
		port.ErrPaymentProviderUnavailable,
		port.ErrInvalidWebhookSignature,
		port.ErrPaymentNotFound,
	}
	for _, e := range errs {
		if e == nil || e.Error() == "" {
			t.Fatal("sentinel error should have non-empty message")
		}
	}
}
