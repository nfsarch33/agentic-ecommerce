package billing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInMemoryRepositorySubscriptionRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	sub, err := NewSubscription(NewSubscriptionInput{
		ID: "sub_1", TenantID: "tenant-a", PlanID: "free",
		StripeSubscriptionID: "sub_stripe_1", Now: now,
	})
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if err := repo.CreateSubscription(ctx, sub); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.CreateSubscription(ctx, sub); !errors.Is(err, ErrSubscriptionAlreadyExists) {
		t.Fatalf("expected ErrSubscriptionAlreadyExists, got %v", err)
	}
	got, err := repo.GetSubscription(ctx, "tenant-a", "sub_1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "sub_1" {
		t.Fatalf("got id=%q want sub_1", got.ID)
	}
	stripeGot, err := repo.GetSubscriptionByStripeID(ctx, "tenant-a", "sub_stripe_1")
	if err != nil {
		t.Fatalf("GetByStripeID: %v", err)
	}
	if stripeGot.ID != "sub_1" {
		t.Fatalf("stripe lookup mismatch: %q", stripeGot.ID)
	}
	got.State = StateActive
	if err := repo.SaveSubscription(ctx, got); err != nil {
		t.Fatalf("Save: %v", err)
	}
	round, _ := repo.GetSubscription(ctx, "tenant-a", "sub_1")
	if round.State != StateActive {
		t.Fatalf("state not persisted: %s", round.State)
	}
}

func TestInMemoryRepositoryListPagination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		sub, _ := NewSubscription(NewSubscriptionInput{
			ID: "sub_" + string(rune('a'+i)), TenantID: "tenant-a", PlanID: "free",
			Now: now.Add(time.Duration(i) * time.Minute),
		})
		if err := repo.CreateSubscription(ctx, sub); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	list, err := repo.ListSubscriptions(ctx, "tenant-a", 1, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Subscriptions) != 2 {
		t.Fatalf("page size = %d, want 2", len(list.Subscriptions))
	}
	if list.Total != 5 {
		t.Fatalf("total = %d, want 5", list.Total)
	}
}

func TestInMemoryRepositoryStripeIdempotency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	seen, err := repo.StripeEventSeen(ctx, "evt_1")
	if err != nil || seen {
		t.Fatalf("seen first time: seen=%v err=%v", seen, err)
	}
	if err := repo.StripeEventRecord(ctx, "evt_1", "subscription.created", time.Now()); err != nil {
		t.Fatalf("Record: %v", err)
	}
	seen, _ = repo.StripeEventSeen(ctx, "evt_1")
	if !seen {
		t.Fatalf("expected seen after Record")
	}
}

func TestInMemoryRepositoryTenantRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	if _, err := repo.GetSubscription(ctx, "", "x"); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
	if err := repo.SaveSubscription(ctx, Subscription{ID: "x"}); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired on Save, got %v", err)
	}
}

func TestInMemoryRepositoryInvoiceUpsert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	inv := Invoice{
		ID: "inv_1", TenantID: "tenant-a", SubscriptionID: "sub_1",
		Amount: 1900, Currency: "AUD", Status: InvoiceOpen,
		PeriodStart: now, PeriodEnd: now.Add(30 * 24 * time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.UpsertInvoice(ctx, inv); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	inv.Status = InvoicePaid
	if err := repo.UpsertInvoice(ctx, inv); err != nil {
		t.Fatalf("Upsert again: %v", err)
	}
	got, err := repo.GetInvoice(ctx, "tenant-a", "inv_1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != InvoicePaid {
		t.Fatalf("status = %s, want paid", got.Status)
	}
}

func TestStaticPlanCatalog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := NewStaticPlanCatalog()
	plans, err := cat.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(plans) != 3 {
		t.Fatalf("len(plans) = %d, want 3", len(plans))
	}
	free, err := cat.Get(ctx, "free")
	if err != nil {
		t.Fatalf("Get free: %v", err)
	}
	if free.APIRatePerMinute != 60 {
		t.Fatalf("free api rate = %d, want 60", free.APIRatePerMinute)
	}
	if _, err := cat.Get(ctx, "made-up"); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestInMemoryUsageMeter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	meter := NewInMemoryUsageMeter()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	if err := meter.Record(ctx, "tenant-a", MetricAPIRequests, 5, now); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := meter.Record(ctx, "tenant-a", MetricAPIRequests, 3, now.Add(time.Minute)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	total, err := meter.Sum(ctx, "tenant-a", MetricAPIRequests, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Sum: %v", err)
	}
	if total != 8 {
		t.Fatalf("total = %d, want 8", total)
	}
	if _, err := meter.Sum(ctx, "", MetricAPIRequests, time.Time{}, time.Time{}); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
}

func TestSnapshotRollup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	meter := NewInMemoryUsageMeter()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	plan := DefaultSeedPlans()[1]
	_ = meter.Record(ctx, "tenant-a", MetricAPIRequests, 100, now)
	rollup, err := Snapshot(ctx, meter, plan, "tenant-a", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(rollup) != 3 {
		t.Fatalf("rollup len = %d, want 3", len(rollup))
	}
	if rollup[0].Metric != MetricAPIRequests || rollup[0].Value != 100 {
		t.Fatalf("api rollup = %+v", rollup[0])
	}
}
