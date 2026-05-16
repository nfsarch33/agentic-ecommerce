package runtimeobs

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/evomap"
	"github.com/nfsarch33/agentic-ecommerce/internal/memwatch"
)

func TestRuntimeObservabilityEmitsPrometheusAndEvomap(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "evomap.ndjson")
	rt := New(slog.Default(), "mc-api", Config{EvomapPath: path})

	sample := memwatch.Sample{
		Binary:         "mc-api",
		RecordedAt:     time.Date(2026, 5, 11, 4, 30, 0, 0, time.UTC),
		HeapInUseBytes: 123456,
		GoroutineCount: 17,
		GCPauseLastNs:  2500,
	}
	rt.Emit(context.Background(), sample)
	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	caps, _, err := evomap.LoadCapsules(path)
	if err != nil {
		t.Fatalf("LoadCapsules: %v", err)
	}
	if len(caps) != 1 {
		t.Fatalf("capsules=%d, want 1", len(caps))
	}
	if caps[0].Binary != "mc-api" || caps[0].KPIs.GoroutineCount != 17 || caps[0].KPIs.HeapInUseBytes != 123456 {
		t.Fatalf("unexpected capsule: %#v", caps[0])
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rt.Registry().Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		`ec_goroutine_count{binary="mc-api"} 17`,
		`ec_heap_bytes{binary="mc-api"} 123456`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func TestDefaultEvomapPathHonoursEnvAndGoTest(t *testing.T) {
	t.Parallel()

	if got := DefaultEvomapPath(func(key string) string {
		if key == "ECOMMERCE_EVOMAP_NDJSON" {
			return "custom.ndjson"
		}
		return ""
	}); got != "custom.ndjson" {
		t.Fatalf("env path = %q, want custom.ndjson", got)
	}
	if got := DefaultEvomapPath(func(string) string { return "" }); got != "" {
		t.Fatalf("go test default path = %q, want empty", got)
	}
	if !runningUnderGoTest() {
		t.Fatalf("runningUnderGoTest = false, want true")
	}
}

func TestRuntimeObservabilityNilAndPrometheusOnly(t *testing.T) {
	t.Parallel()

	var nilRT *RuntimeObservability
	nilRT.Emit(context.Background(), memwatch.Sample{})
	if nilRT.Registry() != nil {
		t.Fatalf("nil runtime registry should be nil")
	}
	if err := nilRT.Close(context.Background()); err != nil {
		t.Fatalf("nil Close: %v", err)
	}

	rt := New(nil, "", Config{})
	if rt.Registry() == nil {
		t.Fatalf("registry should be configured")
	}
	rt.Emit(context.Background(), memwatch.Sample{
		RecordedAt:     time.Date(2026, 5, 11, 4, 45, 0, 0, time.UTC),
		HeapInUseBytes: 456789,
		GoroutineCount: 23,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rt.Registry().Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `ec_goroutine_count{binary="unknown"} 23`) {
		t.Fatalf("metrics body missing unknown binary goroutine count:\n%s", rec.Body.String())
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("prometheus-only Close: %v", err)
	}
}

func TestRuntimeObservability_EmitsAgentraceStoryMetricsFromJSONL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	evomapPath := filepath.Join(dir, "evomap.ndjson")
	agentracePath := filepath.Join(dir, "agentrace.jsonl")
	raw := `{"type":"UserPromptSubmit","timestamp":1000,"session_id":"agentic-ecommerce__v5009r-2","agent_id":"root","payload":{"sprint_id":"v5009r","story_id":"v5009r-2","repo":"agentic-ecommerce","branch":"feat/v5009r-agentrace-story-metrics","remote_target":"wsl1-travel"}}
{"type":"PreToolUse","timestamp":2000,"session_id":"agentic-ecommerce__v5009r-2","agent_id":"root","tool_call_id":"tc-1","tool_name":"Shell"}
{"type":"PostToolUse","timestamp":5000,"session_id":"agentic-ecommerce__v5009r-2","agent_id":"root","tool_call_id":"tc-1","tool_name":"Shell"}
{"type":"Stop","timestamp":10000,"session_id":"agentic-ecommerce__v5009r-2","agent_id":"root","payload":{"sprint_id":"v5009r","story_id":"v5009r-2","repo":"agentic-ecommerce","branch":"feat/v5009r-agentrace-story-metrics","remote_target":"wsl1-travel","blocked_reason":"ssh_timeout"}}`
	if err := os.WriteFile(agentracePath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := New(slog.Default(), "mc-api", Config{
		EvomapPath:         evomapPath,
		AgentraceJSONLPath: agentracePath,
	})
	rt.Emit(context.Background(), memwatch.Sample{
		Binary:         "mc-api",
		RecordedAt:     time.Date(2026, 5, 17, 6, 0, 0, 0, time.UTC),
		HeapInUseBytes: 42,
		GoroutineCount: 7,
	})

	rec := httptest.NewRecorder()
	rt.Registry().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"ec_agentrace_story_wall_seconds",
		"ec_agentrace_story_active_seconds",
		"ec_agentrace_story_blocked_seconds",
		"ec_agentrace_story_outcomes_total",
		`story_id="v5009r-2"`,
		`remote_target="wsl1-travel"`,
		`outcome="blocked"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
}
