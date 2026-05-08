//go:build integration_pg

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/postgres"
	"github.com/nfsarch33/agentic-ecommerce/internal/billing"
)

// TestRLSTenantIsolationPositiveAndNegative verifies that the v2.5.0
// Postgres RLS policy from `migrations/0011_rls.up.sql` actually
// blocks cross-tenant reads and writes:
//
//   - Without setting the GUC, queries see all rows (admin context).
//   - Setting `app.current_tenant_id` to tenant A hides tenant B's
//     rows AND prevents writing tenant B's rows from the same
//     session.
//
// The test exercises billing_subscriptions because it's the v2.5.0
// table; the same policy is applied uniformly to every tenant-keyed
// table.
func TestRLSTenantIsolationPositiveAndNegative(t *testing.T) {
	pool := startContainerPool(t)
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo := postgres.NewBillingRepository(pool)
	now := time.Now().UTC()
	subA, _ := billing.NewSubscription(billing.NewSubscriptionInput{
		ID: "sub_a", TenantID: "tenant-a", PlanID: "free", Now: now,
	})
	subB, _ := billing.NewSubscription(billing.NewSubscriptionInput{
		ID: "sub_b", TenantID: "tenant-b", PlanID: "free", Now: now,
	})
	if err := repo.CreateSubscription(ctx, subA); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := repo.CreateSubscription(ctx, subB); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT set_config('app.current_tenant_id', 'tenant-a', false)"); err != nil {
		t.Fatalf("set GUC: %v", err)
	}

	var visible int
	if err := conn.QueryRow(ctx, "SELECT COUNT(*) FROM billing_subscriptions").Scan(&visible); err != nil {
		t.Fatalf("scoped count: %v", err)
	}
	if visible != 1 {
		t.Fatalf("scoped count = %d, want 1 (RLS not blocking tenant-b row)", visible)
	}

	var bVisible int
	if err := conn.QueryRow(ctx, "SELECT COUNT(*) FROM billing_subscriptions WHERE tenant_id = 'tenant-b'").Scan(&bVisible); err != nil {
		t.Fatalf("scoped tenant-b count: %v", err)
	}
	if bVisible != 0 {
		t.Fatalf("tenant-a session can see %d tenant-b rows", bVisible)
	}

	_, writeErr := conn.Exec(ctx, `
		INSERT INTO billing_subscriptions (id, tenant_id, plan_id, state, created_at, updated_at)
		VALUES ('sub_cross', 'tenant-b', 'free', 'trialing', now(), now())`)
	if writeErr == nil {
		t.Fatalf("RLS allowed cross-tenant write from tenant-a session")
	}
}
