// File scope: v3.6.0 EC-8 + EC-9 customer service + analytics
// observability spine.
//
// V360Metrics is the typed facade around the v3.6.0 metric set.
// Exposes one struct that satisfies the per-package metric ports:
//
//   - customerservice.EnquiryClassifierMetrics  (EC-8-1)
//   - customerservice.FAQResponderMetrics        (EC-8-2)
//   - webhook.MessagingPipelineMetrics           (EC-8-3)
//   - handler.GMVHandlerMetrics                  (EC-9-1)
//   - handler.AgentActivitySSEMetrics            (EC-9-2)
//
// Mirrors the EC-3/4/5/6/7 V340Metrics + V350Metrics pattern so the
// composition root wires one instance and passes it to every
// package.
package observability

import (
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

// V360Metrics is the EC-8 + EC-9 typed facade.
type V360Metrics struct {
	registry *metrics.Registry
}

// NewV360Metrics binds to the supplied registry.
func NewV360Metrics(registry *metrics.Registry) *V360Metrics {
	return &V360Metrics{registry: registry}
}

// RecordClassification implements customerservice.EnquiryClassifierMetrics.
func (m *V360Metrics) RecordClassification(tenantID, intent, sentiment, language string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.EnquiryClassificationsTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"intent":    intent,
		"sentiment": sentiment,
		"language":  language,
	})
}

// RecordFAQResponse implements customerservice.FAQResponderMetrics.
func (m *V360Metrics) RecordFAQResponse(tenantID, outcome string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.FAQResponsesTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"outcome":   outcome,
	})
}

// RecordMessageWebhook implements webhook.MessagingPipelineMetrics.
func (m *V360Metrics) RecordMessageWebhook(tenantID, channel, status string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.MessageWebhookReceivedTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"channel":   channel,
		"status":    status,
	})
}

// ObserveGMVRequestDurationSeconds implements handler.GMVHandlerMetrics.
func (m *V360Metrics) ObserveGMVRequestDurationSeconds(durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.GMVRequestDurationSeconds.Observe(durationSec, nil)
}

// IncActiveConnections + DecActiveConnections implement
// handler.AgentActivitySSEMetrics. The Gauge is keyed on tenant
// so the dashboard can pivot per-tenant.
func (m *V360Metrics) IncActiveConnections(tenantID string) {
	m.adjustSSEConnections(tenantID, +1)
}

// DecActiveConnections decrements the active connections gauge.
func (m *V360Metrics) DecActiveConnections(tenantID string) {
	m.adjustSSEConnections(tenantID, -1)
}

// adjustSSEConnections is the small helper that owns the connection
// delta state. The gauge does not have a native Inc/Dec verb so the
// adapter keeps a per-tenant counter and re-Sets the gauge on
// every change.
func (m *V360Metrics) adjustSSEConnections(tenantID string, delta float64) {
	if m == nil || m.registry == nil {
		return
	}
	sseConnectionsCounter.adjust(tenantID, delta)
	m.registry.SSEActiveConnections.Set(sseConnectionsCounter.get(tenantID), metrics.Labels{
		"tenant_id": tenantID,
	})
}

// IncDispatchedEvents implements handler.AgentActivitySSEMetrics.
func (m *V360Metrics) IncDispatchedEvents(tenantID, eventType string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.SSEEventsDispatchedTotal.Inc(metrics.Labels{
		"tenant_id":  tenantID,
		"event_type": eventType,
	})
}

// Registry returns the underlying registry.
func (m *V360Metrics) Registry() *metrics.Registry { return m.registry }
