// Package observability -- v4.7.0 Story 4: per-tenant metrics
// isolation middleware + audit.
//
// TenantMetricsMiddleware validates that tenant_id is present in
// the request context before any metric emission. The
// TenantMetricsAudit test scans all registered metrics for
// tenant_id label presence.
//
// Decomposition discipline (HARD GATE: complex_fn=4):
//
//   - ValidateTenantContext  -> check context (cyclomatic 2)
//   - WrapHandler            -> middleware chain (cyclomatic 3)
package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

var (
	ErrTenantIDMissing = errors.New("observability: tenant_id missing from context")
)

type tenantIDKeyType struct{}

var tenantIDKey = tenantIDKeyType{}

// WithTenantID returns a new context carrying the tenant_id.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// TenantIDFromContext extracts the tenant_id from the context.
// Returns empty string if not present.
func TenantIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(tenantIDKey).(string); ok {
		return v
	}
	return ""
}

// ValidateTenantContext ensures a non-empty tenant_id is in ctx.
func ValidateTenantContext(ctx context.Context) error {
	if TenantIDFromContext(ctx) == "" {
		return ErrTenantIDMissing
	}
	return nil
}

// TenantMetricsMiddleware is an HTTP middleware that extracts the
// tenant_id from the X-Tenant-Id header (or query string fallback)
// and injects it into the request context. Rejects requests that
// have no tenant_id if required=true.
type TenantMetricsMiddleware struct {
	headerName string
	required   bool
}

// NewTenantMetricsMiddleware creates the middleware. headerName
// defaults to "X-Tenant-Id".
func NewTenantMetricsMiddleware(headerName string, required bool) *TenantMetricsMiddleware {
	if headerName == "" {
		headerName = "X-Tenant-Id"
	}
	return &TenantMetricsMiddleware{headerName: headerName, required: required}
}

// WrapHandler wraps an http.Handler with tenant_id context injection.
func (m *TenantMetricsMiddleware) WrapHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := m.extractTenantID(r)
		if tenantID == "" && m.required {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":"tenant_id required (set %s header)"}`, m.headerName)
			return
		}
		if tenantID != "" {
			ctx := WithTenantID(r.Context(), tenantID)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

func (m *TenantMetricsMiddleware) extractTenantID(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(m.headerName)); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get("tenant_id"))
}

// V470Metrics is the v4.7.0 MADRL + tenant dashboard typed facade.
type V470Metrics struct {
	registry *metrics.Registry
}

// NewV470Metrics binds to the supplied registry.
func NewV470Metrics(registry *metrics.Registry) *V470Metrics {
	return &V470Metrics{registry: registry}
}

// RecordCoordinationResolution records a coord resolution outcome.
func (m *V470Metrics) RecordCoordinationResolution(tenantID, resolutionType string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.CoordResolutionsTotal.Inc(metrics.Labels{
		"tenant_id":       tenantID,
		"resolution_type": resolutionType,
	})
}

// RecordRewardSignal records a reward signal emission.
func (m *V470Metrics) RecordRewardSignal(tenantID, agentID string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.CoordRewardSignalsTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"agent_id":  agentID,
	})
}

// ObserveTenantDashboardDuration records the dashboard request duration.
func (m *V470Metrics) ObserveTenantDashboardDuration(durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.TenantDashboardRequestDuration.Observe(durationSec, metrics.Labels{})
}

// TenantScopedMetrics lists all metric names that MUST carry a
// tenant_id label. Used by the audit test.
func TenantScopedMetrics() []string {
	return []string{
		"ec_http_requests_total",
		"ec_sourcing_runs_total",
		"ec_enrichment_runs_total",
		"ec_tiktok_api_calls_total",
		"ec_facebook_api_calls_total",
		"ec_channel_router_dispatches_total",
		"ec_channel_router_dlq_total",
		"ec_rednote_bridge_calls_total",
		"ec_video_script_generations_total",
		"ec_channel_health_state",
		"ec_supplier_cost_changes_total",
		"ec_pricing_decisions_total",
		"ec_order_aggregator_normalisations_total",
		"ec_dropship_orders_total",
		"ec_enquiry_classifications_total",
		"ec_faq_responses_total",
		"ec_message_webhook_received_total",
		"ec_sse_active_connections",
		"ec_sse_events_dispatched_total",
		"ec_operator_alerts_total",
		"ec_payment_charges_total",
		"ec_payment_refunds_total",
		"ec_coord_resolutions_total",
		"ec_coord_reward_signals_total",
	}
}
