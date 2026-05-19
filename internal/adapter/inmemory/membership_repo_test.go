package inmemory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/adapter/inmemory"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	"github.com/nfsarch33/helixon-ec/internal/domain/membership"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

func mustMoney(t *testing.T, amount int, currency string) catalog.Money {
	t.Helper()
	money, err := catalog.NewMoney(amount, currency)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return money
}

func mustPlan(t *testing.T, tenant string) membership.MembershipPlan {
	t.Helper()
	plan, err := membership.NewMembershipPlan(membership.PlanInput{
		TenantID:     tenant,
		Name:         "Gold",
		BillingCycle: membership.BillingCycleMonthly,
		Price:        mustMoney(t, 1995, "AUD"),
	})
	if err != nil {
		t.Fatalf("NewMembershipPlan: %v", err)
	}
	return plan
}

func TestMembershipRepositoryRequiresTenant(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMembershipRepository()
	if _, err := repo.ListPlans(context.Background(), "", 1, 20); !errors.Is(err, membership.ErrTenantRequired) {
		t.Fatalf("err = %v, want ErrTenantRequired", err)
	}
}

func TestMembershipRepositoryPlanCRUD(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMembershipRepository()
	plan := mustPlan(t, "tenant-a")

	if err := repo.CreatePlan(context.Background(), "tenant-a", plan); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	got, err := repo.GetPlan(context.Background(), "tenant-a", plan.ID())
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.ID() != plan.ID() {
		t.Fatalf("got plan id = %s, want %s", got.ID(), plan.ID())
	}

	renamed, err := plan.Rename("Platinum")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := repo.UpdatePlan(context.Background(), "tenant-a", renamed); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	updated, err := repo.GetPlan(context.Background(), "tenant-a", plan.ID())
	if err != nil {
		t.Fatalf("GetPlan after update: %v", err)
	}
	if updated.Name() != "Platinum" {
		t.Fatalf("updated name = %s, want Platinum", updated.Name())
	}

	list, err := repo.ListPlans(context.Background(), "tenant-a", 1, 20)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if list.Total != 1 || len(list.Plans) != 1 {
		t.Fatalf("list = %+v, want total=1", list)
	}

	if err := repo.DeletePlan(context.Background(), "tenant-a", plan.ID()); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	if _, err := repo.GetPlan(context.Background(), "tenant-a", plan.ID()); !errors.Is(err, port.ErrMembershipPlanNotFound) {
		t.Fatalf("after delete: err = %v, want ErrMembershipPlanNotFound", err)
	}
}

// TestMembershipRepositoryTenantIsolation is the **negative** test case
// the v2.2.0 plan calls out: tenant B must never see tenant A's data.
func TestMembershipRepositoryTenantIsolation(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMembershipRepository()

	planA := mustPlan(t, "tenant-a")
	planB := mustPlan(t, "tenant-b")

	if err := repo.CreatePlan(context.Background(), "tenant-a", planA); err != nil {
		t.Fatalf("CreatePlan A: %v", err)
	}
	if err := repo.CreatePlan(context.Background(), "tenant-b", planB); err != nil {
		t.Fatalf("CreatePlan B: %v", err)
	}

	if _, err := repo.GetPlan(context.Background(), "tenant-b", planA.ID()); !errors.Is(err, port.ErrMembershipPlanNotFound) {
		t.Fatalf("cross-tenant GetPlan err = %v, want ErrMembershipPlanNotFound", err)
	}

	listB, err := repo.ListPlans(context.Background(), "tenant-b", 1, 20)
	if err != nil {
		t.Fatalf("ListPlans tenant-b: %v", err)
	}
	for _, p := range listB.Plans {
		if p.TenantID() != "tenant-b" {
			t.Fatalf("tenant-b list returned %s plan", p.TenantID())
		}
	}

	wrongTenantPlan := mustPlan(t, "tenant-a")
	if err := repo.CreatePlan(context.Background(), "tenant-b", wrongTenantPlan); !errors.Is(err, membership.ErrPlanTenantMismatch) {
		t.Fatalf("plan tenant mismatch err = %v, want ErrPlanTenantMismatch", err)
	}
}

func TestMembershipRepositoryMemberAndSubscriptionFlow(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMembershipRepository()
	plan := mustPlan(t, "tenant-a")
	if err := repo.CreatePlan(context.Background(), "tenant-a", plan); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	member, err := membership.NewMember(membership.MemberInput{TenantID: "tenant-a", Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	if err := repo.CreateMember(context.Background(), "tenant-a", member); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	got, err := repo.GetMember(context.Background(), "tenant-a", member.ID())
	if err != nil || got.Email() != "alice@example.com" {
		t.Fatalf("GetMember = %+v, %v", got, err)
	}

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	sub, err := membership.NewSubscription(membership.SubscriptionInput{
		TenantID:  "tenant-a",
		MemberID:  member.ID(),
		PlanID:    plan.ID(),
		TrialDays: 7,
		Now:       now,
	}, plan)
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if err := repo.CreateSubscription(context.Background(), "tenant-a", sub); err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}

	if err := repo.SaveSubscription(context.Background(), "tenant-a", sub); err != nil {
		t.Fatalf("SaveSubscription: %v", err)
	}

	loaded, err := repo.GetSubscription(context.Background(), "tenant-a", sub.ID())
	if err != nil || loaded.ID() != sub.ID() {
		t.Fatalf("GetSubscription = %+v, %v", loaded, err)
	}
	byMember, err := repo.GetSubscriptionByMember(context.Background(), "tenant-a", member.ID())
	if err != nil || byMember.ID() != sub.ID() {
		t.Fatalf("GetSubscriptionByMember = %+v, %v", byMember, err)
	}

	// Tenant isolation: member exists in tenant-a, must not surface in tenant-b.
	if _, err := repo.GetMember(context.Background(), "tenant-b", member.ID()); !errors.Is(err, port.ErrMemberNotFound) {
		t.Fatalf("cross-tenant GetMember err = %v, want ErrMemberNotFound", err)
	}
	if _, err := repo.GetSubscription(context.Background(), "tenant-b", sub.ID()); !errors.Is(err, port.ErrSubscriptionNotFound) {
		t.Fatalf("cross-tenant GetSubscription err = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestMembershipRepositoryRejectsDuplicateActiveSubscription(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMembershipRepository()
	plan := mustPlan(t, "tenant-a")
	_ = repo.CreatePlan(context.Background(), "tenant-a", plan)

	member, _ := membership.NewMember(membership.MemberInput{TenantID: "tenant-a", Email: "alice@example.com"})
	_ = repo.CreateMember(context.Background(), "tenant-a", member)

	first, _ := membership.NewSubscription(membership.SubscriptionInput{
		TenantID: "tenant-a", MemberID: member.ID(), PlanID: plan.ID(),
		TrialDays: 0, Now: time.Now(),
	}, plan)
	if err := repo.CreateSubscription(context.Background(), "tenant-a", first); err != nil {
		t.Fatalf("first CreateSubscription: %v", err)
	}
	second, _ := membership.NewSubscription(membership.SubscriptionInput{
		TenantID: "tenant-a", MemberID: member.ID(), PlanID: plan.ID(),
		TrialDays: 0, Now: time.Now(),
	}, plan)
	if err := repo.CreateSubscription(context.Background(), "tenant-a", second); !errors.Is(err, port.ErrSubscriptionNotFound) {
		t.Fatalf("duplicate active subscription err = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestMembershipRepositoryListSubscriptionsPagination(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMembershipRepository()
	plan := mustPlan(t, "tenant-a")
	_ = repo.CreatePlan(context.Background(), "tenant-a", plan)

	var ids []uuid.UUID
	base := time.Date(2026, 5, 8, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		member, _ := membership.NewMember(membership.MemberInput{TenantID: "tenant-a", Email: fmtEmail(i)})
		_ = repo.CreateMember(context.Background(), "tenant-a", member)
		sub, _ := membership.NewSubscription(membership.SubscriptionInput{
			TenantID: "tenant-a", MemberID: member.ID(), PlanID: plan.ID(),
			TrialDays: 0, Now: base.Add(time.Duration(i) * time.Hour),
		}, plan)
		_ = repo.CreateSubscription(context.Background(), "tenant-a", sub)
		ids = append(ids, sub.ID())
	}

	page, err := repo.ListSubscriptions(context.Background(), "tenant-a", 1, 2)
	if err != nil || page.Total != 3 || len(page.Subscriptions) != 2 {
		t.Fatalf("page1 = %+v, %v, want total=3 len=2", page, err)
	}
	page2, _ := repo.ListSubscriptions(context.Background(), "tenant-a", 2, 2)
	if len(page2.Subscriptions) != 1 {
		t.Fatalf("page2 = %+v, want len=1", page2)
	}
}

func fmtEmail(i int) string {
	return string(rune('a'+i)) + "@example.com"
}
