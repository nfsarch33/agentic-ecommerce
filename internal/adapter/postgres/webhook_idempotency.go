// File scope: v6.1.0 CF-15 Postgres adapter for the webhook
// IdempotencyStore port.
//
// Design:
//   - INSERT ... ON CONFLICT DO NOTHING is the atomic claim primitive;
//     a non-empty RETURNING tenant_id is the "first observation"
//     signal. The in-memory implementation under
//     internal/webhook.NewMemoryIdempotencyStore remains as a test
//     double for handler-level tests that do not need a real DB.
//   - Implements internal/webhook.IdempotencyStore so the adapter
//     drops in transparently at the composition root.
//
// Coupling: this file imports internal/webhook for the typed
// ErrWebhookPayloadInvalid sentinel and the IdempotencyStore port
// interface; that import goes one direction only (adapter -> domain)
// per the Clean Architecture seam already established in this
// package.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nfsarch33/helixon-ec/internal/webhook"
)

// WebhookIdempotencyStore is the Postgres-backed implementation of
// the webhook.IdempotencyStore port. Goroutine-safe via pgxpool.
type WebhookIdempotencyStore struct {
	pool productStore
}

// NewWebhookIdempotencyStore wires the adapter to a pgx pool.
func NewWebhookIdempotencyStore(pool *pgxpool.Pool) *WebhookIdempotencyStore {
	return &WebhookIdempotencyStore{pool: pool}
}

// Reserve atomically claims the (tenantID, key) tuple. Returns
// (true, nil) on first observation and (false, nil) when the row
// already exists (duplicate webhook delivery).
//
// Empty tenantID or key returns webhook.ErrWebhookPayloadInvalid so
// the adapter mirrors the in-memory shape.
func (s *WebhookIdempotencyStore) Reserve(ctx context.Context, tenantID, key string) (bool, error) {
	if tenantID == "" || key == "" {
		return false, webhook.ErrWebhookPayloadInvalid
	}
	const q = `
		INSERT INTO webhook_idempotency (tenant_id, idempotency_key)
		VALUES ($1, $2)
		ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
		RETURNING tenant_id`
	var inserted string
	err := s.pool.QueryRow(ctx, q, tenantID, key).Scan(&inserted)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("webhook_idempotency reserve: %w", err)
}

// Static interface adherence assertion. Keeps the adapter in lock
// step with the port surface; compilation fails fast if either side
// drifts.
var _ webhook.IdempotencyStore = (*WebhookIdempotencyStore)(nil)
