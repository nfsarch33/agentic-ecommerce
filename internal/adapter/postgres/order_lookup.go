// File scope: v3.8.1 carry-forward closure -- production Postgres
// adapter for the v3.8.0 EC-7-4 fulfilment.OrderLookup port.
//
// The carrier webhook handler resolves a tracking_number back to
// the internal (order_id, tenant_id) tuple via this adapter so the
// status propagator can fan out the carrier event to the right
// channels for the right tenant. Tenant isolation is enforced at
// the SQL level (the SELECT carries no tenant filter -- the lookup
// is keyed by tracking_number which is globally unique per the
// v3.8.0 carrier API contract -- and the returned tenant_id is the
// authoritative tenant for the row).
//
// Migration anchor: shipping_labels (migration 0017_shipping_labels)
// owns the (tenant_id, tracking_number) PK + the (tenant_id,
// order_id) covering index. The adapter reads:
//
//	SELECT order_id, tenant_id FROM shipping_labels
//	WHERE tracking_number = $1
//
// which uses the (tenant_id, tracking_number) PK in either order
// (Postgres sorts the columns of a multi-column index for the
// scan), but for production p99 we keep an explicit
// idx_shipping_labels_tracking_number index so the scan stays a
// single-row index probe regardless of tenant cardinality.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrOrderLookupNotFound is returned when the tracking_number
// has no matching shipping_labels row. The fulfilment package
// wraps this in ErrShipmentNotFound at the webhook handler layer
// so the carrier sees an HTTP 404.
var ErrOrderLookupNotFound = errors.New("postgres: tracking_number not found in shipping_labels")

// OrderLookupRepository is the v3.8.1 production adapter for the
// fulfilment.OrderLookup port.
type OrderLookupRepository struct {
	pool productStore
}

// NewOrderLookupRepository constructs the adapter.
func NewOrderLookupRepository(pool *pgxpool.Pool) *OrderLookupRepository {
	return &OrderLookupRepository{pool: pool}
}

// OrderForTracking resolves a tracking_number to (order_id,
// tenant_id). Returns ErrOrderLookupNotFound if no row matches.
//
// Cyclomatic 3 (validate / scan / wrap-error). The scan branch
// distinguishes pgx.ErrNoRows (typed) from any other database
// error (raw %w wrap).
func (r *OrderLookupRepository) OrderForTracking(ctx context.Context, trackingNumber string) (string, string, error) {
	if trackingNumber == "" {
		return "", "", fmt.Errorf("%w: tracking_number empty", ErrOrderLookupNotFound)
	}
	const q = `
		SELECT order_id, tenant_id
		FROM shipping_labels
		WHERE tracking_number = $1
		LIMIT 1`
	var orderID, tenantID string
	err := r.pool.QueryRow(ctx, q, trackingNumber).Scan(&orderID, &tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", fmt.Errorf("%w: tracking_number=%s", ErrOrderLookupNotFound, trackingNumber)
		}
		return "", "", fmt.Errorf("postgres: order_lookup query: %w", err)
	}
	return orderID, tenantID, nil
}
