package auditjoin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGApprovals reads approved publish_approvals row ids (the workflow_id is
// the join key the proxy logs). Shared by the nightly join and the per-step
// audit record.
type PGApprovals struct{ Pool *pgxpool.Pool }

// ApprovedIDs implements ApprovalsReader.
func (p PGApprovals) ApprovedIDs(ctx context.Context, since time.Time) (map[string]struct{}, error) {
	rows, err := p.Pool.Query(ctx,
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
