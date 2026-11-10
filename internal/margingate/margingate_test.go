package margingate

import (
	"math"
	"testing"
)

// The template's own spike numbers (evidence/v18960-137-ecommerce-spike/
// margin_check.py): retail 29.90, cost 5.00, fulfil 6.50, referral 15%,
// fixed 120.00, batch 500 -> per-unit 13.915, net 46.5%, break-even 9
// units = 1.8% of the batch. PASS.
func TestTemplateNumbersPass(t *testing.T) {
	v := Gate(Inputs{RetailCents: 2990, UnitCostCents: 500, FulfilCents: 650, ReferralPct: 15, BatchFixedCents: 12000, BatchUnits: 500})
	if !v.Pass {
		t.Fatalf("template numbers must pass: %+v", v)
	}
	if math.Abs(v.NetMarginPct-46.5) > 0.1 {
		t.Fatalf("net margin = %.2f, want ~46.5", v.NetMarginPct)
	}
	if math.Abs(v.BreakEvenPctOfBatch-1.8) > 0.1 {
		t.Fatalf("break-even pct = %.2f, want ~1.8", v.BreakEvenPctOfBatch)
	}
}

// Acceptance scenario 2: a 25% net margin rejects.
func TestThinMarginRejects(t *testing.T) {
	v := Gate(Inputs{RetailCents: 2990, UnitCostCents: 500, FulfilCents: 1294, ReferralPct: 15, BatchFixedCents: 12000, BatchUnits: 500})
	if v.Pass {
		t.Fatalf("a net margin under 30 percent must reject: %+v", v)
	}
	if v.Reason == "" {
		t.Fatal("the reject must carry the spec's reason")
	}
}

// Break-even above 20% of the batch rejects even with a healthy margin.
func TestBreakEvenOverTwentyPercentRejects(t *testing.T) {
	v := Gate(Inputs{RetailCents: 2990, UnitCostCents: 500, FulfilCents: 650, ReferralPct: 15, BatchFixedCents: 12000, BatchUnits: 30})
	if v.Pass {
		t.Fatalf("break-even 9 of 30 units = 30%% of the batch must reject: %+v", v)
	}
}

// A non-positive per-unit margin is a reject, not a divide-by-zero.
func TestNoPerUnitMarginRejects(t *testing.T) {
	v := Gate(Inputs{RetailCents: 2990, UnitCostCents: 2990, FulfilCents: 650, ReferralPct: 15, BatchFixedCents: 12000, BatchUnits: 500})
	if v.Pass {
		t.Fatalf("per-unit 0 or worse must reject: %+v", v)
	}
}
