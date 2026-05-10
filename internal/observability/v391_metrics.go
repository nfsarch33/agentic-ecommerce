// File scope: v3.9.1 Existing #10 + EC-9-4 + EC-9-5 + EC-4-4
// observability spine.
//
// V391Metrics is the typed facade around the existing
// internal/metrics.Registry that the four v3.9.1 surfaces emit
// counters / gauges / histograms through:
//
//   - Existing #10 handler.OnboardingMetrics
//     (ec_onboarding_wizard_steps_completed_total +
//     ec_onboarding_wizard_completion_duration_seconds).
//   - EC-9-4 handler.ChannelContentHandlerMetrics
//     (ec_channel_content_query_duration_seconds).
//   - EC-9-5 handler.OperatorAlertMetrics
//     (ec_operator_alerts_total +
//     ec_operator_alerts_resolution_duration_seconds).
//   - EC-4-4 social.StubChannelMetrics
//     (ec_stub_channel_calls_total).
//
// Single struct satisfies all four ports so cmd/* binaries wire one
// instance and pass it everywhere. Mirrors the V340 / V350 / V360 /
// V380 / V390 facades exactly.
package observability

import (
	"github.com/nfsarch33/agentic-ecommerce/internal/api/handler"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

// V391Metrics is the v3.9.1 typed facade.
type V391Metrics struct {
	registry *metrics.Registry
}

// NewV391Metrics binds to the supplied registry.
func NewV391Metrics(registry *metrics.Registry) *V391Metrics {
	return &V391Metrics{registry: registry}
}

// RecordWizardStepCompleted implements handler.OnboardingMetrics.
func (m *V391Metrics) RecordWizardStepCompleted(tenantID string, step int) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.OnboardingWizardStepsCompleted.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"step":      stepLabel(step),
	})
}

// ObserveWizardCompletionDuration implements handler.OnboardingMetrics.
func (m *V391Metrics) ObserveWizardCompletionDuration(durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.OnboardingWizardCompletionDuration.Observe(durationSec, nil)
}

// ObserveChannelContentQueryDuration implements
// handler.ChannelContentHandlerMetrics.
func (m *V391Metrics) ObserveChannelContentQueryDuration(durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ChannelContentQueryDuration.Observe(durationSec, nil)
}

// RecordOperatorAlert implements handler.OperatorAlertMetrics.
func (m *V391Metrics) RecordOperatorAlert(tenantID string, alertType handler.AlertType, status handler.AlertStatus) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.OperatorAlertsTotal.Inc(metrics.Labels{
		"tenant_id":  tenantID,
		"alert_type": string(alertType),
		"status":     string(status),
	})
}

// ObserveOperatorAlertResolutionDuration implements
// handler.OperatorAlertMetrics.
func (m *V391Metrics) ObserveOperatorAlertResolutionDuration(durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.OperatorAlertsResolutionDuration.Observe(durationSec, nil)
}

// RecordStubChannelCall implements social.StubChannelMetrics.
func (m *V391Metrics) RecordStubChannelCall(tenantID, channel, op string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.StubChannelCallsTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"channel":   channel,
		"op":        op,
	})
}

// Registry returns the underlying registry.
func (m *V391Metrics) Registry() *metrics.Registry { return m.registry }

// stepLabel renders the wizard step number as a stable label string.
// Bounded so the cardinality budget for the steps_completed counter
// stays tight (max 4 distinct values).
func stepLabel(step int) string {
	switch step {
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	case 4:
		return "4"
	}
	return "other"
}
