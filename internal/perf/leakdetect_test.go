package perf_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/perf"
)

func TestLeak_TrackAllocRecords(t *testing.T) {
	t.Parallel()
	d := perf.NewLeakDetector()
	d.TrackAlloc("heap", 1000)
	d.TrackAlloc("heap", 2000)
	report, err := d.DetectGrowth("heap", time.Minute)
	if err != nil {
		t.Fatalf("detect growth failed: %v", err)
	}
	if report.Initial != 1000 {
		t.Fatalf("expected initial 1000, got %d", report.Initial)
	}
}

func TestLeak_DetectGrowthIdentifiesLeak(t *testing.T) {
	t.Parallel()
	d := perf.NewLeakDetector()
	d.TrackAlloc("cache", 1000)
	d.TrackAlloc("cache", 5000)
	report, _ := d.DetectGrowth("cache", time.Minute)
	if !report.IsLeak {
		t.Fatal("expected leak detected for growing allocation")
	}
}

func TestLeak_DetectGrowthStableNoLeak(t *testing.T) {
	t.Parallel()
	d := perf.NewLeakDetector()
	d.TrackAlloc("stable", 1000)
	d.TrackAlloc("stable", 900)
	report, _ := d.DetectGrowth("stable", time.Minute)
	if report.IsLeak {
		t.Fatal("expected no leak for stable/declining allocation")
	}
}

func TestLeak_GoroutineLeakDetected(t *testing.T) {
	t.Parallel()
	if !perf.GoroutineLeak(10, 30) {
		t.Fatal("expected goroutine leak detected (30 > 2*10)")
	}
}

func TestLeak_GoroutineCountStableReturnsFalse(t *testing.T) {
	t.Parallel()
	if perf.GoroutineLeak(10, 12) {
		t.Fatal("expected no leak for stable goroutine count")
	}
}

func TestLeak_ReportAggregatesAll(t *testing.T) {
	t.Parallel()
	d := perf.NewLeakDetector()
	d.TrackAlloc("svc1", 100)
	d.TrackAlloc("svc1", 500)
	d.TrackAlloc("svc2", 200)
	d.TrackAlloc("svc2", 200)
	report := d.Report(nil, 5, 15)
	if !report.Goroutine.IsLeak {
		t.Fatal("expected goroutine leak in report")
	}
}
