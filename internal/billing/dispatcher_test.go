package billing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const stripeSubscriptionEvent = `{
  "id": "evt_1",
  "type": "customer.subscription.created",
  "created": 1778240000,
  "data": {
    "object": {
      "id": "sub_stripe_1",
      "status": "active",
      "customer": "cus_1",
      "current_period_start": 1778240000,
      "current_period_end": 1780832000,
      "cancel_at_period_end": false,
      "metadata": {"tenant_id": "tenant-a", "plan_id": "starter"}
    }
  }
}`

const stripeInvoicePaidEvent = `{
  "id": "evt_inv",
  "type": "invoice.payment_succeeded",
  "created": 1778240500,
  "data": {
    "object": {
      "id": "inv_1",
      "subscription": "sub_stripe_1",
      "customer": "cus_1",
      "amount_due": 1900,
      "amount_paid": 1900,
      "currency": "AUD",
      "status": "paid",
      "period_start": 1778240000,
      "period_end": 1780832000,
      "metadata": {"tenant_id": "tenant-a", "plan_id": "starter"}
    }
  }
}`

const stripeInvoiceFailedEvent = `{
  "id": "evt_inv_fail",
  "type": "invoice.payment_failed",
  "created": 1778240600,
  "data": {
    "object": {
      "id": "inv_2",
      "subscription": "sub_stripe_1",
      "customer": "cus_1",
      "amount_due": 1900,
      "amount_paid": 0,
      "currency": "AUD",
      "status": "open",
      "period_start": 1778240000,
      "period_end": 1780832000,
      "metadata": {"tenant_id": "tenant-a", "plan_id": "starter"}
    }
  }
}`

const stripeSubscriptionDeletedEvent = `{
  "id": "evt_del",
  "type": "customer.subscription.deleted",
  "created": 1778240700,
  "data": {
    "object": {
      "id": "sub_stripe_1",
      "status": "canceled",
      "customer": "cus_1",
      "current_period_start": 1778240000,
      "current_period_end": 1780832000,
      "metadata": {"tenant_id": "tenant-a", "plan_id": "starter"}
    }
  }
}`

func TestDispatcherSubscriptionCreated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newTestService(t)
	d := NewDispatcher(svc)
	id, err := d.Dispatch(ctx, []byte(stripeSubscriptionEvent))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if id != "evt_1" {
		t.Fatalf("returned id = %q", id)
	}
	got, err := repo.GetSubscriptionByStripeID(ctx, "tenant-a", "sub_stripe_1")
	if err != nil {
		t.Fatalf("GetByStripeID: %v", err)
	}
	if got.State != StateActive {
		t.Fatalf("state = %s, want active", got.State)
	}
}

func TestDispatcherInvoicePaid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newTestService(t)
	d := NewDispatcher(svc)
	if _, err := d.Dispatch(ctx, []byte(stripeSubscriptionEvent)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.transition(ctx, "tenant-a", "sub_stripe_1", TransitionMarkPastDue, "subscription.updated"); err != nil {
		t.Fatalf("force past_due: %v", err)
	}
	if _, err := d.Dispatch(ctx, []byte(stripeInvoicePaidEvent)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	inv, err := repo.GetInvoice(ctx, "tenant-a", "inv_1")
	if err != nil {
		t.Fatalf("GetInvoice: %v", err)
	}
	if inv.Status != InvoicePaid {
		t.Fatalf("invoice status = %s", inv.Status)
	}
	sub, _ := repo.GetSubscriptionByStripeID(ctx, "tenant-a", "sub_stripe_1")
	if sub.State != StateActive {
		t.Fatalf("subscription state after invoice paid = %s, want active", sub.State)
	}
}

func TestDispatcherInvoiceFailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newTestService(t)
	d := NewDispatcher(svc)
	if _, err := d.Dispatch(ctx, []byte(stripeSubscriptionEvent)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := d.Dispatch(ctx, []byte(stripeInvoiceFailedEvent)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	sub, _ := repo.GetSubscriptionByStripeID(ctx, "tenant-a", "sub_stripe_1")
	if sub.State != StatePastDue {
		t.Fatalf("subscription state after failed = %s, want past_due", sub.State)
	}
}

func TestDispatcherSubscriptionDeleted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newTestService(t)
	d := NewDispatcher(svc)
	if _, err := d.Dispatch(ctx, []byte(stripeSubscriptionEvent)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := d.Dispatch(ctx, []byte(stripeSubscriptionDeletedEvent)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	sub, _ := repo.GetSubscriptionByStripeID(ctx, "tenant-a", "sub_stripe_1")
	if sub.State != StateCanceled {
		t.Fatalf("subscription state after delete = %s, want canceled", sub.State)
	}
}

func TestDispatcherUnknownEventNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	d := NewDispatcher(svc)
	id, err := d.Dispatch(ctx, []byte(`{"id":"evt_x","type":"customer.tax_id.created","data":{"object":{}}}`))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if id != "evt_x" {
		t.Fatalf("id = %q, want evt_x", id)
	}
}

func TestDispatcherInvalidJSON(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	d := NewDispatcher(svc)
	if _, err := d.Dispatch(ctx, []byte(`{`)); err == nil {
		t.Fatalf("expected error for malformed json")
	}
}

func TestDispatcherSubscriptionMissingTenantMeta(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	d := NewDispatcher(svc)
	bad := strings.Replace(stripeSubscriptionEvent, `"tenant_id": "tenant-a"`, `"tenant_id": ""`, 1)
	if _, err := d.Dispatch(ctx, []byte(bad)); err == nil {
		t.Fatalf("expected error for missing tenant id metadata")
	}
}

func TestMapStripeStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want State
	}{
		{"trialing", StateTrialing},
		{"active", StateActive},
		{"past_due", StatePastDue},
		{"unpaid", StatePastDue},
		{"canceled", StateCanceled},
		{"incomplete_expired", StateCanceled},
		{"paused", StatePaused},
		{"unknown", StateActive},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := mapStripeStatus(tc.in); got != tc.want {
				t.Fatalf("mapStripeStatus(%q) = %s want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestStripeEventEnvelopeDecode(t *testing.T) {
	t.Parallel()
	var env stripeEventEnvelope
	if err := json.Unmarshal([]byte(stripeSubscriptionEvent), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.ID != "evt_1" {
		t.Fatalf("env id = %q", env.ID)
	}
	if env.Type != StripeSubscriptionCreated {
		t.Fatalf("env type = %q", env.Type)
	}
	if !time.Unix(env.Created, 0).Before(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("created sanity")
	}
}
