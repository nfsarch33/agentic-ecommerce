// File scope: v6.2.0 CF-16 -- production CoordinatorMetrics adapter
// that flows the existing port through to the metrics.Registry
// counter ec_coord_conflicts_total. Closes the long-standing
// composition-root carry-forward where the port had no production
// implementation.
//
// Decomposition (HARD GATE): cyclomatic stays at 1.
package coord

// MetricEmitter is the small surface the v6.2.0 adapter calls. It
// is a structural match for metrics.Counter so the cmd/* composition
// root can pass `registry.CoordConflictsTotal` directly without an
// import cycle from coord -> metrics.
type MetricEmitter interface {
	Inc(labels map[string]string)
}

// MetricsAdapter implements CoordinatorMetrics by forwarding into a
// MetricEmitter (typically *metrics.Counter for ec_coord_conflicts_total).
type MetricsAdapter struct {
	counter MetricEmitter
}

// NewMetricsAdapter wires an emitter into the CoordinatorMetrics
// port. Returns nil when emitter is nil so callers can pass through
// optional metrics without nil-checking at every call site.
func NewMetricsAdapter(emitter MetricEmitter) *MetricsAdapter {
	if emitter == nil {
		return nil
	}
	return &MetricsAdapter{counter: emitter}
}

// RecordCoordinationConflict satisfies the CoordinatorMetrics port.
// nil-safe so misconfigured composition roots stay silent rather
// than crashing the request path.
func (a *MetricsAdapter) RecordCoordinationConflict(tenantID, agentA, agentB, resolution string) {
	if a == nil || a.counter == nil {
		return
	}
	a.counter.Inc(map[string]string{
		"tenant_id":  tenantID,
		"agent_a":    agentA,
		"agent_b":    agentB,
		"resolution": resolution,
	})
}

// Static interface adherence assertion. Compilation fails fast if
// either side drifts.
var _ CoordinatorMetrics = (*MetricsAdapter)(nil)
