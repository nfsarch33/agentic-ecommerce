// Package postgres refresh_executor.go — v6.3.0 Pair 3 MVP / CF-14:
// Postgres adapter for the GMV daily REFRESH activity. Wraps a
// pgxpool.Pool so the workflow.RefreshExecutor port can drive a
// REFRESH MATERIALIZED VIEW CONCURRENTLY without depending on the
// full PostgresRepository surface.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RefreshExecutor is a pgx-backed implementation of the
// workflow.RefreshExecutor port.
type RefreshExecutor struct {
	pool *pgxpool.Pool
}

// NewRefreshExecutor constructs a RefreshExecutor.
func NewRefreshExecutor(pool *pgxpool.Pool) (*RefreshExecutor, error) {
	if pool == nil {
		return nil, errors.New("postgres refresh executor: pool required")
	}
	return &RefreshExecutor{pool: pool}, nil
}

// ExecRefresh runs the supplied REFRESH SQL and returns the rows
// affected. pgx returns -1 for statements without a row count
// (REFRESH MATERIALIZED VIEW does not report rows); we normalise
// that to 0 so callers can treat the result as "completed without
// row delta".
func (r *RefreshExecutor) ExecRefresh(ctx context.Context, sql string) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, errors.New("postgres refresh executor: not initialised")
	}
	tag, err := r.pool.Exec(ctx, sql)
	if err != nil {
		return 0, fmt.Errorf("postgres refresh exec: %w", err)
	}
	if rows := tag.RowsAffected(); rows >= 0 {
		return rows, nil
	}
	return 0, nil
}
