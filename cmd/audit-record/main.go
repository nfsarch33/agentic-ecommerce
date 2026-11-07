// Command audit-record writes and reads the listing job's audit record on
// the board: one note per step with cost (cost ledger), model, run id, the
// counting-proxy verdict for writing steps, and the failure.
//
//	audit-record step --ticket T --job J --run R --step research|price|publish|verify|rollback
//	                  [--status ok|failed|skipped] [--failure TEXT] [--detail TEXT]
//	                  [--approval ID --log audit.ndjson [--since 24h]]
//	audit-record kpi  --ticket T [--ticket T2 ...] [--since 168h]
//
// Environment (never argv): EC_BOARD_URL, EC_BOARD_TOKEN, EC_BOARD_AUTHOR
// (default listing-job), EC_LEDGER_DSN (cost ledger and approvals; unset =
// the note says cost=unknown). Exit codes: 0 recorded / listed, 1 not
// recorded or the board refused, 2 usage.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nfsarch33/agentic-ecommerce/internal/auditjoin"
	"github.com/nfsarch33/agentic-ecommerce/internal/auditrecord"
	"github.com/nfsarch33/agentic-ecommerce/internal/costledger"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

type env func(string) string

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, getenv env, stdout, stderr io.Writer) int {
	if len(args) < 1 || (args[0] != "step" && args[0] != "kpi") {
		fmt.Fprintln(stderr, "usage: audit-record step|kpi [flags] (see the package doc)")
		return 2
	}
	board, err := boardFrom(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "audit-record:", err)
		return 2
	}
	if args[0] == "kpi" {
		return runKPI(ctx, args[1:], board, stdout, stderr)
	}
	return runStep(ctx, args[1:], board, getenv, stdout, stderr)
}

func boardFrom(getenv env) (auditrecord.HTTPBoard, error) {
	b := auditrecord.HTTPBoard{BaseURL: getenv("EC_BOARD_URL"), Token: getenv("EC_BOARD_TOKEN"), Author: getenv("EC_BOARD_AUTHOR")}
	if b.BaseURL == "" || b.Token == "" {
		return b, errors.New("EC_BOARD_URL and EC_BOARD_TOKEN are required")
	}
	if b.Author == "" {
		b.Author = "listing-job"
	}
	return b, nil
}

func runStep(ctx context.Context, args []string, board auditrecord.Board, getenv env, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("step", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ticket := fs.String("ticket", "", "board ticket of the listing job (required)")
	job := fs.String("job", "", "job id (required)")
	runID := fs.String("run", "", "run id (required)")
	step := fs.String("step", "", "research|price|publish|verify|rollback (required)")
	status := fs.String("status", "ok", "ok|failed|skipped")
	failure := fs.String("failure", "", "the failure, for --status failed")
	detail := fs.String("detail", "", "one-line detail")
	approval := fs.String("approval", "", "approval id the step's writes ride (publish, rollback)")
	logPath := fs.String("log", "", "counting-proxy audit log (publish, rollback)")
	since := fs.Duration("since", 24*time.Hour, "approval window for the proxy join")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st := auditrecord.Status(*status)
	if *ticket == "" || *job == "" || *runID == "" || *step == "" {
		fmt.Fprintln(stderr, "audit-record step: --ticket, --job, --run and --step are required")
		return 2
	}
	if st == auditrecord.StatusFailed && strings.TrimSpace(*failure) == "" {
		fmt.Fprintln(stderr, "audit-record step: --status failed needs --failure naming it")
		return 2
	}
	rec := &auditrecord.Recorder{Board: board, Ticket: *ticket, JobID: *job, RunID: *runID, ApprovalID: *approval}
	if dsn := getenv("EC_LEDGER_DSN"); dsn != "" {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			fmt.Fprintf(stderr, "audit-record step: ledger: %v\n", err)
		} else {
			defer pool.Close()
			rec.Ledger = costledger.NewPGRecorder(pool)
			if *logPath != "" {
				rec.Auditor = auditrecord.ProxyAuditor{LogPath: *logPath, Approvals: auditjoin.PGApprovals{Pool: pool}, Since: time.Now().Add(-*since)}
			}
		}
	}
	posted, err := rec.Record(ctx, auditrecord.Step(*step), st, *detail, *failure)
	if err != nil {
		fmt.Fprintln(stderr, "audit-record step:", err)
		if errors.Is(err, auditrecord.ErrNotRecorded) {
			return 1
		}
		return 2
	}
	verb := "RECORDED"
	if !posted {
		verb = "ALREADY RECORDED (this run and step carry a note; nothing posted)"
	}
	fmt.Fprintf(stdout, "%s step=%s job=%s run=%s status=%s ticket=%s\n", verb, *step, *job, *runID, st, *ticket)
	return 0
}

func runKPI(ctx context.Context, args []string, board auditrecord.Board, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kpi", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var tickets multi
	fs.Var(&tickets, "ticket", "board ticket to read (repeatable, required)")
	since := fs.Duration("since", 7*24*time.Hour, "window")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(tickets) == 0 {
		fmt.Fprintln(stderr, "audit-record kpi: at least one --ticket is required")
		return 2
	}
	now := time.Now()
	var jobs []auditrecord.JobKPI
	for _, t := range tickets {
		cs, err := board.Comments(ctx, t)
		if err != nil {
			fmt.Fprintf(stderr, "audit-record kpi: %v\n", err)
			return 1
		}
		jobs = append(jobs, auditrecord.KPI(cs, now, *since)...)
	}
	complete := 0
	for _, j := range jobs {
		if j.Complete {
			complete++
		}
	}
	fmt.Fprintf(stdout, "jobs=%d complete=%d window=%s\n", len(jobs), complete, *since)
	for _, j := range jobs {
		fmt.Fprintf(stdout, "job=%s run=%s steps=%d/%d complete=%t failed=%s audit_gaps=%s cost_cents=%d last=%s\n",
			j.JobID, j.RunID, len(j.Steps), len(auditrecord.Steps), j.Complete, list(j.Failed), list(j.AuditGaps), j.CostCents, j.Last.Format(time.RFC3339))
	}
	return 0
}

func list(steps []auditrecord.Step) string {
	if len(steps) == 0 {
		return "-"
	}
	s := make([]string, len(steps))
	for i, v := range steps {
		s[i] = string(v)
	}
	return strings.Join(s, ",")
}
