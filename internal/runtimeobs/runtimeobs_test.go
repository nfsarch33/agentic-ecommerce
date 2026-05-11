package runtimeobs

import (
	"context"
	"log/slog"
	"net/http/httptest"
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
