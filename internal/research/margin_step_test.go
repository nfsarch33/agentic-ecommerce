package research

import (
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/margingate"
)

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
// no draft - proved from the result alone: Evaluate is a pure function of
// its recordings and gate inputs (no model, store or draft parameter
// exists to call), and the stop result carries no record and no verdict,
// so nothing downstream of the step runs.
func TestStopPathCallsNothing(t *testing.T) {
	res := Evaluate([]BandRecord{
		rec(BandWholesale, 500, 600, 500, "https://s.example/wh", "q1"),
		rec(BandDemand, 40, 60, 0, "https://s.example/dm", "q2"),
		rec(BandWholesale, 520, 610, 500, "https://s.example/wh2", "q3"),
		rec(BandDemand, 41, 59, 0, "https://s.example/dm2", "q4"),
	}, margingate.Inputs{})
	if res.Kind != StepNotEstablished {
		t.Fatalf("kind = %v, want not-established", res.Kind)
	}
	if res.Stop == "" {
		t.Fatal("the stop line must name what is missing")
	}
	if len(res.Record) != 0 || res.Verdict != "" {
		t.Fatalf("the stop path must produce nothing downstream: %+v", res)
	}
}

// Acceptance scenario 2: a 25% margin rejects and no draft is written -
// the reject result carries the gate's reason and no record rows (the
// record is the draft's content; none is produced).
func TestRejectWritesNoDraft(t *testing.T) {
	res := Evaluate(bothBands(), gateIn(2990, 500, 1294, 15, 12000, 500))
	if res.Kind != StepGateReject {
		t.Fatalf("kind = %v, want gate-reject", res.Kind)
	}
	if res.Verdict == "" {
		t.Fatal("the reject must carry the gate's reason")
	}
	if len(res.Record) != 0 {
		t.Fatalf("a gate reject must write no draft: %d record rows", len(res.Record))
	}
}

// Acceptance scenario 3: 35% margin, break-even 15% of batch -> the record
// carries every number with its source url and the verdict pass. Every
// number means the band's high and MOQ too, not just the low.
func TestPassCarriesNumbersWithSources(t *testing.T) {
	res := Evaluate(bothBands(), gateIn(2990, 500, 990, 15, 78800, 500))
	if res.Kind != StepPass {
		t.Fatalf("kind = %v, want pass", res.Kind)
	}
	rows := map[BandKind]RecordRow{}
	for _, row := range res.Record {
		rows[row.Kind] = row
	}
	wh := rows[BandWholesale]
	if wh.LowCents != 500 || wh.HighCents != 600 || wh.MOQUnits != 500 || wh.SourceURL != "https://s.example/wh" {
		t.Fatalf("wholesale row = %+v, want low 500 high 600 MOQ 500 with its source", wh)
	}
	rt := rows[BandRetail]
	if rt.LowCents != 2900 || rt.HighCents != 3100 || rt.SourceURL != "https://s.example/rt" {
		t.Fatalf("retail row = %+v, want low 2900 high 3100 with its source", rt)
	}
	dm := rows[BandDemand]
	if dm.LowCents != 40 || dm.HighCents != 60 || dm.SourceURL != "https://s.example/dm" {
		t.Fatalf("demand row = %+v, want low 40 high 60 with its source", dm)
	}
	if res.Verdict == "" {
		t.Fatal("the pass must carry the gate verdict line")
	}
}
