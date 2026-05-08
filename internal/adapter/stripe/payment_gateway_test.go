package stripe_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/stripe"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

type fixedClock struct{ now time.Time }

func (f fixedClock) Now() time.Time { return f.now }

func newGateway() *stripe.PaymentGateway {
	return stripe.NewPaymentGateway(stripe.Config{
		Clock: fixedClock{now: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)},
	})
}

func TestPaymentGatewayCreateSubscriptionDeterministic(t *testing.T) {
	t.Parallel()
	gateway := newGateway()
	ctx := context.Background()
	subID := uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a5")
	memID := uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a6")
	req := port.CreateSubscriptionRequest{
		TenantID:       "tenant-a",
		SubscriptionID: subID,
		MemberID:       memID,
		MemberEmail:    "alice@example.com",
		StripePriceID:  "price_dev_1",
		BillingCycle:   membership.BillingCycleMonthly,
		TrialDays:      0,
	}

	got1, err := gateway.CreateSubscription(ctx, req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	got2, err := gateway.CreateSubscription(ctx, req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got1.StripeSubscriptionID != got2.StripeSubscriptionID {
		t.Fatalf("non-deterministic stripe sub id: %s vs %s", got1.StripeSubscriptionID, got2.StripeSubscriptionID)
	}
	if got1.StripeCustomerID != got2.StripeCustomerID {
		t.Fatalf("non-deterministic customer id: %s vs %s", got1.StripeCustomerID, got2.StripeCustomerID)
	}
	if want := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC); !got1.CurrentPeriodEnd.Equal(want) {
		t.Fatalf("period end = %s, want %s", got1.CurrentPeriodEnd, want)
	}
}

func TestPaymentGatewayCreateSubscriptionTrialOverridesBillingCycle(t *testing.T) {
	t.Parallel()
	gateway := newGateway()
	got, err := gateway.CreateSubscription(context.Background(), port.CreateSubscriptionRequest{
		TenantID: "tenant-a", SubscriptionID: uuid.New(), MemberID: uuid.New(),
		MemberEmail:   "alice@example.com",
		StripePriceID: "price_dev_2",
		BillingCycle:  membership.BillingCycleAnnual,
		TrialDays:     14,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	if !got.CurrentPeriodEnd.Equal(want) {
		t.Fatalf("trial period end = %s, want %s", got.CurrentPeriodEnd, want)
	}
}

func TestPaymentGatewayCreateSubscriptionValidatesInput(t *testing.T) {
	t.Parallel()
	gateway := newGateway()
	cases := []port.CreateSubscriptionRequest{
		{},
		{TenantID: "tenant-a"},
		{TenantID: "tenant-a", MemberEmail: "alice@example.com"},
		{TenantID: "tenant-a", MemberEmail: "alice@example.com", StripePriceID: "price_dev_1"},
	}
	for i, req := range cases {
		req := req
		t.Run("case_"+string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			if _, err := gateway.CreateSubscription(context.Background(), req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestPaymentGatewayCancelAndGetSubscription(t *testing.T) {
	t.Parallel()
	gateway := newGateway()
	ctx := context.Background()
	created, err := gateway.CreateSubscription(ctx, port.CreateSubscriptionRequest{
		TenantID: "tenant-a", SubscriptionID: uuid.New(), MemberID: uuid.New(),
		MemberEmail: "alice@example.com", StripePriceID: "price_dev_3",
		BillingCycle: membership.BillingCycleMonthly,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	status, err := gateway.GetSubscription(ctx, port.GetSubscriptionRequest{
		TenantID: "tenant-a", StripeSubscriptionID: created.StripeSubscriptionID,
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if status.Status != "active" {
		t.Fatalf("status = %s, want active", status.Status)
	}

	if err := gateway.CancelSubscription(ctx, port.CancelSubscriptionRequest{
		TenantID: "tenant-a", StripeSubscriptionID: created.StripeSubscriptionID,
	}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	cancelled, err := gateway.GetSubscription(ctx, port.GetSubscriptionRequest{
		TenantID: "tenant-a", StripeSubscriptionID: created.StripeSubscriptionID,
	})
	if err != nil {
		t.Fatalf("get cancelled: %v", err)
	}
	if cancelled.Status != "canceled" || !cancelled.CancelAtPeriodEnd {
		t.Fatalf("cancelled status = %+v", cancelled)
	}
}

func TestPaymentGatewayCancelUnknownIsIdempotent(t *testing.T) {
	t.Parallel()
	gateway := newGateway()
	if err := gateway.CancelSubscription(context.Background(), port.CancelSubscriptionRequest{
		TenantID: "tenant-a", StripeSubscriptionID: "sub_dev_unknown",
	}); err != nil {
		t.Fatalf("cancel unknown: %v", err)
	}
}

func TestPaymentGatewayMissingStripeIDIsTypedError(t *testing.T) {
	t.Parallel()
	gateway := newGateway()
	if err := gateway.CancelSubscription(context.Background(), port.CancelSubscriptionRequest{TenantID: "tenant-a"}); !errors.Is(err, stripe.ErrMissingStripeID) {
		t.Fatalf("err = %v, want ErrMissingStripeID", err)
	}
	if _, err := gateway.GetSubscription(context.Background(), port.GetSubscriptionRequest{TenantID: "tenant-a"}); !errors.Is(err, stripe.ErrMissingStripeID) {
		t.Fatalf("err = %v, want ErrMissingStripeID", err)
	}
}

func TestPaymentGatewayGetSubscriptionUnknown(t *testing.T) {
	t.Parallel()
	gateway := newGateway()
	if _, err := gateway.GetSubscription(context.Background(), port.GetSubscriptionRequest{
		TenantID: "tenant-a", StripeSubscriptionID: "sub_dev_unknown",
	}); !errors.Is(err, stripe.ErrStripeSubscriptionNotFound) {
		t.Fatalf("err = %v, want ErrStripeSubscriptionNotFound", err)
	}
}
