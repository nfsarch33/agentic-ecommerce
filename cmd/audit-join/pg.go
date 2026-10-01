package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pgApprovals reads approved publish_approvals row ids (the workflow_id is
// the join key the proxy logs).
type pgApprovals struct{ pool *pgxpool.Pool }

func (p pgApprovals) ApprovedIDs(ctx context.Context, since time.Time) (map[string]struct{}, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT workflow_id FROM publish_approvals WHERE decision = 'approved' AND created_at >= $1`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = struct{}{}
	}
	return ids, rows.Err()
}
