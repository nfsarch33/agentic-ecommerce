package billing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestService(t *testing.T) (*Service, *InMemoryRepository, *NoopPublisher) {
	t.Helper()
	repo := NewInMemoryRepository()
	pub := &NoopPublisher{}
	svc, err := NewService(ServiceConfig{
		Repository: repo,
		Plans:      NewStaticPlanCatalog(),
		Publisher:  pub,
		Now:        func() time.Time { return time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, repo, pub
}

func TestNewServiceValidation(t *testing.T) {
	t.Parallel()
	if _, err := NewService(ServiceConfig{}); err == nil {
		t.Fatalf("expected error for empty config")
	}
	if _, err := NewService(ServiceConfig{Repository: NewInMemoryRepository()}); err == nil {
		t.Fatalf("expected error without plans")
	}
}

func TestServiceCreateSubscription(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newTestService(t)
	in := NewSubscriptionInput{ID: "sub_1", TenantID: "tenant-a", PlanID: "starter"}
	got, err := svc.CreateSubscription(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.State != StateTrialing {
		t.Fatalf("state = %s", got.State)
	}
	round, _ := repo.GetSubscription(ctx, "tenant-a", "sub_1")
	if round.PlanID != "starter" {
		t.Fatalf("plan_id = %s", round.PlanID)
	}
}

func TestServiceCreateSubscriptionPlanNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	_, err := svc.CreateSubscription(ctx, NewSubscriptionInput{
		ID: "sub_1", TenantID: "tenant-a", PlanID: "made-up",
	})
	if !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestServiceCancelPauseResume(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	_, _ = svc.CreateSubscription(ctx, NewSubscriptionInput{ID: "sub_1", TenantID: "tenant-a", PlanID: "free"})
	if _, err := svc.Activate(ctx, "tenant-a", "sub_1"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if _, err := svc.Pause(ctx, "tenant-a", "sub_1"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := svc.Resume(ctx, "tenant-a", "sub_1"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if _, err := svc.Cancel(ctx, "tenant-a", "sub_1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, err := svc.Cancel(ctx, "tenant-a", "sub_1"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition double-cancel, got %v", err)
	}
}

func TestServiceTenantRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	if _, err := svc.GetSubscription(ctx, "", "x"); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
	if _, err := svc.ListSubscriptions(ctx, "", 1, 10); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired on List, got %v", err)
	}
	if _, err := svc.Cancel(ctx, "", "x"); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired on Cancel, got %v", err)
	}
}

func TestServiceUpsertSubscriptionFromStripe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newTestService(t)
	in := NewSubscriptionInput{
		ID: "sub_stripe_1", TenantID: "tenant-a", PlanID: "starter",
		StripeSubscriptionID: "sub_stripe_1", State: StateActive,
		CurrentPeriodStart: time.Now().UTC(), CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour).UTC(),
	}
	out1, err := svc.UpsertSubscriptionFromStripe(ctx, in, "subscription.created")
	if err != nil {
		t.Fatalf("Upsert insert: %v", err)
	}
	in2 := in
	in2.State = StatePastDue
	out2, err := svc.UpsertSubscriptionFromStripe(ctx, in2, "subscription.updated")
	if err != nil {
		t.Fatalf("Upsert update: %v", err)
	}
	if out2.State != StatePastDue {
		t.Fatalf("state after update = %s", out2.State)
	}
	if out1.ID != out2.ID {
		t.Fatalf("upsert created two rows: %s vs %s", out1.ID, out2.ID)
	}
	round, _ := repo.GetSubscriptionByStripeID(ctx, "tenant-a", "sub_stripe_1")
	if round.State != StatePastDue {
		t.Fatalf("persisted state = %s", round.State)
	}
}

func TestServiceUpsertInvoice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newTestService(t)
	inv := Invoice{ID: "inv_1", TenantID: "tenant-a", SubscriptionID: "sub_1", Amount: 1900, Currency: "AUD"}
	if _, err := svc.UpsertInvoice(ctx, inv, "invoice.paid"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	round, err := repo.GetInvoice(ctx, "tenant-a", "inv_1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if round.Status != InvoiceOpen {
		t.Fatalf("default status = %s, want open", round.Status)
	}
}
