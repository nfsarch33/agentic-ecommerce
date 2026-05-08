package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

func mustMoneyM(t *testing.T, amount int, currency string) catalog.Money {
	t.Helper()
	money, err := catalog.NewMoney(amount, currency)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return money
}

func newPlanForTest(t *testing.T) membership.MembershipPlan {
	t.Helper()
	plan, err := membership.NewMembershipPlan(membership.PlanInput{
		TenantID:     "tenant-a",
		Name:         "Gold",
		BillingCycle: membership.BillingCycleMonthly,
		Price:        mustMoneyM(t, 1995, "AUD"),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan
}

func fakePlanRow(plan membership.MembershipPlan) fakeRow {
	return fakeRow{values: []any{
		plan.ID(),
		plan.TenantID(),
		plan.Name(),
		plan.Description(),
		string(plan.BillingCycle()),
		plan.Price().Amount(),
		plan.Price().Currency(),
		plan.Benefits(),
		plan.StripePriceID(),
		plan.CreatedAt(),
		plan.UpdatedAt(),
	}}
}

func fakeSubscriptionRow(sub membership.Subscription) fakeRow {
	return fakeRow{values: []any{
		sub.ID(), sub.TenantID(), sub.MemberID(), sub.PlanID(), string(sub.State()),
		sub.CurrentPeriodStart(), sub.CurrentPeriodEnd(), sub.TrialEndsAt(),
		sub.StripeSubscriptionID(), sub.CancelledAt(), sub.CreatedAt(), sub.UpdatedAt(),
	}}
}

func TestMembershipRepoCreatePlanRequiresMatchingTenant(t *testing.T) {
	t.Parallel()
	repo := &MembershipRepository{pool: &fakePool{}}
	plan := newPlanForTest(t)
	if err := repo.CreatePlan(context.Background(), "tenant-b", plan); !errors.Is(err, membership.ErrPlanTenantMismatch) {
		t.Fatalf("err = %v, want ErrPlanTenantMismatch", err)
	}
}

func TestMembershipRepoCreatePlanInsertsRow(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &MembershipRepository{pool: pool}
	plan := newPlanForTest(t)
	if err := repo.CreatePlan(context.Background(), "tenant-a", plan); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if len(pool.execSQL) != 1 {
		t.Fatalf("exec count = %d, want 1", len(pool.execSQL))
	}
}

func TestMembershipRepoUpdatePlanReturnsNotFoundOnZeroRows(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 0")}
	repo := &MembershipRepository{pool: pool}
	plan := newPlanForTest(t)
	err := repo.UpdatePlan(context.Background(), "tenant-a", plan)
	if !errors.Is(err, port.ErrMembershipPlanNotFound) {
		t.Fatalf("err = %v, want ErrMembershipPlanNotFound", err)
	}
}

func TestMembershipRepoUpdatePlanRejectsForeignTenant(t *testing.T) {
	t.Parallel()
	repo := &MembershipRepository{pool: &fakePool{}}
	plan := newPlanForTest(t)
	if err := repo.UpdatePlan(context.Background(), "tenant-x", plan); !errors.Is(err, membership.ErrPlanTenantMismatch) {
		t.Fatalf("err = %v, want ErrPlanTenantMismatch", err)
	}
}

func TestMembershipRepoGetPlanScansRow(t *testing.T) {
	t.Parallel()
	plan := newPlanForTest(t)
	pool := &fakePool{row: fakePlanRow(plan)}
	repo := &MembershipRepository{pool: pool}
	got, err := repo.GetPlan(context.Background(), "tenant-a", plan.ID())
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.ID() != plan.ID() || got.Name() != plan.Name() {
		t.Fatalf("got = %+v want %+v", got, plan)
	}
}

func TestMembershipRepoGetPlanReturnsNotFoundOnNoRows(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &MembershipRepository{pool: pool}
	_, err := repo.GetPlan(context.Background(), "tenant-a", uuid.New())
	if !errors.Is(err, port.ErrMembershipPlanNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMembershipRepoListPlansHonoursLimitOffset(t *testing.T) {
	t.Parallel()
	plan := newPlanForTest(t)
	pool := &fakePool{
		rows: &fakeRows{rows: [][]any{fakePlanRow(plan).values}},
		row:  fakeRow{values: []any{1}},
	}
	repo := &MembershipRepository{pool: pool}
	got, err := repo.ListPlans(context.Background(), "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if got.Total != 1 || len(got.Plans) != 1 {
		t.Fatalf("list = total %d len %d", got.Total, len(got.Plans))
	}
}

func TestMembershipRepoDeletePlan(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("DELETE 1")}
	repo := &MembershipRepository{pool: pool}
	if err := repo.DeletePlan(context.Background(), "tenant-a", uuid.New()); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	pool.commandTag = pgconn.NewCommandTag("DELETE 0")
	if err := repo.DeletePlan(context.Background(), "tenant-a", uuid.New()); !errors.Is(err, port.ErrMembershipPlanNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestMembershipRepoCreateMemberInsertsRow(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &MembershipRepository{pool: pool}
	member, err := membership.NewMember(membership.MemberInput{TenantID: "tenant-a", Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	if err := repo.CreateMember(context.Background(), "tenant-a", member); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if len(pool.execSQL) != 1 {
		t.Fatalf("exec count = %d", len(pool.execSQL))
	}
}

func TestMembershipRepoGetMemberScans(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	id := uuid.New()
	pool := &fakePool{
		row: fakeRow{values: []any{id, "tenant-a", "alice@example.com", now, now}},
	}
	repo := &MembershipRepository{pool: pool}
	got, err := repo.GetMember(context.Background(), "tenant-a", id)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.ID() != id || got.Email() != "alice@example.com" {
		t.Fatalf("got = %+v", got)
	}
}

func TestMembershipRepoGetMemberNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &MembershipRepository{pool: pool}
	if _, err := repo.GetMember(context.Background(), "tenant-a", uuid.New()); !errors.Is(err, port.ErrMemberNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMembershipRepoListMembers(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	id := uuid.New()
	pool := &fakePool{
		rows: &fakeRows{rows: [][]any{{id, "tenant-a", "alice@example.com", now, now}}},
		row:  fakeRow{values: []any{1}},
	}
	repo := &MembershipRepository{pool: pool}
	got, err := repo.ListMembers(context.Background(), "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if got.Total != 1 || len(got.Members) != 1 {
		t.Fatalf("list = %+v", got)
	}
}

func TestMembershipRepoCreateAndGetSubscription(t *testing.T) {
	t.Parallel()
	plan := newPlanForTest(t)
	sub, err := membership.NewSubscription(membership.SubscriptionInput{
		TenantID: "tenant-a", MemberID: uuid.New(), PlanID: plan.ID(),
	}, plan)
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	pool := &fakePool{
		commandTag: pgconn.NewCommandTag("INSERT 0 1"),
		row:        fakeSubscriptionRow(sub),
	}
	repo := &MembershipRepository{pool: pool}
	if err := repo.CreateSubscription(context.Background(), "tenant-a", sub); err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	got, err := repo.GetSubscription(context.Background(), "tenant-a", sub.ID())
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if got.ID() != sub.ID() {
		t.Fatalf("id mismatch")
	}
}

func TestMembershipRepoSaveSubscriptionRequiresExistingRow(t *testing.T) {
	t.Parallel()
	plan := newPlanForTest(t)
	sub, _ := membership.NewSubscription(membership.SubscriptionInput{
		TenantID: "tenant-a", MemberID: uuid.New(), PlanID: plan.ID(),
	}, plan)
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 0")}
	repo := &MembershipRepository{pool: pool}
	if err := repo.SaveSubscription(context.Background(), "tenant-a", sub); !errors.Is(err, port.ErrSubscriptionNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMembershipRepoSaveSubscriptionUpdates(t *testing.T) {
	t.Parallel()
	plan := newPlanForTest(t)
	sub, _ := membership.NewSubscription(membership.SubscriptionInput{
		TenantID: "tenant-a", MemberID: uuid.New(), PlanID: plan.ID(),
	}, plan)
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 1")}
	repo := &MembershipRepository{pool: pool}
	if err := repo.SaveSubscription(context.Background(), "tenant-a", sub); err != nil {
		t.Fatalf("SaveSubscription: %v", err)
	}
}

func TestMembershipRepoGetSubscriptionByMember(t *testing.T) {
	t.Parallel()
	plan := newPlanForTest(t)
	sub, _ := membership.NewSubscription(membership.SubscriptionInput{
		TenantID: "tenant-a", MemberID: uuid.New(), PlanID: plan.ID(),
	}, plan)
	pool := &fakePool{row: fakeSubscriptionRow(sub)}
	repo := &MembershipRepository{pool: pool}
	got, err := repo.GetSubscriptionByMember(context.Background(), "tenant-a", sub.MemberID())
	if err != nil {
		t.Fatalf("GetSubscriptionByMember: %v", err)
	}
	if got.MemberID() != sub.MemberID() {
		t.Fatalf("member mismatch")
	}
}

func TestMembershipRepoGetSubscriptionNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &MembershipRepository{pool: pool}
	if _, err := repo.GetSubscription(context.Background(), "tenant-a", uuid.New()); !errors.Is(err, port.ErrSubscriptionNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := repo.GetSubscriptionByMember(context.Background(), "tenant-a", uuid.New()); !errors.Is(err, port.ErrSubscriptionNotFound) {
		t.Fatalf("by-member err = %v", err)
	}
}

func TestMembershipRepoListSubscriptions(t *testing.T) {
	t.Parallel()
	plan := newPlanForTest(t)
	sub, _ := membership.NewSubscription(membership.SubscriptionInput{
		TenantID: "tenant-a", MemberID: uuid.New(), PlanID: plan.ID(),
	}, plan)
	pool := &fakePool{
		rows: &fakeRows{rows: [][]any{fakeSubscriptionRow(sub).values}},
		row:  fakeRow{values: []any{1}},
	}
	repo := &MembershipRepository{pool: pool}
	got, err := repo.ListSubscriptions(context.Background(), "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if got.Total != 1 || len(got.Subscriptions) != 1 {
		t.Fatalf("list = %+v", got)
	}
}

func TestMembershipRepoEnforcesTenantOnSubscription(t *testing.T) {
	t.Parallel()
	plan := newPlanForTest(t)
	sub, _ := membership.NewSubscription(membership.SubscriptionInput{
		TenantID: "tenant-a", MemberID: uuid.New(), PlanID: plan.ID(),
	}, plan)
	repo := &MembershipRepository{pool: &fakePool{}}
	if err := repo.CreateSubscription(context.Background(), "tenant-x", sub); !errors.Is(err, membership.ErrTenantRequired) {
		t.Fatalf("err = %v", err)
	}
	if err := repo.SaveSubscription(context.Background(), "tenant-x", sub); !errors.Is(err, membership.ErrTenantRequired) {
		t.Fatalf("save err = %v", err)
	}
}

func TestMembershipRepoCreateMemberRequiresTenant(t *testing.T) {
	t.Parallel()
	repo := &MembershipRepository{pool: &fakePool{}}
	member, _ := membership.NewMember(membership.MemberInput{TenantID: "tenant-a", Email: "alice@example.com"})
	if err := repo.CreateMember(context.Background(), "tenant-x", member); !errors.Is(err, membership.ErrTenantRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestMembershipRepoNormalisePagination(t *testing.T) {
	t.Parallel()
	if p, pp := normalisePagination(0, 0); p != 1 || pp != 20 {
		t.Fatalf("defaults = %d/%d", p, pp)
	}
	if p, pp := normalisePagination(2, 200); p != 2 || pp != 20 {
		t.Fatalf("clamp big perPage = %d/%d", p, pp)
	}
}

// scanPlanRow / scanSubscriptionRow currency error path.
func TestMembershipRepoScanPlanRejectsBadCurrency(t *testing.T) {
	t.Parallel()
	plan := newPlanForTest(t)
	values := fakePlanRow(plan).values
	values[6] = "" // currency
	pool := &fakePool{row: fakeRow{values: values}}
	repo := &MembershipRepository{pool: pool}
	if _, err := repo.GetPlan(context.Background(), "tenant-a", plan.ID()); err == nil {
		t.Fatal("expected hydrate error")
	}
}

func TestMembershipRepoScanSubscriptionRejectsBadState(t *testing.T) {
	t.Parallel()
	plan := newPlanForTest(t)
	sub, _ := membership.NewSubscription(membership.SubscriptionInput{
		TenantID: "tenant-a", MemberID: uuid.New(), PlanID: plan.ID(),
	}, plan)
	values := fakeSubscriptionRow(sub).values
	values[4] = "bogus_state"
	pool := &fakePool{row: fakeRow{values: values}}
	repo := &MembershipRepository{pool: pool}
	if _, err := repo.GetSubscription(context.Background(), "tenant-a", sub.ID()); err == nil {
		t.Fatal("expected hydrate state error")
	}
}
