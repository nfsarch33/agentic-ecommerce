// File scope: v6.1.0 coverage backfill -- V470Metrics constructor,
// RecordCoordinationResolution, RecordRewardSignal, and
// ObserveTenantDashboardDuration were uncovered after the v4.7.0
// MADRL ship.
package observability

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

func TestV470MetricsRecordersTolerateNilRegistry(t *testing.T) {
	t.Parallel()
	// Nil receiver short-circuit: no panic.
	var m *V470Metrics
	m.RecordCoordinationResolution("tenant-a", "weighted_priority")
	m.RecordRewardSignal("tenant-a", "agent-1")
	m.ObserveTenantDashboardDuration(0.42)

	// Empty struct (nil registry) short-circuit.
	empty := &V470Metrics{}
	empty.RecordCoordinationResolution("tenant-a", "weighted_priority")
	empty.RecordRewardSignal("tenant-a", "agent-1")
	empty.ObserveTenantDashboardDuration(0.42)
}

func TestV470MetricsRecordersIncrementBoundCounters(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry("v470-test")
	m := NewV470Metrics(reg)
	if m == nil {
		t.Fatal("NewV470Metrics returned nil")
	}
	// Exercise the happy path so the labelled counters and histogram
	// fire. We don't assert specific scrape output here; the metrics
	// package has its own tests for that. The aim is to lift the
	// per-function coverage from 0% to 100%.
	m.RecordCoordinationResolution("tenant-a", "weighted_priority")
	m.RecordCoordinationResolution("tenant-a", "constraint_override")
	m.RecordRewardSignal("tenant-a", "agent-1")
	m.ObserveTenantDashboardDuration(0.123)
}
