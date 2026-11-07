package costledger

import (
	"context"
	"testing"
)

func TestSummarizeFoldsRowsAndCountsErrors(t *testing.T) {
	s := Summarize([]Row{
		{Model: "b", TokensIn: 10, TokensOut: 5, CostCents: 3, Status: "ok"},
		{Model: "a", TokensIn: 1, TokensOut: 2, CostCents: 4, Status: "ok"},
		{Model: "b", Status: "error", ErrorText: "timeout"},
	})
	if s.Calls != 3 || s.Errors != 1 || s.TokensIn != 11 || s.TokensOut != 7 || s.CostCents != 7 {
		t.Fatalf("summary = %+v", s)
	}
	if len(s.Models) != 2 || s.Models[0] != "a" || s.Models[1] != "b" {
		t.Fatalf("models = %v, want distinct and sorted", s.Models)
	}
	if z := Summarize(nil); z.Calls != 0 || z.Models != nil {
		t.Fatalf("empty = %+v", z)
	}
}

func TestMemLedgerReadsBackOneJobsStep(t *testing.T) {
	m := NewMemLedger()
	ctx := context.Background()
	_ = m.Record(ctx, Row{JobID: "j1", RunID: "r1", Action: "research", CostCents: 1})
	_ = m.Record(ctx, Row{JobID: "j1", RunID: "r1", Action: "price", CostCents: 2})
	_ = m.Record(ctx, Row{JobID: "j2", RunID: "r1", Action: "research", CostCents: 4})
	_ = m.Record(ctx, Row{JobID: "j1", RunID: "r2", Action: "research", CostCents: 16})
	_ = m.Record(ctx, Row{JobID: "j1", RunID: "r1", Action: "research", CostCents: 8})
	rows, err := m.Rows(ctx, "j1", "r1", "research")
	if err != nil || len(rows) != 2 || rows[0].CostCents != 1 || rows[1].CostCents != 8 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
}
