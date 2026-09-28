package publishgate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is the Postgres implementation of Store over the two gate tables.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore builds a PGStore from a pool. The pool is not closed here.
func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

func decisionOf(workflowID, tenantID, productID, decision, actor, reason, updateID string) Decision {
	return Decision{
		WorkflowID: workflowID, TenantID: tenantID, ProductID: productID,
		Approved: decision == "approved", Actor: actor, Reason: reason, UpdateID: updateID,
	}
}

// RecordApproval inserts the decision; ON CONFLICT keeps the FIRST decision
// and returns it with inserted=false.
func (s *PGStore) RecordApproval(ctx context.Context, d Decision) (Decision, bool, error) {
	const q = `INSERT INTO publish_approvals (workflow_id, tenant_id, product_id, decision, actor, reason, update_id)
	           VALUES ($1,$2,$3,$4,$5,$6,$7)
	           ON CONFLICT (workflow_id) DO NOTHING
	           RETURNING decision`
	var decision string
	err := s.pool.QueryRow(ctx, q, d.WorkflowID, d.TenantID, d.ProductID,
		map[bool]string{true: "approved", false: "rejected"}[d.Approved], d.Actor, d.Reason, d.UpdateID,
	).Scan(&decision)
	switch {
	case err == nil:
		return d, true, nil
	case errors.Is(err, pgx.ErrNoRows):
		stored, gerr := s.ApprovalFor(ctx, d.WorkflowID)
		if gerr != nil {
			return Decision{}, false, fmt.Errorf("record approval conflict read: %w", gerr)
		}
		return stored, false, nil
	default:
		return Decision{}, false, fmt.Errorf("record approval: %w", err)
	}
}

// ApprovalFor loads the decision for a workflow.
func (s *PGStore) ApprovalFor(ctx context.Context, workflowID string) (Decision, error) {
	const q = `SELECT workflow_id, tenant_id, product_id::text, decision, actor, reason, update_id
	           FROM publish_approvals WHERE workflow_id = $1`
	var d Decision
	var decision string
	err := s.pool.QueryRow(ctx, q, workflowID).
		Scan(&d.WorkflowID, &d.TenantID, &d.ProductID, &decision, &d.Actor, &d.Reason, &d.UpdateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Decision{}, ErrNoApproval
	}
	if err != nil {
		return Decision{}, fmt.Errorf("approval for: %w", err)
	}
	d.Approved = decision == "approved"
	return d, nil
}

// Claim inserts or claims the ledger row (design 4.1). One UPDATE decides:
// attempts+1 and the lease move only when the row is not completed and the
// lease is free or expired (or ours, for heartbeat re-claim).
func (s *PGStore) Claim(ctx context.Context, idemKey, tenantID, workflowID, productID, sku, fingerprint, owner string, leaseTTL time.Duration) error {
	const ins = `INSERT INTO publish_ledger
	               (idem_key, tenant_id, workflow_id, product_id, sku, fingerprint, status, attempts, lease_owner, lease_until)
	             VALUES ($1,$2,$3,$4,$5,$6,'in_progress',1,$7,now()+($8::interval))
	             ON CONFLICT (idem_key) DO NOTHING`
	if _, err := s.pool.Exec(ctx, ins, idemKey, tenantID, workflowID, productID, sku, fingerprint, owner, leaseTTL.String()); err != nil {
		return fmt.Errorf("claim insert: %w", err)
	}
	const upd = `UPDATE publish_ledger SET
	               attempts = attempts + 1,
	               status = 'in_progress',
	               lease_owner = $2,
	               lease_until = now() + ($3::interval)
	             WHERE idem_key = $1
	               AND status <> 'completed'
	               AND (lease_until IS NULL OR lease_until < now() OR lease_owner = $2)
	             RETURNING status`
	var status string
	err := s.pool.QueryRow(ctx, upd, idemKey, owner, leaseTTL.String()).Scan(&status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Either completed (short-circuit) or lease held; distinguish.
		var cur string
		qErr := s.pool.QueryRow(ctx, `SELECT status FROM publish_ledger WHERE idem_key = $1`, idemKey).Scan(&cur)
		if qErr != nil {
			return fmt.Errorf("claim distinguish: %w", qErr)
		}
		if cur == "completed" {
			return ErrAlreadyCompleted
		}
		return ErrLeaseHeld
	case err == nil:
		return nil
	default:
		return fmt.Errorf("claim update: %w", err)
	}
}

// Complete finalises the row.
func (s *PGStore) Complete(ctx context.Context, idemKey, status, writeKind, remoteID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE publish_ledger SET status=$2, write_kind=$3, remote_id=$4, completed_at=now(), lease_owner=NULL, lease_until=NULL
		 WHERE idem_key=$1`, idemKey, status, writeKind, nullable(remoteID))
	if err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	return nil
}

// Fail marks the row failed and releases the lease for the next attempt.
func (s *PGStore) Fail(ctx context.Context, idemKey string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE publish_ledger SET status='failed', lease_owner=NULL, lease_until=NULL WHERE idem_key=$1`, idemKey)
	if err != nil {
		return fmt.Errorf("fail: %w", err)
	}
	return nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
