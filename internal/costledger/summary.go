package costledger

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Summary is the cost of one job step, folded from its ledger rows.
type Summary struct {
	Calls     int
	Errors    int
	TokensIn  int64
	TokensOut int64
	CostCents int64
	Models    []string // distinct, sorted
}

// Summarize folds ledger rows into one Summary. Error rows count as calls
// and as errors; their tokens and cost are whatever the row carries (0 for
// a provider that failed before answering).
func Summarize(rows []Row) Summary {
	var s Summary
	seen := map[string]struct{}{}
	for _, r := range rows {
		s.Calls++
		if r.Status == "error" {
			s.Errors++
		}
		s.TokensIn += r.TokensIn
		s.TokensOut += r.TokensOut
		s.CostCents += r.CostCents
		if r.Model != "" {
			if _, ok := seen[r.Model]; !ok {
				seen[r.Model] = struct{}{}
				s.Models = append(s.Models, r.Model)
			}
		}
	}
	sort.Strings(s.Models)
	return s
}

// Reader reads ledger rows back for one run of a job and one action (the
// step). The run id is part of the key: two runs of one job are two costs.
type Reader interface {
	Rows(ctx context.Context, jobID, runID, action string) ([]Row, error)
}

// Rows returns one run's rows for one action, oldest first.
func (r *PGRecorder) Rows(ctx context.Context, jobID, runID, action string) ([]Row, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT job_id, run_id, tenant_id, action, model, tokens_in, tokens_out, cost_cents, status, error_text
		   FROM cost_ledger WHERE job_id = $1 AND run_id = $2 AND action = $3 ORDER BY id`, jobID, runID, action)
	if err != nil {
		return nil, fmt.Errorf("costledger read: %w", err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var row Row
		if err := rows.Scan(&row.JobID, &row.RunID, &row.TenantID, &row.Action, &row.Model,
			&row.TokensIn, &row.TokensOut, &row.CostCents, &row.Status, &row.ErrorText); err != nil {
			return nil, fmt.Errorf("costledger scan: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("costledger rows: %w", err)
	}
	return out, nil
}

// MemLedger is an in-memory Recorder and Reader, safe for concurrent use:
// the no-database path and tests.
type MemLedger struct {
	mu   sync.Mutex
	rows []Row
}

// NewMemLedger returns an empty in-memory ledger.
func NewMemLedger() *MemLedger { return &MemLedger{} }

// Record appends one row.
func (m *MemLedger) Record(_ context.Context, row Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, row)
	return nil
}

// Rows returns one run's rows for one action, oldest first.
func (m *MemLedger) Rows(_ context.Context, jobID, runID, action string) ([]Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Row
	for _, r := range m.rows {
		if r.JobID == jobID && r.RunID == runID && r.Action == action {
			out = append(out, r)
		}
	}
	return out, nil
}
