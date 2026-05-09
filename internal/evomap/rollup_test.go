package evomap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadCapsulesRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "evomap.ndjson")
	sink, err := NewSink(quietLogger(), Config{Path: path, Binary: "agent-worker"})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := sink.Write(context.Background(), Capsule{KPIs: KPIs{ThroughputRPS: float64(i + 1)}}); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	caps, skipped, err := LoadCapsules(path)
	if err != nil {
		t.Fatalf("LoadCapsules: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped=%v", skipped)
	}
	if len(caps) != 3 {
		t.Fatalf("len=%d, want 3", len(caps))
	}
	if caps[2].KPIs.ThroughputRPS != 3 {
		t.Errorf("RPS=%f, want 3", caps[2].KPIs.ThroughputRPS)
	}
}

func TestLoadCapsulesSkipsMalformed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "evomap.ndjson")
	if err := os.WriteFile(path, []byte("{\"binary\":\"x\"}\nNOT JSON\n{\"binary\":\"y\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	caps, skipped, err := LoadCapsules(path)
	if err != nil {
		t.Fatalf("LoadCapsules: %v", err)
	}
	if len(caps) != 2 {
		t.Errorf("loaded=%d, want 2", len(caps))
	}
	if len(skipped) != 1 {
		t.Errorf("skipped=%d, want 1", len(skipped))
	}
}

func TestRolloverGlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{
		"evomap-2026-05-09.ndjson",
		"evomap-2026-05-10.ndjson",
		"unrelated.txt",
	} {
		_ = os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644)
	}
	matches, err := RolloverGlob(filepath.Join(dir, "evomap.ndjson"))
	if err != nil {
		t.Fatalf("RolloverGlob: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches=%v, want 2", matches)
	}
}

func TestRenderCapsuleMarkdown(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	agg := AggregateResult{
		SampleCount:        100,
		MeanThroughputRPS:  150.5,
		MaxP95Ms:           42,
		MeanErrorRate:      0.005,
		TotalOOMAlarms:     2,
		MaxGoroutineCount:  500,
		MaxHeapInUseBytes:  1 << 20,
		MeanGCPauseP99Us:   25.5,
		WindowStart:        now.Add(-24 * time.Hour),
		WindowEnd:          now,
		BinaryDistribution: map[string]int{"mc-api": 60, "agent-worker": 40},
	}
	out := RenderCapsuleMarkdown(now, agg)
	for _, want := range []string{
		"kind: evoloop_rollup",
		"host: ec-stack",
		"# EC Stack Daily Rollup -- 2026-05-09",
		"mean throughput rps: 150.50",
		"total OOM alarms: 2",
		"mc-api: 60 capsules",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
}

func TestWriteCapsuleCreatesParents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "child", "out.md")
	if err := WriteCapsule(context.Background(), path, "hello"); err != nil {
		t.Fatalf("WriteCapsule: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got=%q", got)
	}
}
