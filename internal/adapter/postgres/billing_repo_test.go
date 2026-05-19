package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/helixon-ec/internal/billing"
)

func fakeBillingSubscriptionRow(sub billing.Subscription) fakeRow {
	var startPtr, endPtr *time.Time
	if !sub.CurrentPeriodStart.IsZero() {
		t := sub.CurrentPeriodStart
		startPtr = &t
	}
	if !sub.CurrentPeriodEnd.IsZero() {
		t := sub.CurrentPeriodEnd
		endPtr = &t
	}
	return fakeRow{values: []any{
		sub.ID, sub.TenantID, sub.PlanID, string(sub.State),
		sub.StripeSubscriptionID, sub.StripeCustomerID,
		startPtr, endPtr, sub.CancelAtPeriodEnd,
		sub.CreatedAt, sub.UpdatedAt,
	}}
}

func fakeBillingInvoiceRow(inv billing.Invoice) fakeRow {
	var startPtr, endPtr *time.Time
	if !inv.PeriodStart.IsZero() {
		t := inv.PeriodStart
		startPtr = &t
	}
	if !inv.PeriodEnd.IsZero() {
		t := inv.PeriodEnd
		endPtr = &t
	}
	return fakeRow{values: []any{
		inv.ID, inv.TenantID, inv.SubscriptionID,
		inv.Amount, inv.Currency, string(inv.Status),
		startPtr, endPtr, inv.CreatedAt, inv.UpdatedAt,
	}}
}

func TestBillingRepositoryCreateSubscriptionSuccess(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &BillingRepository{pool: pool}
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	sub, _ := billing.NewSubscription(billing.NewSubscriptionInput{
		ID: "sub_1", TenantID: "tenant-a", PlanID: "free", Now: now,
	})
	if err := repo.CreateSubscription(context.Background(), sub); err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if len(pool.execSQL) != 1 {
		t.Fatalf("expected 1 exec, got %d", len(pool.execSQL))
	}
}

func TestBillingRepositoryCreateSubscriptionTenantRequired(t *testing.T) {
	t.Parallel()
	repo := &BillingRepository{pool: &fakePool{}}
	if err := repo.CreateSubscription(context.Background(), billing.Subscription{}); !errors.Is(err, billing.ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
}

func TestBillingRepositoryCreateSubscriptionUnique(t *testing.T) {
	t.Parallel()
	pool := &fakePool{execErr: errors.New("ERROR: 23505 duplicate key")}
	repo := &BillingRepository{pool: pool}
	sub, _ := billing.NewSubscription(billing.NewSubscriptionInput{ID: "sub_1", TenantID: "tenant-a", PlanID: "free"})
	if err := repo.CreateSubscription(context.Background(), sub); !errors.Is(err, billing.ErrSubscriptionAlreadyExists) {
		t.Fatalf("expected ErrSubscriptionAlreadyExists, got %v", err)
	}
}

func TestBillingRepositoryGetSubscription(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	sub, _ := billing.NewSubscription(billing.NewSubscriptionInput{
		ID: "sub_1", TenantID: "tenant-a", PlanID: "free", Now: now,
		StripeSubscriptionID: "sub_stripe_1",
	})
	pool := &fakePool{row: fakeBillingSubscriptionRow(sub)}
	repo := &BillingRepository{pool: pool}
	got, err := repo.GetSubscription(context.Background(), "tenant-a", "sub_1")
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if got.PlanID != "free" {
		t.Fatalf("plan = %s", got.PlanID)
	}
}

func TestBillingRepositoryGetSubscriptionNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &BillingRepository{pool: pool}
	if _, err := repo.GetSubscription(context.Background(), "tenant-a", "missing"); !errors.Is(err, billing.ErrSubscriptionNotFound) {
		t.Fatalf("expected ErrSubscriptionNotFound, got %v", err)
	}
}

func TestBillingRepositoryGetSubscriptionByStripeID(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	sub, _ := billing.NewSubscription(billing.NewSubscriptionInput{
		ID: "sub_1", TenantID: "tenant-a", PlanID: "free", Now: now,
		StripeSubscriptionID: "sub_stripe_1",
	})
	pool := &fakePool{row: fakeBillingSubscriptionRow(sub)}
	repo := &BillingRepository{pool: pool}
	got, err := repo.GetSubscriptionByStripeID(context.Background(), "tenant-a", "sub_stripe_1")
	if err != nil {
		t.Fatalf("GetByStripeID: %v", err)
	}
	if got.StripeSubscriptionID != "sub_stripe_1" {
		t.Fatalf("stripe id = %s", got.StripeSubscriptionID)
	}
}

func TestBillingRepositorySaveSubscriptionNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 0")}
	repo := &BillingRepository{pool: pool}
	sub, _ := billing.NewSubscription(billing.NewSubscriptionInput{ID: "sub_1", TenantID: "tenant-a", PlanID: "free"})
	if err := repo.SaveSubscription(context.Background(), sub); !errors.Is(err, billing.ErrSubscriptionNotFound) {
		t.Fatalf("expected ErrSubscriptionNotFound, got %v", err)
	}
}

func TestBillingRepositoryUpsertInvoice(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &BillingRepository{pool: pool}
	if err := repo.UpsertInvoice(context.Background(), billing.Invoice{
		ID: "inv_1", TenantID: "tenant-a", SubscriptionID: "sub_1",
		Amount: 1900, Currency: "AUD", Status: billing.InvoiceOpen,
	}); err != nil {
		t.Fatalf("UpsertInvoice: %v", err)
	}
}

func TestBillingRepositoryGetInvoiceNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &BillingRepository{pool: pool}
	if _, err := repo.GetInvoice(context.Background(), "tenant-a", "missing"); !errors.Is(err, billing.ErrInvoiceNotFound) {
		t.Fatalf("expected ErrInvoiceNotFound, got %v", err)
	}
}

func TestBillingRepositoryGetInvoiceSuccess(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeBillingInvoiceRow(billing.Invoice{
		ID: "inv_1", TenantID: "tenant-a", SubscriptionID: "sub_1",
		Amount: 100, Currency: "AUD", Status: billing.InvoicePaid,
	})}
	repo := &BillingRepository{pool: pool}
	got, err := repo.GetInvoice(context.Background(), "tenant-a", "inv_1")
	if err != nil {
		t.Fatalf("GetInvoice: %v", err)
	}
	if got.Status != billing.InvoicePaid {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestBillingRepositoryStripeEventSeen(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{values: []any{1}}}
	repo := &BillingRepository{pool: pool}
	seen, err := repo.StripeEventSeen(context.Background(), "evt_1")
	if err != nil {
		t.Fatalf("StripeEventSeen: %v", err)
	}
	if !seen {
		t.Fatalf("expected seen=true")
	}
}

func TestBillingRepositoryStripeEventSeenMissing(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &BillingRepository{pool: pool}
	seen, err := repo.StripeEventSeen(context.Background(), "evt_1")
	if err != nil {
		t.Fatalf("StripeEventSeen: %v", err)
	}
	if seen {
		t.Fatalf("expected seen=false")
	}
}

func TestBillingRepositoryStripeEventRecord(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &BillingRepository{pool: pool}
	if err := repo.StripeEventRecord(context.Background(), "evt_1", "subscription.created", time.Now()); err != nil {
		t.Fatalf("StripeEventRecord: %v", err)
	}
	if err := repo.StripeEventRecord(context.Background(), "", "x", time.Now()); err == nil {
		t.Fatalf("expected error for empty event id")
	}
}

func TestBillingRepositoryListEmptyTenant(t *testing.T) {
	t.Parallel()
	repo := &BillingRepository{pool: &fakePool{}}
	if _, err := repo.ListSubscriptions(context.Background(), "", 1, 10); !errors.Is(err, billing.ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
	if _, err := repo.ListInvoices(context.Background(), "", 1, 10); !errors.Is(err, billing.ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
}

func TestBillingNullTime(t *testing.T) {
	t.Parallel()
	if v := nullTime(time.Time{}); v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	if v := nullTime(now); v == nil {
		t.Fatalf("expected non-nil for non-zero time")
	}
}

func TestBillingNormalize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		page, perPage, wantPage, wantPerPage int
	}{
		{0, 0, 1, 20},
		{-1, 5, 1, 5},
		{2, 1000, 2, 100},
	}
	for _, tc := range cases {
		page, perPage := normalize(tc.page, tc.perPage)
		if page != tc.wantPage || perPage != tc.wantPerPage {
			t.Fatalf("normalize(%d,%d)=%d,%d want %d,%d", tc.page, tc.perPage, page, perPage, tc.wantPage, tc.wantPerPage)
		}
	}
}
