package perf_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/perf"
)

func TestProfiler_CPUProfileCapturesData(t *testing.T) {
	t.Parallel()
	p, err := perf.CPUProfile(nil, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("cpu profile failed: %v", err)
	}
	if p.Type != "cpu" {
		t.Fatalf("expected type cpu, got %s", p.Type)
	}
	if len(p.Samples) == 0 {
		t.Fatal("expected non-empty samples")
	}
}

func TestProfiler_CPUProfileZeroDurationError(t *testing.T) {
	t.Parallel()
	_, err := perf.CPUProfile(nil, 0)
	if err != perf.ErrZeroDuration {
		t.Fatalf("expected ErrZeroDuration, got %v", err)
	}
}

func TestProfiler_MemProfileCapturesHeap(t *testing.T) {
	t.Parallel()
	p, err := perf.MemProfile(nil)
	if err != nil {
		t.Fatalf("mem profile failed: %v", err)
	}
	if p.Type != "mem" {
		t.Fatalf("expected type mem, got %s", p.Type)
	}
	if len(p.Samples) < 2 {
		t.Fatal("expected at least 2 heap samples")
	}
}

func TestProfiler_GoroutineTraceListsActive(t *testing.T) {
	t.Parallel()
	infos, err := perf.GoroutineTrace(nil)
	if err != nil {
		t.Fatalf("goroutine trace failed: %v", err)
	}
	if len(infos) == 0 {
		t.Fatal("expected at least 1 goroutine")
	}
}

func TestProfiler_HotspotReportSortsByUsage(t *testing.T) {
	t.Parallel()
	p := perf.Profile{Type: "cpu", Samples: []int64{100, 500, 200, 50}}
	spots := perf.HotspotReport(p, 2)
	if len(spots) != 2 {
		t.Fatalf("expected 2 hotspots, got %d", len(spots))
	}
	if spots[0].Value < spots[1].Value {
		t.Fatal("expected hotspots sorted descending")
	}
}

func TestProfiler_HotspotReportTopNTruncates(t *testing.T) {
	t.Parallel()
	p := perf.Profile{Type: "mem", Samples: []int64{1, 2, 3, 4, 5}}
	spots := perf.HotspotReport(p, 3)
	if len(spots) != 3 {
		t.Fatalf("expected 3 spots, got %d", len(spots))
	}
}
