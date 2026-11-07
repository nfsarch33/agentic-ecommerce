package auditrecord

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/auditjoin"
	"github.com/nfsarch33/agentic-ecommerce/internal/costcalc"
	"github.com/nfsarch33/agentic-ecommerce/internal/costledger"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// fakeBoard is an in-memory board: one comment list per ticket.
type fakeBoard struct {
	mu       sync.Mutex
	now      time.Time
	comments map[string][]Comment
	postErr  error
	listErr  error
}

func newFakeBoard(now time.Time) *fakeBoard {
	return &fakeBoard{now: now, comments: map[string][]Comment{}}
}

func (b *fakeBoard) Comments(_ context.Context, ticket string) ([]Comment, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listErr != nil {
		return nil, b.listErr
	}
	return append([]Comment(nil), b.comments[ticket]...), nil
}

func (b *fakeBoard) Comment(_ context.Context, ticket, body string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.postErr != nil {
		return b.postErr
	}
	b.comments[ticket] = append(b.comments[ticket], Comment{Body: body, CreatedAt: b.now})
	return nil
}

func (b *fakeBoard) notes(ticket string) []Note {
	var out []Note
	for _, c := range b.comments[ticket] {
		if n, ok := ParseNote(c.Body); ok {
			out = append(out, n)
		}
	}
	return out
}

// fakeGen answers every completion with a fixed token split.
type fakeGen struct {
	in, out int
	err     error
}

func (g fakeGen) Complete(context.Context, port.AICompletionRequest) (port.AICompletionResponse, error) {
	if g.err != nil {
		return port.AICompletionResponse{}, g.err
	}
	return port.AICompletionResponse{Content: "ok", TokensIn: g.in, TokensOut: g.out, TokensUsed: g.in + g.out}, nil
}

type stubApprovals map[string]struct{}

func (s stubApprovals) ApprovedIDs(context.Context, time.Time) (map[string]struct{}, error) {
	return s, nil
}

func proxyLog(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "audit.ndjson")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	return p
}

var testPrices = costcalc.Table{"test-model": {InputCentsPerMTok: 100, OutputCentsPerMTok: 400}}

const ticket = "TICKET-1"

func newRecorder(b Board, ledger *costledger.MemLedger, aud Auditor, now time.Time) *Recorder {
	return &Recorder{
		Board:      b,
		Ledger:     ledger,
		Auditor:    aud,
		Ticket:     ticket,
		JobID:      "job-7",
		RunID:      "run-42",
		TenantID:   "own-shop",
		ApprovalID: "appr-1",
	}
}

// runListingJob drives the five steps the way a listing job does: research
// calls a model (so it has a cost), publish writes through the counting
// proxy (so it has an audit verdict), rollback has nothing to undo.
func runListingJob(t *testing.T, r *Recorder, ledger *costledger.MemLedger, publishErr error) error {
	t.Helper()
	ctx := context.Background()
	gen := &costledger.RecordingGenerator{Inner: fakeGen{in: 120000, out: 30000}, Recorder: ledger, Prices: testPrices}
	if err := r.Run(ctx, Research, func(ctx context.Context) (string, error) {
		_, err := gen.Complete(ctx, port.AICompletionRequest{Model: "test-model"})
		return "5 candidates", err
	}); err != nil {
		return err
	}
	if err := r.Run(ctx, Price, func(context.Context) (string, error) { return "rule band 1.6x", nil }); err != nil {
		return err
	}
	if err := r.Run(ctx, Publish, func(context.Context) (string, error) { return "listed sku A1", publishErr }); err != nil {
		if serr := r.Skip(ctx, Verify, "publish failed"); serr != nil {
			t.Fatalf("skip verify: %v", serr)
		}
		_ = r.Run(ctx, Rollback, func(context.Context) (string, error) { return "nothing listed", nil })
		return err
	}
	if err := r.Run(ctx, Verify, func(context.Context) (string, error) { return "PASS 4/4 fields", nil }); err != nil {
		return err
	}
	return r.Skip(ctx, Rollback, "publish verified, nothing to roll back")
}

// Scenario: Given one listing job When it completes Then the board ticket for
// the job carries five step notes with cost and run id, and the 7-day KPI
// query returns the job.
func TestAcceptance_CompletedJobCarriesFiveStepNotesAndKPIReturnsIt(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	board := newFakeBoard(now)
	ledger := costledger.NewMemLedger()
	log := proxyLog(t, `{"kind":"intent","method":"POST","path":"/wp-json/wc/v3/products","approval_id":"appr-1"}`,
		`{"kind":"done","method":"POST","path":"/wp-json/wc/v3/products","approval_id":"appr-1"}`)
	aud := ProxyAuditor{LogPath: log, Approvals: stubApprovals{"appr-1": {}}, Since: now.Add(-24 * time.Hour)}
	r := newRecorder(board, ledger, aud, now)

	if err := runListingJob(t, r, ledger, nil); err != nil {
		t.Fatalf("job: %v", err)
	}
	notes := board.notes(ticket)
	if len(notes) != 5 {
		t.Fatalf("notes = %d, want 5 (one per step)", len(notes))
	}
	for i, n := range notes {
		if n.Step != Steps[i] {
			t.Errorf("note %d step = %s, want %s", i, n.Step, Steps[i])
		}
		if n.RunID != "run-42" || n.JobID != "job-7" {
			t.Errorf("note %d run/job = %s/%s", i, n.RunID, n.JobID)
		}
		if !strings.Contains(board.comments[ticket][i].Body, "cost_cents=") {
			t.Errorf("note %d carries no cost line: %q", i, board.comments[ticket][i].Body)
		}
	}
	// research: 120k in at 100c/M + 30k out at 400c/M = 12 + 12 = 24 cents
	if got := notes[0].Cost.CostCents; got != 24 {
		t.Errorf("research cost_cents = %d, want 24", got)
	}
	if notes[0].Cost.Models[0] != "test-model" {
		t.Errorf("research model = %v", notes[0].Cost.Models)
	}
	if notes[1].Cost.CostCents != 0 || notes[1].Cost.Calls != 0 {
		t.Errorf("price step was attributed someone else's cost: %+v", notes[1].Cost)
	}
	if notes[2].Audit == nil || notes[2].Audit.Status != auditjoin.StatusOK || notes[2].Audit.Writes != 1 {
		t.Errorf("publish audit = %+v, want OK with 1 write", notes[2].Audit)
	}
	if notes[4].Status != StatusSkipped {
		t.Errorf("rollback status = %s, want skipped", notes[4].Status)
	}

	jobs := KPI(board.comments[ticket], now, 7*24*time.Hour)
	if len(jobs) != 1 {
		t.Fatalf("KPI jobs = %d, want 1", len(jobs))
	}
	j := jobs[0]
	if j.JobID != "job-7" || j.RunID != "run-42" || !j.Complete || len(j.Failed) != 0 || j.CostCents != 24 {
		t.Errorf("KPI row = %+v", j)
	}
}

// Scenario: Given a step fails Then its note names the failure.
func TestAcceptance_FailedStepNoteNamesTheFailure(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	board := newFakeBoard(now)
	ledger := costledger.NewMemLedger()
	aud := ProxyAuditor{LogPath: proxyLog(t, `{"kind":"intent","method":"GET","path":"/x"}`), Approvals: stubApprovals{}, Since: now.Add(-time.Hour)}
	r := newRecorder(board, ledger, aud, now)

	err := runListingJob(t, r, ledger, errors.New("store lookup failed: 503"))
	if err == nil || !strings.Contains(err.Error(), "store lookup failed: 503") {
		t.Fatalf("job err = %v, want the publish failure returned to the caller", err)
	}
	notes := board.notes(ticket)
	if len(notes) != 5 {
		t.Fatalf("notes = %d, want 5 (failed publish, skipped verify, rollback still recorded)", len(notes))
	}
	pub := notes[2]
	if pub.Status != StatusFailed || pub.Failure != "store lookup failed: 503" {
		t.Errorf("publish note = %+v, want failed naming the store error", pub)
	}
	if !strings.Contains(board.comments[ticket][2].Body, "failure: store lookup failed: 503") {
		t.Errorf("publish body does not name the failure: %q", board.comments[ticket][2].Body)
	}
	jobs := KPI(board.comments[ticket], now, 7*24*time.Hour)
	if len(jobs) != 1 || jobs[0].Complete != true || len(jobs[0].Failed) != 1 || jobs[0].Failed[0] != Publish {
		t.Errorf("KPI = %+v, want the job with publish failed", jobs)
	}
}

// MUTANT: drop the existing-note check and a re-run doubles every note.
func TestRerunOfTheSameRunAndStepPostsOnce(t *testing.T) {
	now := time.Now()
	board := newFakeBoard(now)
	r := newRecorder(board, costledger.NewMemLedger(), nil, now)
	for i := 0; i < 2; i++ {
		if err := r.Run(context.Background(), Price, func(context.Context) (string, error) { return "", nil }); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(board.comments[ticket]); n != 1 {
		t.Fatalf("comments = %d after a re-run, want 1 (keyed on run id and step)", n)
	}
	// a NEW run of the same job is a new record
	r.RunID = "run-43"
	_ = r.Run(context.Background(), Price, func(context.Context) (string, error) { return "", nil })
	if n := len(board.comments[ticket]); n != 2 {
		t.Fatalf("comments = %d after a new run, want 2", n)
	}
}

// The step's model calls are attributed to the step: a MUTANT that leaves the
// caller's Action on the context bills research's cost to every step.
func TestRunAttributesModelCallsToTheStep(t *testing.T) {
	ledger := costledger.NewMemLedger()
	r := newRecorder(newFakeBoard(time.Now()), ledger, nil, time.Now())
	ctx := costledger.WithAttrs(context.Background(), costledger.Attrs{Action: "outer"})
	gen := &costledger.RecordingGenerator{Inner: fakeGen{in: 10, out: 10}, Recorder: ledger, Prices: testPrices}
	_ = r.Run(ctx, Verify, func(ctx context.Context) (string, error) {
		_, err := gen.Complete(ctx, port.AICompletionRequest{Model: "test-model"})
		return "", err
	})
	rows, _ := ledger.Rows(context.Background(), "job-7", "verify")
	if len(rows) != 1 || rows[0].TenantID != "own-shop" {
		t.Fatalf("verify rows = %+v, want one row attributed to job-7/verify/own-shop", rows)
	}
}

type failingLedger struct{}

func (failingLedger) Rows(context.Context, string, string) ([]costledger.Row, error) {
	return nil, errors.New("ledger down")
}

// An unreadable ledger must not drop the note: the step still happened.
func TestLedgerUnreadableStillPostsAndSaysSo(t *testing.T) {
	board := newFakeBoard(time.Now())
	r := newRecorder(board, nil, nil, time.Now())
	r.Ledger = failingLedger{}
	if err := r.Run(context.Background(), Price, func(context.Context) (string, error) { return "", nil }); err != nil {
		t.Fatal(err)
	}
	body := board.comments[ticket][0].Body
	if !strings.Contains(body, "cost=unknown (ledger down)") {
		t.Fatalf("body = %q, want the cost marked unknown with the reason", body)
	}
	n, _ := ParseNote(body)
	if n.CostErr != "ledger down" {
		t.Fatalf("parsed CostErr = %q", n.CostErr)
	}
}

// A board that refuses the note is an audit gap the job must see, even when
// the step itself succeeded.
func TestBoardRefusalIsReturnedAsNotRecorded(t *testing.T) {
	board := newFakeBoard(time.Now())
	board.postErr = errors.New("http 500")
	r := newRecorder(board, costledger.NewMemLedger(), nil, time.Now())
	err := r.Run(context.Background(), Price, func(context.Context) (string, error) { return "", nil })
	if !errors.Is(err, ErrNotRecorded) {
		t.Fatalf("err = %v, want ErrNotRecorded", err)
	}
	// and a failed step plus a refused note returns both
	err = r.Run(context.Background(), Publish, func(context.Context) (string, error) { return "", errors.New("boom") })
	if !errors.Is(err, ErrNotRecorded) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want both the step error and ErrNotRecorded", err)
	}
}

// A writing step whose writes the proxy never saw is NOT_RUN, never OK.
func TestPublishWithNoProxyRowForItsApprovalIsNotRun(t *testing.T) {
	now := time.Now()
	board := newFakeBoard(now)
	log := proxyLog(t, `{"kind":"intent","method":"POST","path":"/p","approval_id":"someone-else"}`)
	aud := ProxyAuditor{LogPath: log, Approvals: stubApprovals{"someone-else": {}}, Since: now.Add(-time.Hour)}
	r := newRecorder(board, costledger.NewMemLedger(), aud, now)
	_ = r.Run(context.Background(), Publish, func(context.Context) (string, error) { return "", nil })
	n, _ := ParseNote(board.comments[ticket][0].Body)
	if n.Audit == nil || n.Audit.Status != auditjoin.StatusNotRun {
		t.Fatalf("audit = %+v, want NOT_RUN", n.Audit)
	}
}

// A writing step with no auditor configured says so rather than claiming OK.
func TestWritingStepWithoutAuditorIsNotRun(t *testing.T) {
	board := newFakeBoard(time.Now())
	r := newRecorder(board, costledger.NewMemLedger(), nil, time.Now())
	_ = r.Run(context.Background(), Rollback, func(context.Context) (string, error) { return "", nil })
	n, _ := ParseNote(board.comments[ticket][0].Body)
	if n.Audit == nil || n.Audit.Status != auditjoin.StatusNotRun {
		t.Fatalf("audit = %+v, want NOT_RUN when no auditor is configured", n.Audit)
	}
	// a non-writing step carries no audit line at all
	_ = r.Run(context.Background(), Research, func(context.Context) (string, error) { return "", nil })
	n, _ = ParseNote(board.comments[ticket][1].Body)
	if n.Audit != nil {
		t.Fatalf("research audit = %+v, want none", n.Audit)
	}
}

func TestParseNoteRoundTripAndRejectsOtherComments(t *testing.T) {
	in := Note{Step: Publish, JobID: "j", RunID: "r", Status: StatusFailed,
		Cost:    costledger.Summary{Calls: 2, Errors: 1, TokensIn: 5, TokensOut: 6, CostCents: 7, Models: []string{"a", "b"}},
		Audit:   &AuditLine{Status: auditjoin.StatusUnmatched, Writes: 2, Unmatched: 1},
		Failure: "line one\nline two", Detail: "d"}
	out, ok := ParseNote(in.Body())
	if !ok {
		t.Fatal("did not parse its own body")
	}
	if out.Step != in.Step || out.JobID != "j" || out.RunID != "r" || out.Status != StatusFailed ||
		out.Cost.CostCents != 7 || out.Cost.Calls != 2 || len(out.Cost.Models) != 2 ||
		out.Audit.Status != auditjoin.StatusUnmatched || out.Audit.Unmatched != 1 ||
		out.Failure != "line one line two" || out.Detail != "d" {
		t.Fatalf("round trip = %+v", out)
	}
	for _, other := range []string{"", "CHECKPOINT 1", "AUDIT listing-step step=bogus job=j run=r status=ok", "AUDIT listing-step step=price job= run=r status=ok"} {
		if _, ok := ParseNote(other); ok {
			t.Errorf("parsed a non-record comment: %q", other)
		}
	}
}

func TestKPIWindowAndOrdering(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	mk := func(job, run string, s Step, age time.Duration) Comment {
		return Comment{Body: Note{Step: s, JobID: job, RunID: run, Status: StatusOK}.Body(), CreatedAt: now.Add(-age)}
	}
	cs := []Comment{
		mk("old", "r1", Research, 8*24*time.Hour),
		mk("new", "r2", Research, time.Hour),
		mk("new", "r2", Price, 30*time.Minute),
		mk("mid", "r3", Research, 3*24*time.Hour),
		{Body: "an unrelated note", CreatedAt: now},
	}
	jobs := KPI(cs, now, 7*24*time.Hour)
	if len(jobs) != 2 || jobs[0].JobID != "new" || jobs[1].JobID != "mid" {
		t.Fatalf("jobs = %+v, want new then mid, old outside the window", jobs)
	}
	if jobs[0].Complete || len(jobs[0].Steps) != 2 {
		t.Fatalf("partial job reported complete: %+v", jobs[0])
	}
}
