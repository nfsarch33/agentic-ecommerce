package runtimeobs

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/evomap"
	"github.com/nfsarch33/helixon-ec/internal/memwatch"
	"github.com/nfsarch33/helixon-ec/internal/observability/agentrace"
)

type recordingTraceSink struct {
	mu     sync.Mutex
	events []agentrace.Event
}

func (r *recordingTraceSink) Emit(_ context.Context, ev agentrace.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return nil
}

func (r *recordingTraceSink) snapshot() []agentrace.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]agentrace.Event, len(r.events))
	copy(out, r.events)
	return out
}

func TestResourceGuardObservabilityFansOutToMetricsTraceAndEvomap(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "evomap.ndjson")
	trace := &recordingTraceSink{}
	rt := New(slog.Default(), "mc-api", Config{EvomapPath: path})
	guard := NewResourceGuard(ResourceGuardConfig{
		HeapWarningBytes:       700,
		HeapCriticalBytes:      900,
		GoroutineWarning:       10,
		GoroutineCritical:      20,
		SentruxDesktopWarning:  1,
		SentruxDesktopCritical: 3,
		TraceSink:              trace,
	})

	result := rt.ObserveResource(context.Background(), guard, memwatch.Sample{
		Binary:         "mc-api",
		RecordedAt:     time.Date(2026, 5, 12, 4, 30, 0, 0, time.UTC),
		HeapInUseBytes: 950,
		GoroutineCount: 12,
	}, ProcessSnapshot{SentruxDesktopProcesses: 2})
	if result.AlertCount != 3 {
		t.Fatalf("AlertCount = %d, want 3", result.AlertCount)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	rec := httptest.NewRecorder()
	rt.Registry().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`ec_resource_guard_alerts_total{binary="mc-api",severity="critical",signal="heap"} 1`,
		`ec_resource_guard_alerts_total{binary="mc-api",severity="warning",signal="goroutine"} 1`,
		`ec_resource_guard_alerts_total{binary="mc-api",severity="warning",signal="sentrux_desktop"} 1`,
		`ec_sentrux_desktop_process_count{binary="mc-api"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}

	events := trace.snapshot()
	if len(events) != 3 {
		t.Fatalf("trace events = %d, want 3", len(events))
	}
	if events[0].Type != "resource_alert" || events[0].Tool != "runtimeobs" {
		t.Fatalf("unexpected first trace event: %#v", events[0])
	}

	caps, _, err := evomap.LoadCapsules(path)
	if err != nil {
		t.Fatalf("LoadCapsules: %v", err)
	}
	if len(caps) != 1 {
		t.Fatalf("capsules=%d, want 1", len(caps))
	}
	if caps[0].KPIs.ResourceGuardAlertsTotal != 3 {
		t.Fatalf("ResourceGuardAlertsTotal=%d, want 3", caps[0].KPIs.ResourceGuardAlertsTotal)
	}
	if caps[0].KPIs.SentruxDesktopProcessCount != 2 {
		t.Fatalf("SentruxDesktopProcessCount=%d, want 2", caps[0].KPIs.SentruxDesktopProcessCount)
	}
}
