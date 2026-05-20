package health_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/api/health"
)

func TestDashboard_ComponentUp(t *testing.T) {
	t.Parallel()
	d := health.NewDashboard()
	d.SetComponent("db", health.StatusUp)
	if d.ComponentStatus("db") != health.StatusUp {
		t.Fatal("expected up")
	}
}

func TestDashboard_ComponentDown(t *testing.T) {
	t.Parallel()
	d := health.NewDashboard()
	d.SetComponent("redis", health.StatusDown)
	if d.ComponentStatus("redis") != health.StatusDown {
		t.Fatal("expected down")
	}
}

func TestDashboard_DependencyCheckAggregation(t *testing.T) {
	t.Parallel()
	d := health.NewDashboard()
	d.SetComponent("db", health.StatusUp)
	d.SetComponent("cache", health.StatusDegraded)
	result := d.DependencyCheck()
	if len(result) != 2 {
		t.Fatalf("expected 2 components, got %d", len(result))
	}
}

func TestDashboard_LatencyMetricRecording(t *testing.T) {
	t.Parallel()
	d := health.NewDashboard()
	d.RecordLatency("/api/orders", 50*time.Millisecond)
	d.RecordLatency("/api/orders", 100*time.Millisecond)
	metrics := d.LatencyMetrics()
	if metrics["/api/orders"] == 0 {
		t.Fatal("expected latency > 0")
	}
}

func TestDashboard_AllHealthyResponse(t *testing.T) {
	t.Parallel()
	d := health.NewDashboard()
	d.SetComponent("db", health.StatusUp)
	d.SetComponent("cache", health.StatusUp)
	for _, s := range d.DependencyCheck() {
		if s != health.StatusUp {
			t.Fatalf("expected all up, got %s", s)
		}
	}
}

func TestDashboard_PartialDegradedResponse(t *testing.T) {
	t.Parallel()
	d := health.NewDashboard()
	d.SetComponent("db", health.StatusUp)
	d.SetComponent("search", health.StatusDegraded)
	result := d.DependencyCheck()
	if result["search"] != health.StatusDegraded {
		t.Fatalf("expected degraded for search, got %s", result["search"])
	}
}
