// File scope: v5.5.0 Postgres connection pool observability spine.
//
// V550Metrics is the typed facade around the v5.5.0 PG pool metrics
// registered in internal/metrics/pgpool_metrics.go:
//
//   - ec_pg_pool_open_connections (gauge)
//   - ec_pg_pool_idle_connections (gauge)
//   - ec_pg_pool_wait_total (counter)
//   - ec_pg_pool_wait_duration_seconds (histogram)
//
// Mirrors the V350 / V380 / V391 facade pattern exactly.
package observability

import (
	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

// PGPoolMetrics is the port the pool sampler records through.
type PGPoolMetrics interface {
	SetOpenConnections(n int)
	SetIdleConnections(n int)
	RecordPoolWait()
	ObservePoolWaitDuration(durationSec float64)
}

// V550Metrics is the v5.5.0 typed facade.
type V550Metrics struct {
	registry *metrics.Registry
}

// NewV550Metrics binds to the supplied registry.
func NewV550Metrics(registry *metrics.Registry) *V550Metrics {
	return &V550Metrics{registry: registry}
}

// SetOpenConnections records the current open connection count.
func (m *V550Metrics) SetOpenConnections(n int) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.PGPoolOpenConnections.Set(float64(n), nil)
}

// SetIdleConnections records the current idle connection count.
func (m *V550Metrics) SetIdleConnections(n int) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.PGPoolIdleConnections.Set(float64(n), nil)
}

// RecordPoolWait increments the wait counter.
func (m *V550Metrics) RecordPoolWait() {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.PGPoolWaitTotal.Inc(nil)
}

// ObservePoolWaitDuration records a connection-acquire wait duration.
func (m *V550Metrics) ObservePoolWaitDuration(durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.PGPoolWaitDuration.Observe(durationSec, nil)
}
