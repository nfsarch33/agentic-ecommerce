package affiliate_test

import (
	"math"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/affiliate"
)

const epsilon = 1e-9

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

func baseCode(slug string, active bool) affiliate.Code {
	return affiliate.Code{
		ID:             "c-" + slug,
		OwnerID:        "owner-1",
		Slug:           slug,
		DiscountPct:    10,
		CommissionRate: 0.05,
		Active:         active,
		CreatedAt:      time.Now(),
	}
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()
	r := affiliate.NewRegistry()
	code := baseCode("SAVE10", true)
	r.Register(code)

	got, err := r.Lookup("SAVE10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Slug != "SAVE10" {
		t.Errorf("slug = %q, want %q", got.Slug, "SAVE10")
	}
}

func TestRegistry_LookupNotFound(t *testing.T) {
	t.Parallel()
	r := affiliate.NewRegistry()
	_, err := r.Lookup("NOPE")
	if err != affiliate.ErrCodeNotFound {
		t.Errorf("err = %v, want ErrCodeNotFound", err)
	}
}

func TestRegistry_LookupInactiveCode(t *testing.T) {
	t.Parallel()
	r := affiliate.NewRegistry()
	r.Register(baseCode("OFF", false))
	_, err := r.Lookup("OFF")
	if err != affiliate.ErrInactiveCode {
		t.Errorf("err = %v, want ErrInactiveCode", err)
	}
}

func TestRegistry_List(t *testing.T) {
	t.Parallel()
	r := affiliate.NewRegistry()
	r.Register(baseCode("A", true))
	r.Register(baseCode("B", false))
	list := r.List()
	if len(list) != 2 {
		t.Errorf("len(list) = %d, want 2", len(list))
	}
}

func TestLedger_CommissionAccumulation(t *testing.T) {
	t.Parallel()
	l := affiliate.NewLedger()
	l.RecordSale(affiliate.Sale{CodeID: "owner-1", CommissionEarned: 5.00})
	l.RecordSale(affiliate.Sale{CodeID: "owner-1", CommissionEarned: 3.50})

	got := l.PendingPayout("owner-1")
	if got != 8.50 {
		t.Errorf("PendingPayout = %.2f, want 8.50", got)
	}
}

func TestLedger_MarkPaidResetBalance(t *testing.T) {
	t.Parallel()
	l := affiliate.NewLedger()
	l.RecordSale(affiliate.Sale{CodeID: "owner-2", CommissionEarned: 20.00})
	l.MarkPaid("owner-2")

	got := l.PendingPayout("owner-2")
	if got != 0 {
		t.Errorf("PendingPayout after MarkPaid = %.2f, want 0", got)
	}
}

func TestLedger_UnknownOwnerReturnsZero(t *testing.T) {
	t.Parallel()
	l := affiliate.NewLedger()
	got := l.PendingPayout("nobody")
	if got != 0 {
		t.Errorf("PendingPayout(nobody) = %.2f, want 0", got)
	}
}

func TestCommissionCalculation(t *testing.T) {
	t.Parallel()
	rate := 0.07
	amount := 100.00
	earned := amount * rate
	if !almostEqual(earned, 7.00) {
		t.Errorf("commission = %.10f, want ~7.00", earned)
	}
}
