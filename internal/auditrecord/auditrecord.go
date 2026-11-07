// Package auditrecord writes the audit record of a listing job: one board
// note per step (research, price, publish, verify, rollback) carrying the
// step's cost from the cost ledger, the model(s) it called, the run id, the
// counting-proxy verdict for the steps that write, and the failure when the
// step failed. The weekly board report reads the notes back through KPI.
//
// The record is part of the job, not decoration: a note the board refuses
// comes back to the caller as ErrNotRecorded, and a writing step whose
// writes the proxy never saw is NOT_RUN, never OK.
package auditrecord

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/auditjoin"
	"github.com/nfsarch33/agentic-ecommerce/internal/costledger"
)

// Step is one stage of a listing job.
type Step string

// The five steps, in the order a listing job runs them.
const (
	Research Step = "research"
	Price    Step = "price"
	Publish  Step = "publish"
	Verify   Step = "verify"
	Rollback Step = "rollback"
)

// Steps lists every step in job order.
var Steps = []Step{Research, Price, Publish, Verify, Rollback}

// Valid reports whether s is one of the five steps.
func (s Step) Valid() bool {
	for _, k := range Steps {
		if s == k {
			return true
		}
	}
	return false
}

// Writes reports whether the step writes to the store (and so has a
// counting-proxy verdict).
func (s Step) Writes() bool { return s == Publish || s == Rollback }

// Status of a recorded step.
type Status string

// Step statuses.
const (
	StatusOK      Status = "ok"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

func (s Status) valid() bool { return s == StatusOK || s == StatusFailed || s == StatusSkipped }

// ErrNotRecorded marks an audit record the board did not take (or that the
// recorder could not build). The job must treat it as an audit gap.
var ErrNotRecorded = errors.New("auditrecord: step not recorded")

// Comment is one board note on a ticket.
type Comment struct {
	Body      string
	CreatedAt time.Time
}

// Board lists and posts ticket notes.
type Board interface {
	Comments(ctx context.Context, ticket string) ([]Comment, error)
	Comment(ctx context.Context, ticket, body string) error
}

// Auditor returns the counting-proxy verdict for the writes one approval
// authorised.
type Auditor interface {
	Audit(ctx context.Context, approvalID string) auditjoin.Result
}

// ProxyAuditor audits through the counting proxy's log (auditjoin).
type ProxyAuditor struct {
	LogPath   string
	Approvals auditjoin.ApprovalsReader
	Since     time.Time
}

// Audit implements Auditor.
func (a ProxyAuditor) Audit(ctx context.Context, approvalID string) auditjoin.Result {
	return auditjoin.JoinApproval(ctx, a.LogPath, a.Approvals, a.Since, approvalID)
}

// AuditLine is the proxy verdict as it rides a note.
type AuditLine struct {
	Status    auditjoin.Status
	Writes    int
	Unmatched int
	Note      string
}

// Note is one step's audit record.
type Note struct {
	Step    Step
	JobID   string
	RunID   string
	Status  Status
	Cost    costledger.Summary
	CostErr string     // set when the ledger could not be read
	Audit   *AuditLine // writing steps that ran; nil otherwise
	Failure string
	Detail  string
}

const header = "AUDIT listing-step"

func (n Note) key() string {
	return fmt.Sprintf("%s step=%s job=%s run=%s ", header, n.Step, n.JobID, n.RunID)
}

// Body renders the note. The first line is the record key; ParseNote reads
// it back.
func (n Note) Body() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%sstatus=%s\n", n.key(), n.Status)
	if n.CostErr != "" {
		fmt.Fprintf(&b, "cost=unknown (%s)\n", oneLine(n.CostErr))
	} else {
		models := "-"
		if len(n.Cost.Models) > 0 {
			models = strings.Join(n.Cost.Models, ",")
		}
		fmt.Fprintf(&b, "cost_cents=%d calls=%d errors=%d tokens_in=%d tokens_out=%d models=%s\n",
			n.Cost.CostCents, n.Cost.Calls, n.Cost.Errors, n.Cost.TokensIn, n.Cost.TokensOut, models)
	}
	if n.Audit != nil {
		fmt.Fprintf(&b, "audit=%s writes=%d unmatched=%d", n.Audit.Status, n.Audit.Writes, n.Audit.Unmatched)
		if n.Audit.Note != "" {
			fmt.Fprintf(&b, " note=%s", oneLine(n.Audit.Note))
		}
		b.WriteString("\n")
	}
	if n.Failure != "" {
		fmt.Fprintf(&b, "failure: %s\n", oneLine(n.Failure))
	}
	if n.Detail != "" {
		fmt.Fprintf(&b, "detail: %s\n", oneLine(n.Detail))
	}
	return strings.TrimRight(b.String(), "\n")
}

// ParseNote reads a note back from a comment body. Comments that are not
// audit records return ok=false.
func ParseNote(body string) (Note, bool) {
	lines := strings.Split(body, "\n")
	n, ok := parseKey(lines[0])
	if !ok {
		return Note{}, false
	}
	for _, l := range lines[1:] {
		parseLine(&n, l)
	}
	return n, true
}

// parseKey reads the record key line; anything else is not a record.
func parseKey(line string) (Note, bool) {
	rest, ok := strings.CutPrefix(line, header+" ")
	if !ok {
		return Note{}, false
	}
	f := fields(rest)
	n := Note{Step: Step(f["step"]), JobID: f["job"], RunID: f["run"], Status: Status(f["status"])}
	if !n.Step.Valid() || n.JobID == "" || n.RunID == "" || !n.Status.valid() {
		return Note{}, false
	}
	return n, true
}

func parseLine(n *Note, l string) {
	switch {
	case strings.HasPrefix(l, "cost=unknown ("):
		n.CostErr = strings.TrimSuffix(strings.TrimPrefix(l, "cost=unknown ("), ")")
	case strings.HasPrefix(l, "cost_cents="):
		n.Cost = parseCost(fields(l))
	case strings.HasPrefix(l, "audit="):
		n.Audit = parseAudit(l)
	case strings.HasPrefix(l, "failure: "):
		n.Failure = strings.TrimPrefix(l, "failure: ")
	case strings.HasPrefix(l, "detail: "):
		n.Detail = strings.TrimPrefix(l, "detail: ")
	}
}

func parseCost(f map[string]string) costledger.Summary {
	s := costledger.Summary{
		CostCents: atoi64(f["cost_cents"]),
		Calls:     int(atoi64(f["calls"])),
		Errors:    int(atoi64(f["errors"])),
		TokensIn:  atoi64(f["tokens_in"]),
		TokensOut: atoi64(f["tokens_out"]),
	}
	if m := f["models"]; m != "" && m != "-" {
		s.Models = strings.Split(m, ",")
	}
	return s
}

func parseAudit(l string) *AuditLine {
	f := fields(l)
	a := &AuditLine{Status: auditjoin.Status(f["audit"]), Writes: int(atoi64(f["writes"])), Unmatched: int(atoi64(f["unmatched"]))}
	if _, note, ok := strings.Cut(l, " note="); ok {
		a.Note = note
	}
	return a
}

// StepFunc runs one step. Its context carries the ledger attribution for the
// step, so every model call it makes through a costledger.RecordingGenerator
// is billed to this job and step. It returns a one-line detail for the note.
type StepFunc func(ctx context.Context) (detail string, err error)

// Recorder records the steps of one listing job run on its board ticket.
type Recorder struct {
	Board      Board
	Ledger     costledger.Reader
	Auditor    Auditor // nil: writing steps are recorded NOT_RUN
	Ticket     string
	JobID      string
	RunID      string
	TenantID   string
	ApprovalID string // the approval the job's writes ride; publish and rollback are audited against it
}

// Run executes one step and records it. The step's own error is returned
// unchanged; a record the board did not take is joined as ErrNotRecorded. A
// recorder missing its ticket, job or run refuses before the step runs: an
// unrecorded write is worse than no write.
func (r *Recorder) Run(ctx context.Context, step Step, fn StepFunc) error {
	if err := r.check(step); err != nil {
		return err
	}
	stepCtx := costledger.WithAttrs(ctx, costledger.Attrs{JobID: r.JobID, RunID: r.RunID, TenantID: r.TenantID, Action: string(step)})
	detail, err := fn(stepCtx)
	status := StatusOK
	if err != nil {
		status = StatusFailed
	}
	recErr := r.record(ctx, step, status, detail, err)
	if recErr != nil {
		return errors.Join(err, recErr)
	}
	return err
}

// Skip records a step that did not run, with the reason.
func (r *Recorder) Skip(ctx context.Context, step Step, reason string) error {
	if err := r.check(step); err != nil {
		return err
	}
	return r.record(ctx, step, StatusSkipped, reason, nil)
}

// Record records a step whose outcome is already known (a step run by
// another process, e.g. the CLI). failure is empty for a step that passed.
// posted is false when this run and step already carried a note.
func (r *Recorder) Record(ctx context.Context, step Step, status Status, detail, failure string) (posted bool, err error) {
	if err := r.check(step); err != nil {
		return false, err
	}
	if !status.valid() {
		return false, fmt.Errorf("%w: status %q", ErrNotRecorded, status)
	}
	var stepErr error
	if failure != "" {
		stepErr = errors.New(failure)
	}
	return r.post(ctx, step, status, detail, stepErr)
}

func (r *Recorder) check(step Step) error {
	switch {
	case !step.Valid():
		return fmt.Errorf("%w: unknown step %q", ErrNotRecorded, step)
	case r.Board == nil:
		return fmt.Errorf("%w: no board", ErrNotRecorded)
	case strings.TrimSpace(r.Ticket) == "":
		return fmt.Errorf("%w: no ticket", ErrNotRecorded)
	case token(r.JobID) == "" || token(r.RunID) == "":
		return fmt.Errorf("%w: job and run ids are required", ErrNotRecorded)
	}
	return nil
}

func (r *Recorder) record(ctx context.Context, step Step, status Status, detail string, stepErr error) error {
	_, err := r.post(ctx, step, status, detail, stepErr)
	return err
}

func (r *Recorder) post(ctx context.Context, step Step, status Status, detail string, stepErr error) (bool, error) {
	n := Note{Step: step, JobID: token(r.JobID), RunID: token(r.RunID), Status: status, Detail: detail}
	if stepErr != nil {
		n.Failure = stepErr.Error()
	}
	n.Cost, n.CostErr = r.costOf(ctx, step)
	if step.Writes() && status != StatusSkipped {
		n.Audit = r.auditOf(ctx)
	}
	if r.alreadyRecorded(ctx, n) {
		return false, nil
	}
	if err := r.Board.Comment(ctx, r.Ticket, n.Body()); err != nil {
		return false, fmt.Errorf("%w: %s on %s: %v", ErrNotRecorded, step, r.Ticket, err)
	}
	return true, nil
}

func (r *Recorder) costOf(ctx context.Context, step Step) (costledger.Summary, string) {
	if r.Ledger == nil {
		return costledger.Summary{}, "no ledger configured"
	}
	rows, err := r.Ledger.Rows(ctx, r.JobID, r.RunID, string(step))
	if err != nil {
		return costledger.Summary{}, err.Error()
	}
	return costledger.Summarize(rows), ""
}

func (r *Recorder) auditOf(ctx context.Context) *AuditLine {
	if r.Auditor == nil {
		return &AuditLine{Status: auditjoin.StatusNotRun, Note: "no auditor configured"}
	}
	res := r.Auditor.Audit(ctx, r.ApprovalID)
	a := &AuditLine{Status: res.Status, Writes: res.Writes, Unmatched: len(res.Unmatched)}
	if res.Err != nil {
		a.Note = res.Err.Error()
	}
	return a
}

// alreadyRecorded keys the record on run id and step: a re-run of the same
// run posts nothing new. A board that cannot list answers false and the
// note is posted; KPI keeps the first note per key, so a rare duplicate is
// harmless and a lost note is not.
func (r *Recorder) alreadyRecorded(ctx context.Context, n Note) bool {
	existing, err := r.Board.Comments(ctx, r.Ticket)
	if err != nil {
		return false
	}
	for _, c := range existing {
		if p, ok := parseKey(strings.SplitN(c.Body, "\n", 2)[0]); ok && p.Step == n.Step && p.JobID == n.JobID && p.RunID == n.RunID {
			return true
		}
	}
	return false
}

// JobKPI is one listing job run as the weekly report reads it.
type JobKPI struct {
	JobID     string
	RunID     string
	Steps     map[Step]Status
	Complete  bool   // all five steps recorded
	Failed    []Step // in job order
	AuditGaps []Step // writing steps whose proxy verdict is not OK
	CostCents int64
	Last      time.Time
}

// KPI folds the audit notes inside the window ending at now into one row
// per job run, newest first. The first note per (job, run, step) wins.
func KPI(comments []Comment, now time.Time, window time.Duration) []JobKPI {
	from := now.Add(-window)
	byKey := map[string]*JobKPI{}
	var order []*JobKPI
	notes := map[string]map[Step]Note{}
	for _, c := range comments {
		if c.CreatedAt.Before(from) || c.CreatedAt.After(now) {
			continue
		}
		n, ok := ParseNote(c.Body)
		if !ok {
			continue
		}
		k := n.JobID + "\x00" + n.RunID
		j := byKey[k]
		if j == nil {
			j = &JobKPI{JobID: n.JobID, RunID: n.RunID, Steps: map[Step]Status{}}
			byKey[k] = j
			order = append(order, j)
			notes[k] = map[Step]Note{}
		}
		if c.CreatedAt.After(j.Last) {
			j.Last = c.CreatedAt
		}
		if _, dup := j.Steps[n.Step]; dup {
			continue
		}
		j.Steps[n.Step] = n.Status
		notes[k][n.Step] = n
		j.CostCents += n.Cost.CostCents
	}
	out := make([]JobKPI, 0, len(order))
	for _, j := range order {
		k := j.JobID + "\x00" + j.RunID
		for _, s := range Steps {
			n, ok := notes[k][s]
			if !ok {
				continue
			}
			if n.Status == StatusFailed {
				j.Failed = append(j.Failed, s)
			}
			if n.Audit != nil && n.Audit.Status != auditjoin.StatusOK {
				j.AuditGaps = append(j.AuditGaps, s)
			}
		}
		j.Complete = len(j.Steps) == len(Steps)
		out = append(out, *j)
	}
	sortByLastDesc(out)
	return out
}

func sortByLastDesc(js []JobKPI) {
	for i := 1; i < len(js); i++ {
		for k := i; k > 0 && js[k].Last.After(js[k-1].Last); k-- {
			js[k], js[k-1] = js[k-1], js[k]
		}
	}
}

func fields(s string) map[string]string {
	out := map[string]string{}
	for _, f := range strings.Fields(s) {
		if k, v, ok := strings.Cut(f, "="); ok {
			out[k] = v
		}
	}
	return out
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// oneLine keeps a value on one note line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// token makes an id safe for the key line (no spaces, no '=').
func token(s string) string {
	s = strings.Join(strings.Fields(s), "_")
	return strings.ReplaceAll(s, "=", "_")
}
