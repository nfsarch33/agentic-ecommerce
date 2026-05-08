package billing

import (
	"context"
	"errors"
	"testing"
)

func TestServiceMarkPastDue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	if _, err := svc.CreateSubscription(ctx, NewSubscriptionInput{ID: "sub_1", TenantID: "tenant-a", PlanID: "free"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Activate(ctx, "tenant-a", "sub_1"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	got, err := svc.MarkPastDue(ctx, "tenant-a", "sub_1")
	if err != nil {
		t.Fatalf("MarkPastDue: %v", err)
	}
	if got.State != StatePastDue {
		t.Fatalf("state = %s", got.State)
	}
}

func TestServiceActivateFromPastDue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	if _, err := svc.CreateSubscription(ctx, NewSubscriptionInput{ID: "sub_1", TenantID: "tenant-a", PlanID: "free"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Activate(ctx, "tenant-a", "sub_1"); err != nil {
		t.Fatalf("first activate: %v", err)
	}
	if _, err := svc.MarkPastDue(ctx, "tenant-a", "sub_1"); err != nil {
		t.Fatalf("MarkPastDue: %v", err)
	}
	got, err := svc.Activate(ctx, "tenant-a", "sub_1")
	if err != nil {
		t.Fatalf("recover via Activate: %v", err)
	}
	if got.State != StateActive {
		t.Fatalf("recovered state = %s", got.State)
	}
}

func TestServiceUpsertInvoiceTenantRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	if _, err := svc.UpsertInvoice(ctx, Invoice{ID: "inv_1"}, "invoice.paid"); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
}

func TestServiceGetInvoiceTenantRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := newTestService(t)
	if _, err := svc.GetInvoice(ctx, "", "x"); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
	if _, err := svc.ListInvoices(ctx, "", 1, 10); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
}

func TestSnapshotRequiresMeter(t *testing.T) {
	t.Parallel()
	if _, err := Snapshot(context.Background(), nil, Plan{}, "tenant-a", testTime(), testTime()); err == nil {
		t.Fatalf("expected error for nil meter")
	}
}
