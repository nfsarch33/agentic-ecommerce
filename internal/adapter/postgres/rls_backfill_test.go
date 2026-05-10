//go:build integration_pg

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestRLSBackfillTenantIsolation verifies that migration
// 0026_rls_backfill.up.sql enables RLS on every table created in
// migrations 0012-0025. For each table, tenant A inserts a row, then
// sets the GUC to tenant B and asserts zero visibility.
func TestRLSBackfillTenantIsolation(t *testing.T) {
	pool := startContainerPool(t)
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tables := []struct {
		name      string
		insertSQL string
	}{
		{
			name:      "marketplace_submissions",
			insertSQL: `INSERT INTO marketplace_submissions (id, tenant_id, submitter_email, slug, name, version, vendor, manifest, state) VALUES ('ms1', '%s', 'a@t.co', 'test-plugin', 'Test', '1.0.0', 'v', '{}', 'pending_review')`,
		},
		{
			name:      "supplier_cost_baselines",
			insertSQL: `INSERT INTO supplier_cost_baselines (tenant_id, source, supplier_sku, baseline_cny_cents, last_observed_cny) VALUES ('%s', '1688', 'SKU1', 100, 100)`,
		},
		{
			name:      "faq_entries",
			insertSQL: `INSERT INTO faq_entries (tenant_id, language, intent_category, question, answer) VALUES ('%s', 'en', 'order_status', 'Where is my order?', 'Check tracking.')`,
		},
		{
			name:      "shipping_labels",
			insertSQL: `INSERT INTO shipping_labels (tenant_id, tracking_number, order_id, carrier, label_pdf_path, cost_aud_cents, eta_days, sla_days) VALUES ('%s', 'TRK001', 'ORD1', 'auspost', '/tmp/l.pdf', 1200, 3, 5)`,
		},
		{
			name:      "returns",
			insertSQL: `INSERT INTO returns (tenant_id, rma_id, order_id, reason, refund_amount_aud_cents) VALUES ('%s', 'RMA1', 'ORD1', 'defective', 2500)`,
		},
		{
			name:      "competitor_prices",
			insertSQL: `INSERT INTO competitor_prices (tenant_id, sku, channel, competitor_id, observed_price_aud_cents) VALUES ('%s', 'SKU1', 'tiktok', 'comp1', 999)`,
		},
		{
			name:      "content_calendar_entries",
			insertSQL: `INSERT INTO content_calendar_entries (id, tenant_id, scheduled_at, channel, content_type, payload_ref) VALUES ('cce1', '%s', now(), 'tiktok', 'video', 'ref1')`,
		},
		{
			name:      "content_performance_history",
			insertSQL: `INSERT INTO content_performance_history (tenant_id, content_id, channel, content_type) VALUES ('%s', 'c1', 'tiktok', 'video')`,
		},
		{
			name:      "onboarding_wizards",
			insertSQL: `INSERT INTO onboarding_wizards (tenant_id, wizard_id) VALUES ('%s', 'wiz1')`,
		},
		{
			name:      "operator_alerts",
			insertSQL: `INSERT INTO operator_alerts (tenant_id, alert_id, alert_type) VALUES ('%s', 'alert1', 'captcha_detected')`,
		},
	}

	for _, tc := range tables {
		t.Run(tc.name, func(t *testing.T) {
			tenantA := fmt.Sprintf("rls-a-%s", tc.name[:4])
			tenantB := fmt.Sprintf("rls-b-%s", tc.name[:4])

			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			defer conn.Release()

			if _, err := conn.Exec(ctx, "SELECT set_config('app.current_tenant_id', '', false)"); err != nil {
				t.Fatalf("reset GUC: %v", err)
			}
			if _, err := conn.Exec(ctx, fmt.Sprintf(tc.insertSQL, tenantA)); err != nil {
				t.Fatalf("seed tenant A: %v", err)
			}

			if _, err := conn.Exec(ctx, "SELECT set_config('app.current_tenant_id', $1, false)", tenantB); err != nil {
				t.Fatalf("set GUC to tenant B: %v", err)
			}

			var count int
			q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE tenant_id = $1", tc.name)
			if err := conn.QueryRow(ctx, q, tenantA).Scan(&count); err != nil {
				t.Fatalf("count query: %v", err)
			}
			if count != 0 {
				t.Fatalf("tenant B session can see %d rows from tenant A in %s; RLS not enforced", count, tc.name)
			}
		})
	}
}
