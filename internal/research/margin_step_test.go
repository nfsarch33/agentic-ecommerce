package research

import (
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/margingate"
)

type spyCalls struct {
	model, store, draft int
}

func (s *spyCalls) Model() { s.model++ }
func (s *spyCalls) Store() { s.store++ }
func (s *spyCalls) Draft() { s.draft++ }

func bothBands() []BandRecord {
	return []BandRecord{
		rec(BandWholesale, 500, 600, 500, "https://s.example/wh", "q1"),
		rec(BandRetail, 2900, 3100, 0, "https://s.example/rt", "q2"),
		rec(BandDemand, 40, 60, 0, "https://s.example/dm", "q3"),
	}
}

func gateIn(retail, cost, fulfil, referral, fixed, batch int64) margingate.Inputs {
	return margingate.Inputs{RetailCents: retail, UnitCostCents: cost, FulfilCents: fulfil, ReferralPct: float64(referral), BatchFixedCents: fixed, BatchUnits: batch}
}

// Acceptance scenario 1: not established -> no model call, no store call,
// asserted by counting the spies.
func TestStopPathCallsNothing(t *testing.T) {
	spies := &spyCalls{}
	res := Evaluate([]BandRecord{
		rec(BandWholesale, 500, 600, 500, "https://s.example/wh", "q1"),
		rec(BandDemand, 40, 60, 0, "https://s.example/dm", "q2"),
		rec(BandWholesale, 520, 610, 500, "https://s.example/wh2", "q3"),
		rec(BandDemand, 41, 59, 0, "https://s.example/dm2", "q4"),
	}, margingate.Inputs{}, StepCalls{Model: spies.Model, Store: spies.Store, Draft: spies.Draft})
	if res.Kind != StepNotEstablished {
		t.Fatalf("kind = %v, want not-established", res.Kind)
	}
	if spies.model != 0 || spies.store != 0 {
		t.Fatalf("the stop path must make no calls: model=%d store=%d", spies.model, spies.store)
	}
}

// Acceptance scenario 2: a 25% margin rejects and no draft is written.
func TestRejectWritesNoDraft(t *testing.T) {
	spies := &spyCalls{}
	res := Evaluate(bothBands(), gateIn(2990, 500, 1294, 15, 12000, 500), StepCalls{Model: spies.Model, Store: spies.Store, Draft: spies.Draft})
	if res.Kind != StepGateReject {
		t.Fatalf("kind = %v, want gate-reject", res.Kind)
	}
	if spies.draft != 0 {
		t.Fatalf("a gate reject must write no draft: draft=%d", spies.draft)
	}
}

// Acceptance scenario 3: 35% margin, break-even 15% of batch -> the record
// carries every number with its source url and the verdict pass.
func TestPassCarriesNumbersWithSources(t *testing.T) {
	spies := &spyCalls{}
	res := Evaluate(bothBands(), gateIn(2990, 500, 990, 15, 78800, 500), StepCalls{Model: spies.Model, Store: spies.Store, Draft: spies.Draft})
	if res.Kind != StepPass {
		t.Fatalf("kind = %v, want pass", res.Kind)
	}
	if len(res.Record) != 3 {
		t.Fatalf("record rows = %d, want 3 (wholesale, retail, demand)", len(res.Record))
	}
	for _, row := range res.Record {
		if row.Number == 0 || row.SourceURL == "" {
			t.Fatalf("every number must carry its source: %+v", row)
		}
	}
	if res.Verdict == "" {
		t.Fatal("the pass must carry the gate verdict line")
	}
}
