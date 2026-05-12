package evomap_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/evomap"
	"github.com/nfsarch33/agentic-ecommerce/internal/observability/spine"
)

func TestResourceGuardKPIsAggregateAndReachSpine(t *testing.T) {
	t.Parallel()

	eventAt := time.Date(2026, 5, 12, 4, 30, 0, 0, time.UTC)
	caps := []evomap.Capsule{
		{EventAt: eventAt, Binary: "mc-api", KPIs: evomap.KPIs{ResourceGuardAlertsTotal: 2, SentruxDesktopProcessCount: 1, WorkerpoolResizeTotal: 3}},
		{EventAt: eventAt.Add(time.Minute), Binary: "mc-api", KPIs: evomap.KPIs{ResourceGuardAlertsTotal: 4, SentruxDesktopProcessCount: 3, WorkerpoolResizeTotal: 1}},
	}

	agg := evomap.Aggregate(caps)
	if agg.TotalResourceGuardAlerts != 6 {
		t.Fatalf("TotalResourceGuardAlerts=%d, want 6", agg.TotalResourceGuardAlerts)
	}
	if agg.MaxSentruxDesktopProcessCount != 3 {
		t.Fatalf("MaxSentruxDesktopProcessCount=%d, want 3", agg.MaxSentruxDesktopProcessCount)
	}
	if agg.TotalWorkerpoolResizes != 4 {
		t.Fatalf("TotalWorkerpoolResizes=%d, want 4", agg.TotalWorkerpoolResizes)
	}

	snapshot := spine.SnapshotFromCapsule(caps[1])
	for _, field := range []string{
		"resource_guard_alerts_total",
		"sentrux_desktop_process_count",
		"workerpool_resize_total",
	} {
		assertSnapshotField(t, snapshot, field)
	}
}

func assertSnapshotField(t *testing.T, snapshot spine.DashboardSnapshot, name string) {
	t.Helper()
	for _, field := range snapshot.Fields {
		if field.Name == name {
			return
		}
	}
	t.Fatalf("snapshot missing field %q: %#v", name, snapshot.Fields)
}
