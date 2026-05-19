package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/helixon-ec/internal/billing"
)

type fakeMultiPool struct {
	*fakePool
	rowQueue []fakeRow
	rowsOut  []*fakeRows
	rowIndex int
}

func (p *fakeMultiPool) QueryRow(_ context.Context, sql string, _ ...any) interface{} {
	p.querySQL = append(p.querySQL, sql)
	if p.rowIndex >= len(p.rowQueue) {
		return p.rowQueue[len(p.rowQueue)-1]
	}
	row := p.rowQueue[p.rowIndex]
	p.rowIndex++
	return row
}

func TestBillingRepositoryListSubscriptionsCountAndPage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	sub, _ := billing.NewSubscription(billing.NewSubscriptionInput{
		ID: "sub_1", TenantID: "tenant-a", PlanID: "free", Now: now,
		StripeSubscriptionID: "sub_stripe_1",
	})
	pool := &fakePool{
		row: fakeRow{values: []any{1}}, // count = 1
		rows: &fakeRows{rows: [][]any{
			{
				sub.ID, sub.TenantID, sub.PlanID, string(sub.State),
				sub.StripeSubscriptionID, sub.StripeCustomerID,
				timePtr(sub.CurrentPeriodStart), timePtr(sub.CurrentPeriodEnd),
				sub.CancelAtPeriodEnd, sub.CreatedAt, sub.UpdatedAt,
			},
		}},
	}
	repo := &BillingRepository{pool: pool}
	got, err := repo.ListSubscriptions(context.Background(), "tenant-a", 1, 20)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if got.Total != 1 {
		t.Fatalf("total = %d", got.Total)
	}
	if len(got.Subscriptions) != 1 {
		t.Fatalf("page len = %d", len(got.Subscriptions))
	}
}

func TestBillingRepositoryListSubscriptionsCountErr(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: errors.New("boom")}}
	repo := &BillingRepository{pool: pool}
	if _, err := repo.ListSubscriptions(context.Background(), "tenant-a", 1, 20); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBillingRepositoryListInvoicesCountAndPage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	pool := &fakePool{
		row: fakeRow{values: []any{1}},
		rows: &fakeRows{rows: [][]any{
			{
				"inv_1", "tenant-a", "sub_1", 1900, "AUD", string(billing.InvoicePaid),
				timePtr(now), timePtr(now.Add(time.Hour)), now, now,
			},
		}},
	}
	repo := &BillingRepository{pool: pool}
	got, err := repo.ListInvoices(context.Background(), "tenant-a", 1, 20)
	if err != nil {
		t.Fatalf("ListInvoices: %v", err)
	}
	if got.Total != 1 {
		t.Fatalf("total = %d", got.Total)
	}
	if len(got.Invoices) != 1 {
		t.Fatalf("page len = %d", len(got.Invoices))
	}
}

func TestBillingRepositoryUpsertInvoiceTenantRequired(t *testing.T) {
	t.Parallel()
	repo := &BillingRepository{pool: &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 0")}}
	if err := repo.UpsertInvoice(context.Background(), billing.Invoice{ID: "inv_1"}); !errors.Is(err, billing.ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
