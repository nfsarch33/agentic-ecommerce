package observability

import (
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

// V420Metrics is the v4.2.0 payment foundation typed facade.
type V420Metrics struct {
	registry *metrics.Registry
}

// NewV420Metrics binds to the supplied registry.
func NewV420Metrics(registry *metrics.Registry) *V420Metrics {
	return &V420Metrics{registry: registry}
}

// RecordCharge records a payment charge outcome.
func (m *V420Metrics) RecordCharge(tenantID, provider, status string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.PaymentChargesTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"provider":  provider,
		"status":    status,
	})
}

// ObserveChargeDuration records the duration of a charge operation.
func (m *V420Metrics) ObserveChargeDuration(provider string, seconds float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.PaymentChargeDuration.Observe(seconds, metrics.Labels{
		"provider": provider,
	})
}

// RecordRefund records a payment refund outcome.
func (m *V420Metrics) RecordRefund(tenantID, provider, status string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.PaymentRefundsTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"provider":  provider,
		"status":    status,
	})
}
