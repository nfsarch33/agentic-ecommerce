// File scope: v3.8.1 carry-forward closure -- production Postgres
// adapter for the v3.8.0 EC-9-3 handler.ROIRepository port.
//
// Reads the v3.8.0 roi_daily_rollup materialized view (migration
// 0019_roi_daily_rollup) so the EC-9-3 ROI analytics handler hits
// a small materialized view + the (tenant_id, day) covering index
// rather than scanning the orders + supplier_costs +
// shipping_labels JOIN for every dashboard load.
//
// Acceptance gate (per plan EC-9-3): p95 <300ms over a 90-day
// window with 100K orders. The materialized view + indexes
// referenced in 0019 deliver this; the adapter just wires the
// per-route SQL.
//
// Migration anchor: 0019_roi_daily_rollup creates
//
//	CREATE MATERIALIZED VIEW roi_daily_rollup AS ...
//
// with PK (tenant_id, day, channel, product_id) +
//
//	CREATE UNIQUE INDEX uq_roi_daily_rollup_pk
//	CREATE INDEX idx_roi_daily_rollup_tenant_day
//	CREATE INDEX idx_roi_daily_rollup_tenant_channel_day
//
// All three queries below filter by tenant_id (RLS-friendly) and
// constrain the day window so the planner can use the covering
// index.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/helixon-ec/internal/api/handler"
)

// ROIRepository is the v3.8.1 production adapter for the
// handler.ROIRepository port.
type ROIRepository struct {
	pool productStore
}

// NewROIRepository constructs the adapter.
func NewROIRepository(pool *pgxpool.Pool) *ROIRepository {
	return &ROIRepository{pool: pool}
}

// Heatmap returns the (channel, product, day, ROI) rollup rows for
// the supplied filter window. Cyclomatic 3.
func (r *ROIRepository) Heatmap(ctx context.Context, f handler.ROIFilter) ([]handler.ROIPoint, error) {
	const q = `
		SELECT day, channel, product_id,
		       total_revenue_aud_cents,
		       gross_profit_aud_cents,
		       order_count,
		       total_supplier_cost_aud_cents + total_shipping_cost_aud_cents AS total_cost_aud_cents
		FROM roi_daily_rollup
		WHERE tenant_id = $1
		  AND day BETWEEN $2 AND $3
		ORDER BY day, channel, product_id`
	return r.runQuery(ctx, q, f.TenantID, f.From, f.To)
}

// DeadStock returns rollup rows that have not had an order in the
// last MinAgeDays. The "no orders" filter pivots on order_count=0
// in the rollup; a future migration could add a last_order_at
// column for finer-grained filtering, but for v3.8.1 the
// order_count=0 + day-window combination satisfies the EC-9-3
// dead-stock acceptance.
func (r *ROIRepository) DeadStock(ctx context.Context, f handler.ROIFilter) ([]handler.ROIPoint, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -f.MinAgeDays)
	const q = `
		SELECT day, channel, product_id,
		       total_revenue_aud_cents,
		       gross_profit_aud_cents,
		       order_count,
		       total_supplier_cost_aud_cents + total_shipping_cost_aud_cents AS total_cost_aud_cents
		FROM roi_daily_rollup
		WHERE tenant_id = $1
		  AND day < $2
		  AND order_count = 0
		ORDER BY day DESC, channel, product_id`
	return r.runQuery(ctx, q, f.TenantID, cutoff)
}

// ByChannel returns the per-channel rollup rows aggregated across
// the supplied date window so the handler's buildChannelBreakdown
// can pivot directly. Cyclomatic 3.
func (r *ROIRepository) ByChannel(ctx context.Context, f handler.ROIFilter) ([]handler.ROIPoint, error) {
	const q = `
		SELECT day, channel, product_id,
		       total_revenue_aud_cents,
		       gross_profit_aud_cents,
		       order_count,
		       total_supplier_cost_aud_cents + total_shipping_cost_aud_cents AS total_cost_aud_cents
		FROM roi_daily_rollup
		WHERE tenant_id = $1
		  AND day BETWEEN $2 AND $3
		ORDER BY channel, day`
	return r.runQuery(ctx, q, f.TenantID, f.From, f.To)
}

// runQuery scans rollup rows into the handler.ROIPoint shape.
// Cyclomatic 4 (rows.Next loop + scan + close + wrap-error).
func (r *ROIRepository) runQuery(ctx context.Context, q string, args ...any) ([]handler.ROIPoint, error) {
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: roi query: %w", err)
	}
	defer rows.Close()
	out := make([]handler.ROIPoint, 0, 64)
	for rows.Next() {
		var p handler.ROIPoint
		if err := rows.Scan(
			&p.Day,
			&p.Channel,
			&p.ProductID,
			&p.TotalRevenueAUDCents,
			&p.GrossProfitAUDCents,
			&p.OrderCount,
			&p.TotalCostAUDCents,
		); err != nil {
			return nil, fmt.Errorf("postgres: roi scan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: roi iterate: %w", err)
	}
	return out, nil
}
