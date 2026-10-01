package costledger

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/costcalc"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

type recRow struct{ row Row; calls int }

type memRecorder struct{ rows []Row }

func (m *memRecorder) Record(_ context.Context, row Row) error {
	m.rows = append(m.rows, row)
	return nil
}

type stubGen struct {
	resp port.AICompletionResponse
	err  error
}

func (s stubGen) Complete(context.Context, port.AICompletionRequest) (port.AICompletionResponse, error) {
	return s.resp, s.err
}

var testPrices = costcalc.Table{"test-model": {InputCentsPerMTok: 1000, OutputCentsPerMTok: 2000}} // cents per Mtok

// MUTANT: drop the Recorder call in Complete and no row exists — the
// nightly report and the alert both go blind; this test goes red.
func TestCompleteRecordsExactlyOneRowPerCall(t *testing.T) {
	rec := &memRecorder{}
	g := &RecordingGenerator{Inner: stubGen{resp: port.AICompletionResponse{Content: "x", TokensUsed: 120}}, Recorder: rec, Prices: testPrices}
	ctx := WithAttrs(context.Background(), Attrs{JobID: "job-1", TenantID: "t-1", Action: "content.generate"})

	if _, err := g.Complete(ctx, port.AICompletionRequest{Model: "test-model"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(rec.rows) != 1 {
		t.Fatalf("rows = %d, want exactly 1 per call", len(rec.rows))
	}
	r := rec.rows[0]
	if r.JobID != "job-1" || r.TenantID != "t-1" || r.Action != "content.generate" || r.Model != "test-model" {
		t.Fatalf("attribution lost: %+v", r)
	}
	if r.Status != "ok" || r.TokensOut != 120 || r.CostCents == 0 {
		t.Fatalf("usage/cost wrong: %+v", r)
	}
}

// MUTANT: skip the error branch and failures vanish from the ledger — the
// same-day alert on failing loops goes blind; this test goes red.
func TestCompleteRecordsFailuresToo(t *testing.T) {
	rec := &memRecorder{}
	g := &RecordingGenerator{Inner: stubGen{err: errors.New("boom")}, Recorder: rec, Prices: testPrices}
	ctx := WithAttrs(context.Background(), Attrs{JobID: "job-2"})

	resp, err := g.Complete(ctx, port.AICompletionRequest{Model: "test-model"})
	if err == nil {
		t.Fatal("the inner error must surface")
	}
	_ = resp
	if len(rec.rows) != 1 || rec.rows[0].Status != "error" || rec.rows[0].ErrorText != "boom" {
		t.Fatalf("failure not recorded: %+v", rec.rows)
	}
}

// MUTANT: fail the generation when the ledger write fails and product calls
// start failing for observability reasons; this test goes red.
func TestLedgerWriteFailureDoesNotPoisonTheCall(t *testing.T) {
	g := &RecordingGenerator{
		Inner:    stubGen{resp: port.AICompletionResponse{Content: "ok"}},
		Recorder: failRecorder{},
		Prices:   testPrices,
	}
	if _, err := g.Complete(context.Background(), port.AICompletionRequest{Model: "test-model"}); err != nil {
		t.Fatalf("a ledger hiccup must not fail the product call: %v", err)
	}
}

type failRecorder struct{}

func (failRecorder) Record(context.Context, Row) error { return errors.New("pg down") }

// MUTANT: lower the threshold or drop the per-job grouping and the 21-loop
// acceptance breaks; these pins go red.
func TestAlertOnFailingLoop(t *testing.T) {
	now := time.Date(2026, 10, 1, 6, 0, 0, 0, time.UTC)
	var loop []Failure
	for i := 0; i < 21; i++ {
		loop = append(loop, Failure{JobID: "job-x", At: now.Add(-time.Duration(i) * time.Minute)})
	}
	if !AlertOnFailingLoop(loop, 24*time.Hour, now) {
		t.Fatal("21 failing calls for one job inside the window must raise")
	}
	if AlertOnFailingLoop(loop[:20], 24*time.Hour, now) {
		t.Fatal("20 failing calls must not raise")
	}
	var spread []Failure
	for i := 0; i < 21; i++ {
		spread = append(spread, Failure{JobID: string(rune('a'+i%2)), At: now.Add(-time.Duration(i) * time.Minute)})
	}
	if AlertOnFailingLoop(spread, 24*time.Hour, now) {
		t.Fatal("21 failures spread across two jobs must not raise")
	}
	old := []Failure{{JobID: "job-x", At: now.Add(-48 * time.Hour)}}
	for i := 1; i < 21; i++ {
		old = append(old, Failure{JobID: "job-x", At: now.Add(-48 * time.Hour)})
	}
	if AlertOnFailingLoop(old, 24*time.Hour, now) {
		t.Fatal("failures outside the window must not raise")
	}
}

// compile-time guard on the recRow shape (kept for the PR body's row fields).
var _ = recRow{}
