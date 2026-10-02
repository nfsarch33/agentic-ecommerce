// Package costledger records one row per model call (v18870-2): job- and
// tenant-attributed, with tokens and an AUD-cents estimate from
// internal/costcalc. The RecordingGenerator decorates the AI port so every
// call — success or failure — lands in the ledger; the same-day alert on a
// failing loop reads the recorded attempts.
package costledger

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nfsarch33/agentic-ecommerce/internal/costcalc"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// Row is one ledger entry.
type Row struct {
	JobID     string
	TenantID  string
	Action    string
	Model     string
	TokensIn  int64
	TokensOut int64
	CostCents int64
	Status    string // ok | error
	ErrorText string
}

// Recorder persists ledger rows.
type Recorder interface {
	Record(ctx context.Context, row Row) error
}

// PGRecorder writes rows to the cost_ledger table.
type PGRecorder struct{ pool *pgxpool.Pool }

// NewPGRecorder returns a Postgres-backed recorder.
func NewPGRecorder(pool *pgxpool.Pool) *PGRecorder { return &PGRecorder{pool: pool} }

// Record inserts one row.
func (r *PGRecorder) Record(ctx context.Context, row Row) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO cost_ledger (job_id, tenant_id, action, model, tokens_in, tokens_out, cost_cents, status, error_text)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		row.JobID, row.TenantID, row.Action, row.Model, row.TokensIn, row.TokensOut, row.CostCents, row.Status, row.ErrorText)
	if err != nil {
		return fmt.Errorf("costledger insert: %w", err)
	}
	return nil
}

// RecordingGenerator decorates an AI port: every Complete call produces
// exactly one ledger row, success or failure. The ledger write is
// best-effort observability — a recording failure must not fail the
// generation (the caller's job is the product, not the ledger).
type RecordingGenerator struct {
	Inner    port.AITextGenerator
	Recorder Recorder
	Prices   costcalc.Table
}

// Complete runs the inner generator and records the row.
//
// Tokens: providers that report the split fill tokens_in/tokens_out; a
// total-only response lands entirely in tokens_out and is priced at the
// OUTPUT rate — an upper bound, stated here rather than silently zero.
// The row is recorded on an uncancellable context with a short deadline:
// the likeliest failing loop fails WITH a cancelled context, and that is
// exactly the row the alert must see.
func (g *RecordingGenerator) Complete(ctx context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	att := AttrFrom(ctx)
	resp, err := g.Inner.Complete(ctx, req)
	row := Row{
		JobID:    att.JobID,
		TenantID: att.TenantID,
		Action:   att.Action,
		Model:    req.Model,
		Status:   "ok",
	}
	if err != nil {
		row.Status = "error"
		row.ErrorText = err.Error()
	} else {
		row.TokensIn = clamp64(resp.TokensIn)
		row.TokensOut = clamp64(resp.TokensOut)
		if row.TokensOut == 0 && resp.TokensUsed > 0 {
			row.TokensOut = clamp64(resp.TokensUsed)
		}
		if cost, cerr := g.Prices.Cost(req.Model, row.TokensIn, row.TokensOut); cerr == nil {
			row.CostCents = cost
		}
	}
	recCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = g.Recorder.Record(recCtx, row) // best-effort: a ledger hiccup must not poison the product call
	return resp, err
}

func clamp64(n int) int64 {
	if n < 0 {
		return 0
	}
	return int64(n)
}

// DiscardRecorder drops rows (used when no DSN is configured: the worker
// must still boot and serve product calls without the ledger).
type DiscardRecorder struct{}

// Record implements Recorder by discarding.
func (DiscardRecorder) Record(context.Context, Row) error { return nil }
