package spine

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/evomap"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

func TestReplayNDJSONCapsulesBuildsDashboardSnapshots(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, capsule := range []evomap.Capsule{
		qaCapsule("mc-api", 125*time.Millisecond),
		qaCapsule("agent-worker", 250*time.Millisecond),
	} {
		if err := enc.Encode(capsule); err != nil {
			t.Fatalf("encode capsule: %v", err)
		}
	}

	snapshots, err := DecodeCapsuleSnapshots(&buf)
	if err != nil {
		t.Fatalf("DecodeCapsuleSnapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshots len = %d, want 2", len(snapshots))
	}
	for _, snapshot := range snapshots {
		if err := ValidateDashboardSnapshot(snapshot); err != nil {
			t.Fatalf("snapshot invalid: %v", err)
		}
		assertSnapshotField(t, snapshot, "agentrace_session_duration_seconds")
		assertSnapshotField(t, snapshot, "workerpool_rejected_total")
		assertSnapshotField(t, snapshot, "workerpool_resize_total")
		assertSnapshotField(t, snapshot, "breaker_open_total")
		assertSnapshotField(t, snapshot, "resource_guard_alerts_total")
		assertSnapshotField(t, snapshot, "sentrux_desktop_process_count")
		assertSnapshotField(t, snapshot, "coord_conflicts_total")
	}
}

func TestMetricInventoryMatchesPrometheusTextAndDashboardFields(t *testing.T) {
	t.Parallel()

	inventory := MetricInventory()
	registry := metrics.NewRegistry("mc-api")
	recordSpineMetricSamples(registry)

	rec := httptest.NewRecorder()
	registry.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err := ValidateInventoryAgainstPrometheus(inventory, rec.Body.String()); err != nil {
		t.Fatalf("ValidateInventoryAgainstPrometheus: %v", err)
	}

	snapshot := SnapshotFromCapsule(qaCapsule("mc-api", time.Second))
	if err := ValidateDashboardFields(inventory, snapshot); err != nil {
		t.Fatalf("ValidateDashboardFields: %v", err)
	}
}

func recordSpineMetricSamples(r *metrics.Registry) {
	r.HTTPDuration.Observe(0.025, metrics.Labels{"method": "GET", "route": "/healthz"})
	r.OOMAlarms.Inc(metrics.Labels{})
	r.GoroutineCount.Set(42, metrics.Labels{})
	r.HeapBytes.Set(256*1024*1024, metrics.Labels{})
	r.AgentraceSessionDuration.Observe(12.5, metrics.Labels{})
	r.AgentraceToolCallsTotal.Inc(metrics.Labels{"tool_name": "Read", "outcome": "ok"})
	r.AgentraceCostUSDTotal.Add(0.25, metrics.Labels{"session_id": "qa-session"})
	r.AgentraceBottlenecksTotal.Inc(metrics.Labels{"severity": "all"})
	r.AgentraceParallelismRatio.Set(0.78, metrics.Labels{})
	r.WorkerpoolRejected.Inc(metrics.Labels{"pool": "agent"})
	r.WorkerpoolSize.Set(4, metrics.Labels{"pool": "agent"})
	r.WorkerpoolResizeTotal.Inc(metrics.Labels{"pool": "agent", "direction": "shrink"})
	r.BreakerOpenTotal.Inc(metrics.Labels{"name": "stripe"})
	r.ResourceGuardAlertsTotal.Inc(metrics.Labels{"signal": "heap", "severity": "critical"})
	r.SentruxDesktopProcessCount.Set(1, metrics.Labels{})
	r.CoordConflictsTotal.Inc(metrics.Labels{
		"tenant_id":  "tenant-1",
		"agent_a":    "PricingAgent",
		"agent_b":    "FulfilmentAgent",
		"resolution": "last_write_wins",
	})
	r.MarketplaceSyncEventsTotal.Inc(metrics.Labels{"provider": "shopify", "entity_type": "product", "status": "applied"})
	r.MarketplaceSyncDLQTotal.Inc(metrics.Labels{"provider": "shopify", "entity_type": "product", "reason": "transient"})
	r.MarketplaceReplayTotal.Inc(metrics.Labels{"provider": "shopify", "entity_type": "product", "status": "duplicate"})
}

func qaCapsule(binary string, sessionDuration time.Duration) evomap.Capsule {
	recordedAt := time.Date(2026, 5, 12, 1, 0, 0, 0, time.UTC)
	return evomap.Capsule{
		RecordedAt: recordedAt,
		EventAt:    recordedAt.Add(-time.Minute),
		Binary:     binary,
		KPIs: evomap.KPIs{
			ThroughputRPS:               180,
			P95Ms:                       9.2,
			ErrorRate:                   0.001,
			OOMAlarms:                   1,
			GoroutineCount:              144,
			HeapInUseBytes:              256 * 1024 * 1024,
			AgentraceAvailable:          true,
			AgentraceSessionDurationSec: sessionDuration.Seconds(),
			AgentraceToolCallCount:      17,
			AgentraceCostUSD:            0.42,
			AgentraceBottleneckCount:    2,
			AgentraceParallelismRatio:   0.78,
			UIAutoRateLimitDropsTotal:   3,
			WorkerpoolRejectedTotal:     4,
			WorkerpoolSize:              5,
			WorkerpoolResizeTotal:       1,
			BreakerOpenTotal:            5,
			CoordConflictsTotal:         6,
			ResourceGuardAlertsTotal:    2,
			SentruxDesktopProcessCount:  1,
			MarketplaceSyncEventsTotal:  7,
			MarketplaceSyncDLQTotal:     8,
			MarketplaceReplayTotal:      9,
		},
	}
}

func assertSnapshotField(t *testing.T, snapshot DashboardSnapshot, name string) {
	t.Helper()
	for _, field := range snapshot.Fields {
		if field.Name == name {
			return
		}
	}
	t.Fatalf("snapshot missing field %q: %#v", name, snapshot.Fields)
}
