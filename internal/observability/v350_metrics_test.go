package observability

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

func TestV350Metrics_NilSafe(t *testing.T) {
	t.Parallel()
	var m *V350Metrics
	m.RecordSupplierCostChange("t", "1688", "up")
	m.RecordPricingDecision("t", "approved")
	m.ObservePriceChangePct(0.05)
	m.RecordOrderNormalisation("t", "tiktok", "ok")
	m.RecordDropshipOrder("t", "1688", "placed")
	m.SetFXRateAgeSeconds(123)
	// no panic = pass
}

func TestV350Metrics_RegistersWithoutCollision(t *testing.T) {
	t.Parallel()
	registry := metrics.NewRegistry("v350-metrics-test")
	m := NewV350Metrics(registry)
	m.RecordSupplierCostChange("tenant-1", "1688", "up")
	m.RecordSupplierCostChange("tenant-1", "1688", "up")
	m.RecordPricingDecision("tenant-1", "approved")
	m.RecordPricingDecision("tenant-1", "approval_pending")
	m.ObservePriceChangePct(0.08)
	m.ObservePriceChangePct(0.20)
	m.RecordOrderNormalisation("tenant-1", "tiktok", "ok")
	m.RecordOrderNormalisation("tenant-1", "tiktok", "duplicate")
	m.RecordDropshipOrder("tenant-1", "1688", "placed")
	m.RecordDropshipOrder("tenant-1", "aliexpress", "placed")
	m.SetFXRateAgeSeconds(3600)
	if m.Registry() == nil {
		t.Fatal("Registry() returned nil")
	}
	// Render the metrics to make sure they emit text format without
	// collision. The handler is the only interface we have to the
	// underlying counters; rendering is the integration smoke.
	expected := []string{
		"ec_supplier_cost_changes_total",
		"ec_pricing_decisions_total",
		"ec_pricing_change_pct",
		"ec_order_aggregator_normalisations_total",
		"ec_dropship_orders_total",
		"ec_fx_rate_age_seconds",
	}
	rendered := metricsRender(t, registry)
	for _, name := range expected {
		if !strings.Contains(rendered, name) {
			t.Errorf("metric %q missing from rendered text", name)
		}
	}
}

func metricsRender(t *testing.T, registry *metrics.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	registry.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}
