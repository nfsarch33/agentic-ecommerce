package inmemory

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
)

func TestPaginationHelpers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		page        int
		perPage     int
		wantPage    int
		wantPerPage int
	}{
		{name: "defaults", page: 0, perPage: 0, wantPage: 1, wantPerPage: 20},
		{name: "negative page", page: -1, perPage: 5, wantPage: 1, wantPerPage: 5},
		{name: "huge perpage", page: 1, perPage: 9999, wantPage: 1, wantPerPage: 20},
		{name: "happy", page: 2, perPage: 50, wantPage: 2, wantPerPage: 50},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			page, perPage := normalisePagination(tc.page, tc.perPage)
			if page != tc.wantPage || perPage != tc.wantPerPage {
				t.Fatalf("got page=%d perPage=%d, want %d/%d", page, perPage, tc.wantPage, tc.wantPerPage)
			}
		})
	}
}

func TestPageBoundsClampsBeyondTotal(t *testing.T) {
	t.Parallel()
	start, end := pageBounds(3, 5, 10)
	if start != 3 || end != 3 {
		t.Fatalf("page beyond total: start=%d end=%d", start, end)
	}
}

func TestSortMembersByEmailAndPaginateMembers(t *testing.T) {
	t.Parallel()
	members := []membership.Member{}
	for _, email := range []string{"charlie@example.com", "alice@example.com", "bob@example.com"} {
		m, err := membership.NewMember(membership.MemberInput{TenantID: "tenant-a", Email: email})
		if err != nil {
			t.Fatalf("NewMember: %v", err)
		}
		members = append(members, m)
	}
	sortMembersByEmail(members)
	if members[0].Email() != "alice@example.com" || members[2].Email() != "charlie@example.com" {
		t.Fatalf("sorted = %v", []string{members[0].Email(), members[1].Email(), members[2].Email()})
	}
	page := paginateMembers(members, 1, 2)
	if page.Total != 3 || len(page.Members) != 2 {
		t.Fatalf("paginate = %+v", page)
	}
	page2 := paginateMembers(members, 2, 2)
	if len(page2.Members) != 1 || page2.Members[0].Email() != "charlie@example.com" {
		t.Fatalf("paginate page2 = %+v", page2)
	}
}

func TestMembershipRepositoryListMembersPaginatesDeterministically(t *testing.T) {
	t.Parallel()
	repo := NewMembershipRepository()
	for _, email := range []string{"charlie@example.com", "alice@example.com", "bob@example.com"} {
		m, _ := membership.NewMember(membership.MemberInput{TenantID: "tenant-a", Email: email})
		_ = repo.CreateMember(nil, "tenant-a", m)
	}
	list, err := repo.ListMembers(nil, "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if list.Total != 3 || list.Members[0].Email() != "alice@example.com" {
		t.Fatalf("list = %+v", list)
	}
}

func TestMembershipRepositoryRequiresMatchingTenantOnSubscription(t *testing.T) {
	t.Parallel()
	repo := NewMembershipRepository()
	plan, _ := membership.NewMembershipPlan(membership.PlanInput{
		TenantID: "tenant-a", Name: "Gold", BillingCycle: membership.BillingCycleMonthly,
		Price: mustMoneyHelper(t, 1995, "AUD"),
	})
	_ = repo.CreatePlan(nil, "tenant-a", plan)
	bad := membership.ReconstructSubscription(membership.SubscriptionRecord{
		ID: uuid.New(), TenantID: "tenant-x", PlanID: plan.ID(), MemberID: uuid.New(),
		State: membership.StateTrial,
	})
	if err := repo.CreateSubscription(nil, "tenant-a", bad); err == nil {
		t.Fatal("expected tenant mismatch error")
	}
}

func TestMembershipRepositoryDuplicatePlanCreate(t *testing.T) {
	t.Parallel()
	repo := NewMembershipRepository()
	plan, _ := membership.NewMembershipPlan(membership.PlanInput{
		TenantID: "tenant-a", Name: "Gold", BillingCycle: membership.BillingCycleMonthly,
		Price: mustMoneyHelper(t, 1995, "AUD"),
	})
	if err := repo.CreatePlan(nil, "tenant-a", plan); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := repo.CreatePlan(nil, "tenant-a", plan); err == nil {
		t.Fatal("expected duplicate plan error")
	}
}

func TestMembershipRepositoryUpdatePlanMissing(t *testing.T) {
	t.Parallel()
	repo := NewMembershipRepository()
	plan, _ := membership.NewMembershipPlan(membership.PlanInput{
		TenantID: "tenant-a", Name: "Gold", BillingCycle: membership.BillingCycleMonthly,
		Price: mustMoneyHelper(t, 1995, "AUD"),
	})
	if err := repo.UpdatePlan(nil, "tenant-a", plan); err == nil {
		t.Fatal("expected ErrMembershipPlanNotFound")
	}
}

func TestMembershipRepositorySaveSubscriptionMissing(t *testing.T) {
	t.Parallel()
	repo := NewMembershipRepository()
	plan, _ := membership.NewMembershipPlan(membership.PlanInput{
		TenantID: "tenant-a", Name: "Gold", BillingCycle: membership.BillingCycleMonthly,
		Price: mustMoneyHelper(t, 1995, "AUD"),
	})
	_ = repo.CreatePlan(nil, "tenant-a", plan)
	sub, _ := membership.NewSubscription(membership.SubscriptionInput{
		TenantID: "tenant-a", MemberID: uuid.New(), PlanID: plan.ID(),
	}, plan)
	if err := repo.SaveSubscription(nil, "tenant-a", sub); err == nil {
		t.Fatal("expected ErrSubscriptionNotFound")
	}
}

func mustMoneyHelper(t *testing.T, amount int, currency string) catalog.Money {
	t.Helper()
	money, err := catalog.NewMoney(amount, currency)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return money
}
