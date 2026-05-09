package evomap

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestSinkWritesNDJSONLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "evomap.ndjson")
	sink, err := NewSink(quietLogger(), Config{Path: path, Binary: "mc-api"})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}
	defer sink.Close(context.Background())

	cap := Capsule{
		EventAt:  time.Now(),
		TenantID: "tenant-x",
		KPIs: KPIs{
			ThroughputRPS:  100,
			P95Ms:          42,
			ErrorRate:      0.001,
			OOMAlarms:      0,
			GoroutineCount: 120,
			GCPauseP99Us:   15,
			HeapInUseBytes: 1024 * 1024,
		},
	}
	if err := sink.Write(context.Background(), cap); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"binary":"mc-api"`) {
		t.Errorf("missing binary field: %s", data)
	}
	if !strings.Contains(string(data), `"throughput_rps":100`) {
		t.Errorf("missing throughput_rps: %s", data)
	}

	var parsed Capsule
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Scan()
	if err := json.Unmarshal(scanner.Bytes(), &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if parsed.Binary != "mc-api" {
		t.Errorf("Binary=%q", parsed.Binary)
	}
}

func TestSinkRotatesByDay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "evomap.ndjson")
	now := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	sink, err := NewSink(quietLogger(), Config{Path: path, Binary: "mc-api", Now: clock, Rotate: true})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}
	defer sink.Close(context.Background())

	if err := sink.Write(context.Background(), Capsule{EventAt: now, KPIs: KPIs{ThroughputRPS: 1}}); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	now = now.Add(25 * time.Hour)
	if err := sink.Write(context.Background(), Capsule{EventAt: now, KPIs: KPIs{ThroughputRPS: 2}}); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "evomap*.ndjson"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("expected >=2 files (rotation), got %v", files)
	}
}

func TestRolledFileAppendOnReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "evomap.ndjson")

	sink1, err := NewSink(quietLogger(), Config{Path: path, Binary: "x"})
	if err != nil {
		t.Fatalf("NewSink1: %v", err)
	}
	if err := sink1.Write(context.Background(), Capsule{KPIs: KPIs{ThroughputRPS: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := sink1.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	sink2, err := NewSink(quietLogger(), Config{Path: path, Binary: "x"})
	if err != nil {
		t.Fatalf("NewSink2: %v", err)
	}
	if err := sink2.Write(context.Background(), Capsule{KPIs: KPIs{ThroughputRPS: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := sink2.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d:\n%s", len(lines), data)
	}
}

func TestAggregateKPIs(t *testing.T) {
	t.Parallel()
	caps := []Capsule{
		{KPIs: KPIs{ThroughputRPS: 100, P95Ms: 50, ErrorRate: 0.01, GoroutineCount: 200, OOMAlarms: 0, GCPauseP99Us: 10, HeapInUseBytes: 1000}},
		{KPIs: KPIs{ThroughputRPS: 200, P95Ms: 60, ErrorRate: 0.02, GoroutineCount: 250, OOMAlarms: 1, GCPauseP99Us: 20, HeapInUseBytes: 2000}},
	}
	got := Aggregate(caps)
	if got.SampleCount != 2 {
		t.Errorf("SampleCount=%d, want 2", got.SampleCount)
	}
	if got.MeanThroughputRPS != 150 {
		t.Errorf("MeanThroughputRPS=%f, want 150", got.MeanThroughputRPS)
	}
	if got.MaxP95Ms != 60 {
		t.Errorf("MaxP95Ms=%f, want 60", got.MaxP95Ms)
	}
	if got.TotalOOMAlarms != 1 {
		t.Errorf("TotalOOMAlarms=%d, want 1", got.TotalOOMAlarms)
	}
}

// TestAggregateUIAutoHardeningKPIs is the v3.7.0 EC-10 evomap-side
// gate. The aggregator MUST roll the new uiauto hardening KPIs
// into the daily roll-up so the EvoLoop pipeline + Grafana panel
// can read them.
func TestAggregateUIAutoHardeningKPIs(t *testing.T) {
	t.Parallel()
	caps := []Capsule{
		{KPIs: KPIs{
			UIAutoSessionOpsTotal:       3,
			OmniParserInferenceP95Ms:    140,
			OmniParserMemoryPausesTotal: 2,
			UIAutoRateLimitDropsTotal:   1,
			CAPTCHADetectionsTotal:      4,
			CAPTCHAAvgResolutionSeconds: 90,
		}},
		{KPIs: KPIs{
			UIAutoSessionOpsTotal:       2,
			OmniParserInferenceP95Ms:    220,
			OmniParserMemoryPausesTotal: 1,
			UIAutoRateLimitDropsTotal:   3,
			CAPTCHADetectionsTotal:      2,
			CAPTCHAAvgResolutionSeconds: 110,
		}},
	}
	got := Aggregate(caps)
	if got.TotalUIAutoSessionOps != 5 {
		t.Errorf("TotalUIAutoSessionOps=%d, want 5", got.TotalUIAutoSessionOps)
	}
	if got.MaxOmniParserInferenceP95Ms != 220 {
		t.Errorf("MaxOmniParserInferenceP95Ms=%f, want 220", got.MaxOmniParserInferenceP95Ms)
	}
	if got.TotalOmniParserMemoryPauses != 3 {
		t.Errorf("TotalOmniParserMemoryPauses=%d, want 3", got.TotalOmniParserMemoryPauses)
	}
	if got.TotalUIAutoRateLimitDrops != 4 {
		t.Errorf("TotalUIAutoRateLimitDrops=%d, want 4", got.TotalUIAutoRateLimitDrops)
	}
	if got.TotalCAPTCHADetections != 6 {
		t.Errorf("TotalCAPTCHADetections=%d, want 6", got.TotalCAPTCHADetections)
	}
	if got.MeanCAPTCHAResolutionSeconds != 100 {
		t.Errorf("MeanCAPTCHAResolutionSeconds=%f, want 100", got.MeanCAPTCHAResolutionSeconds)
	}
}
