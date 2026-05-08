package membership

import (
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

func mustMoney(t *testing.T, amount int, currency string) catalog.Money {
	t.Helper()
	money, err := catalog.NewMoney(amount, currency)
	if err != nil {
		t.Fatalf("catalog.NewMoney(%d, %q): %v", amount, currency, err)
	}
	return money
}

func TestNewMembershipPlanRequiresAllInvariants(t *testing.T) {
	t.Parallel()

	validPrice := mustMoney(t, 1995, "AUD")

	cases := []struct {
		name    string
		input   PlanInput
		wantErr error
	}{
		{
			name: "missing tenant",
			input: PlanInput{
				Name:         "Gold",
				BillingCycle: BillingCycleMonthly,
				Price:        validPrice,
			},
			wantErr: ErrTenantRequired,
		},
		{
			name: "missing name",
			input: PlanInput{
				TenantID:     "tenant-a",
				BillingCycle: BillingCycleMonthly,
				Price:        validPrice,
			},
			wantErr: ErrPlanNameRequired,
		},
		{
			name: "invalid cycle",
			input: PlanInput{
				TenantID:     "tenant-a",
				Name:         "Gold",
				BillingCycle: BillingCycle("daily"),
				Price:        validPrice,
			},
			wantErr: ErrInvalidBillingCycle,
		},
		{
			name: "missing currency",
			input: PlanInput{
				TenantID:     "tenant-a",
				Name:         "Gold",
				BillingCycle: BillingCycleMonthly,
			},
			wantErr: ErrPlanCurrencyRequired,
		},
		{
			name: "non-positive price",
			input: PlanInput{
				TenantID:     "tenant-a",
				Name:         "Gold",
				BillingCycle: BillingCycleMonthly,
				Price:        mustMoney(t, 0, "AUD"),
			},
			wantErr: ErrPlanPriceNotPositive,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewMembershipPlan(tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewMembershipPlanHappyPath(t *testing.T) {
	t.Parallel()

	plan, err := NewMembershipPlan(PlanInput{
		TenantID:      "tenant-a",
		Name:          " Gold ",
		Description:   "Best plan",
		BillingCycle:  BillingCycleMonthly,
		Price:         mustMoney(t, 4995, "AUD"),
		Benefits:      []string{"priority_support", "free_shipping"},
		StripePriceID: "price_1234",
	})
	if err != nil {
		t.Fatalf("NewMembershipPlan: %v", err)
	}
	if plan.ID().String() == "" {
		t.Fatal("plan.ID empty")
	}
	if plan.TenantID() != "tenant-a" {
		t.Fatalf("tenant = %q, want tenant-a", plan.TenantID())
	}
	if plan.Name() != "Gold" {
		t.Fatalf("name = %q, want trimmed Gold", plan.Name())
	}
	if plan.Price().Amount() != 4995 || plan.Price().Currency() != "AUD" {
		t.Fatalf("price = %+v, want 4995 AUD", plan.Price())
	}
	if len(plan.Benefits()) != 2 {
		t.Fatalf("benefits len = %d, want 2", len(plan.Benefits()))
	}
	if plan.StripePriceID() != "price_1234" {
		t.Fatalf("stripe price id = %q", plan.StripePriceID())
	}

	// Mutating the returned slice must not mutate the underlying plan.
	benefits := plan.Benefits()
	benefits[0] = "changed"
	if plan.Benefits()[0] != "priority_support" {
		t.Fatal("Benefits() returned aliased slice")
	}
}

func TestMembershipPlanRename(t *testing.T) {
	t.Parallel()

	plan, err := NewMembershipPlan(PlanInput{
		TenantID:     "tenant-a",
		Name:         "Gold",
		BillingCycle: BillingCycleMonthly,
		Price:        mustMoney(t, 1995, "AUD"),
	})
	if err != nil {
		t.Fatalf("NewMembershipPlan: %v", err)
	}

	if _, err := plan.Rename(""); !errors.Is(err, ErrPlanNameRequired) {
		t.Fatalf("Rename empty err = %v, want ErrPlanNameRequired", err)
	}
	noop, err := plan.Rename("Gold")
	if err != nil {
		t.Fatalf("Rename same name err = %v", err)
	}
	if !noop.UpdatedAt().Equal(plan.UpdatedAt()) {
		t.Fatalf("noop rename should not bump updatedAt")
	}
	renamed, err := plan.Rename("Platinum")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.Name() != "Platinum" {
		t.Fatalf("rename = %q, want Platinum", renamed.Name())
	}
	if !renamed.UpdatedAt().After(plan.UpdatedAt()) && !renamed.UpdatedAt().Equal(plan.UpdatedAt()) {
		// Time may be equal at very fast clocks; we accept "not before".
		t.Fatalf("renamed updatedAt = %s, original = %s", renamed.UpdatedAt(), plan.UpdatedAt())
	}
}

func TestMembershipPlanSetStripePriceID(t *testing.T) {
	t.Parallel()
	plan, err := NewMembershipPlan(PlanInput{
		TenantID:     "tenant-a",
		Name:         "Gold",
		BillingCycle: BillingCycleMonthly,
		Price:        mustMoney(t, 1995, "AUD"),
	})
	if err != nil {
		t.Fatalf("NewMembershipPlan: %v", err)
	}
	updated := plan.SetStripePriceID(" price_xyz ")
	if updated.StripePriceID() != "price_xyz" {
		t.Fatalf("stripe price id = %q, want trimmed price_xyz", updated.StripePriceID())
	}
}

func TestReconstructPlanRoundtrip(t *testing.T) {
	t.Parallel()
	original, err := NewMembershipPlan(PlanInput{
		TenantID:      "tenant-a",
		Name:          "Gold",
		BillingCycle:  BillingCycleMonthly,
		Price:         mustMoney(t, 1995, "AUD"),
		Benefits:      []string{"priority_support"},
		StripePriceID: "price_1",
	})
	if err != nil {
		t.Fatalf("NewMembershipPlan: %v", err)
	}
	rec := PlanRecord{
		ID:            original.ID(),
		TenantID:      original.TenantID(),
		Name:          original.Name(),
		BillingCycle:  original.BillingCycle(),
		Price:         original.Price(),
		Benefits:      original.Benefits(),
		StripePriceID: original.StripePriceID(),
		CreatedAt:     original.CreatedAt(),
		UpdatedAt:     original.UpdatedAt(),
	}
	rebuilt := ReconstructPlan(rec)
	if rebuilt.ID() != original.ID() || rebuilt.Name() != original.Name() || rebuilt.Price().Amount() != original.Price().Amount() {
		t.Fatalf("reconstruct mismatch: %+v vs %+v", rebuilt, original)
	}
}
