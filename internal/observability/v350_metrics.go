// File scope: v3.5.0 EC-6 + EC-7 pricing + fulfilment observability
// spine.
//
// V350Metrics is the typed facade around the existing
// internal/metrics.Registry that the four EC-6/7 surfaces emit
// counters / gauges / histograms through:
//
//   - EC-6-1 monitor.SupplierCostMonitor emits ec_supplier_cost_changes_total.
//   - EC-6-2 billing.PlatformFeeCalculator surfaces ec_fx_rate_age_seconds
//     via the operator-driven sampler driver (the calculator is
//     pure; the cmd/* binary samples MaxFXAge - now-rate.FetchedAt).
//   - EC-6-3 pricing.PricingAgent emits ec_pricing_decisions_total
//   - ec_pricing_change_pct.
//   - EC-7-1 workflow.OrderAggregatorWorkflow emits
//     ec_order_aggregator_normalisations_total via the activity wrap.
//   - EC-7-2 fulfilment.DropshipAgent emits ec_dropship_orders_total.
//
// Single struct satisfies SupplierCostMetrics, PricingAgentMetrics,
// + DropshipAgentMetrics so cmd/* binaries wire one instance and
// pass it everywhere. Mirrors the EC-3 TikTokMetrics pattern.
package observability

import (
	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

// V350Metrics is the EC-6/7 typed facade.
type V350Metrics struct {
	registry *metrics.Registry
}

// NewV350Metrics binds to the supplied registry.
func NewV350Metrics(registry *metrics.Registry) *V350Metrics {
	return &V350Metrics{registry: registry}
}

// RecordSupplierCostChange implements monitor.SupplierCostMetrics.
func (m *V350Metrics) RecordSupplierCostChange(tenantID, source, direction string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.SupplierCostChangesTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"source":    source,
		"direction": direction,
	})
}

// RecordPricingDecision implements pricing.PricingAgentMetrics.
func (m *V350Metrics) RecordPricingDecision(tenantID, outcome string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.PricingDecisionsTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"outcome":   outcome,
	})
}

// ObservePriceChangePct implements pricing.PricingAgentMetrics.
func (m *V350Metrics) ObservePriceChangePct(deltaPct float64) {
	if m == nil || m.registry == nil {
		return
	}
	if deltaPct < 0 {
		deltaPct = -deltaPct
	}
	m.registry.PricingChangePctHistogram.Observe(deltaPct, nil)
}

// RecordOrderNormalisation is the activity-side hook used by
// workflow.OrderAggregatorActivities.PublishOrderNormalised.
// Tenant + channel from the normalised payload; status =
// ok|duplicate|failure.
func (m *V350Metrics) RecordOrderNormalisation(tenantID, channel, status string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.OrderAggregatorNormalisationsTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"channel":   channel,
		"status":    status,
	})
}

// RecordDropshipOrder implements fulfilment.DropshipAgentMetrics.
func (m *V350Metrics) RecordDropshipOrder(tenantID, supplier, status string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.DropshipOrdersTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"supplier":  supplier,
		"status":    status,
	})
}

// SetFXRateAgeSeconds publishes the ec_fx_rate_age_seconds gauge
// using the latest calculator sample. Callers compute the age from
// PlatformFeeCalculator + the latest provider rate.
func (m *V350Metrics) SetFXRateAgeSeconds(ageSeconds float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.FXRateAgeSeconds.Set(ageSeconds, nil)
}

// Registry returns the underlying registry.
func (m *V350Metrics) Registry() *metrics.Registry { return m.registry }
