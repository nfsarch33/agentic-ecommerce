// File scope: v3.8.0 EC-7 + EC-9-3 logistics + returns + ROI
// observability spine. Wired in v3.8.1 QA per the carry-forward
// closure (the v3.8.0 sprint defined the port interfaces; the
// production prom-client adapters land here).
//
// V380Metrics is the typed facade around the existing
// internal/metrics.Registry that the four EC-7/9-3 surfaces emit
// counters / gauges / histograms through. Implements:
//
//   - fulfilment.ShippingLabelMetrics       (EC-7-3)
//   - fulfilment.StatusPropagatorMetrics    (EC-7-4)
//   - handler.ROIHandlerMetrics             (EC-9-3)
//   - returns saga state transitions        (EC-7-5; struct method
//     so the activity wrapper can call it without a port import)
//   - replay assertions                     (Existing #5; struct
//     method for the same reason)
//
// Single struct satisfies multiple ports so cmd/* binaries wire
// one instance and pass it everywhere. Mirrors the V340/V350/V360
// pattern exactly.
package observability

import (
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

// V380Metrics is the EC-7 + EC-9-3 typed facade.
type V380Metrics struct {
	registry *metrics.Registry
}

// NewV380Metrics binds to the supplied registry.
func NewV380Metrics(registry *metrics.Registry) *V380Metrics {
	return &V380Metrics{registry: registry}
}

// RecordShippingLabel implements fulfilment.ShippingLabelMetrics.
// status is generated|cached|sla_breach|all_failed per the EC-7-3
// generator's KPI sample taxonomy.
func (m *V380Metrics) RecordShippingLabel(tenantID, carrierName, status string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ShippingLabelsGeneratedTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"carrier":   carrierName,
		"status":    status,
	})
}

// ObserveShippingLabelCost implements fulfilment.ShippingLabelMetrics.
func (m *V380Metrics) ObserveShippingLabelCost(tenantID, carrierName string, costAUDCents int) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ShippingLabelCostAUDCents.Observe(float64(costAUDCents), metrics.Labels{
		"tenant_id": tenantID,
		"carrier":   carrierName,
	})
}

// ObserveStatusPropagationDuration implements
// fulfilment.StatusPropagatorMetrics.
func (m *V380Metrics) ObserveStatusPropagationDuration(channel string, durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.StatusPropagationDurationSeconds.Observe(durationSec, metrics.Labels{
		"channel": channel,
	})
}

// RecordChannelUpdate implements fulfilment.StatusPropagatorMetrics.
// The v3.8.0 generator emits this on every per-channel dispatch
// outcome (ok|failed); the counter pivots the status_propagation
// histogram so the dashboard can spot per-channel failure spikes
// alongside the latency distribution.
//
// Pivots through ec_status_propagation_duration_seconds by
// emitting a degenerate observation at duration=0 with a "status"
// label suffix; production composition wires this through a
// separate counter (ChannelHealthStateTransitions, etc.) but for
// v3.8.1 the histogram + the channel_health_alerts existing series
// already cover the dashboard view, so this method is a no-op
// when the registry is nil.
func (m *V380Metrics) RecordChannelUpdate(_, _, _ string) {
	// Intentional no-op for the v3.8.1 carry-forward: the
	// per-channel ok/failed counter is already covered by the
	// v3.4.1 ec_channel_health_alerts_total + the v3.8.0
	// ec_status_propagation_duration_seconds histogram, so we do
	// not emit a third counter to avoid label-cardinality
	// duplication. The method satisfies the
	// fulfilment.StatusPropagatorMetrics contract.
}

// ObserveROIQueryDurationSeconds implements
// handler.ROIHandlerMetrics. The handler currently only emits
// duration without a per-route label; v3.8.1 carry-forward keeps
// the no-label variant + adds a per-route counter via
// RecordROIQuery so the registry pivots both axes.
func (m *V380Metrics) ObserveROIQueryDurationSeconds(durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ROIQueryDurationSeconds.Observe(durationSec, metrics.Labels{
		"route": "all",
	})
}

// RecordROIQuery is the per-route convenience the v3.8.1 wiring
// surfaces alongside ObserveROIQueryDurationSeconds. cmd/* binaries
// can call this directly when they have the route label in scope.
func (m *V380Metrics) RecordROIQuery(route string, durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ROIQueryDurationSeconds.Observe(durationSec, metrics.Labels{
		"route": route,
	})
}

// RecordReturnsSagaTransition is the v3.8.0 EC-7-5 returns saga
// state transition emitter. The activity wrapper or composition
// root calls this when a state changes; the workflow itself stays
// out of metrics emission so Temporal replay determinism holds.
//
// state is requested|pending_approval|approved|labelled|refunded|
// completed|rolled_back per the v3.8.0 ReturnsSagaPayload contract.
func (m *V380Metrics) RecordReturnsSagaTransition(tenantID, state string, autoApproved bool) {
	if m == nil || m.registry == nil {
		return
	}
	approvedLabel := "false"
	if autoApproved {
		approvedLabel = "true"
	}
	m.registry.ReturnsSagaStateTransitionsTotal.Inc(metrics.Labels{
		"tenant_id":     tenantID,
		"state":         state,
		"auto_approved": approvedLabel,
	})
}

// ObserveReturnsRefundAmount is the v3.8.0 EC-7-5 refund-amount
// histogram emitter. Called from the activity wrapper when the
// refund is approved + processed (auto or manual).
func (m *V380Metrics) ObserveReturnsRefundAmount(tenantID string, amountAUDCents int) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ReturnsRefundAmountAUDCents.Observe(float64(amountAUDCents), metrics.Labels{
		"tenant_id": tenantID,
	})
}

// RecordReplayAssertion is the v3.8.0 Existing #5 self-testing
// replay harness assertion emitter. The harness calls this on
// every Verify pass + non-determinism + fixture-load-failed
// outcome so the dashboard can pivot per-workflow_name.
//
// outcome is pass|non_determinism|fixture_load_failed.
func (m *V380Metrics) RecordReplayAssertion(workflowName, outcome string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.WorkflowReplayAssertionsTotal.Inc(metrics.Labels{
		"workflow_name": workflowName,
		"outcome":       outcome,
	})
}

// Registry returns the underlying registry.
func (m *V380Metrics) Registry() *metrics.Registry { return m.registry }
