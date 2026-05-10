package evomap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type mockHTTPDoer struct {
	resp *http.Response
	err  error
}

func (m *mockHTTPDoer) Do(_ *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

func sampleInsights() []AgentraceInsight {
	t0 := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	return []AgentraceInsight{
		{Type: "session_start", SessionID: "s1", Timestamp: t0},
		{Type: "tool_call", Tool: "Shell", Outcome: "ok"},
		{Type: "tool_call", Tool: "Read", Outcome: "ok"},
		{Type: "tool_call", Tool: "Shell", Outcome: "error"},
		{Type: "cost", CostUSD: 0.42},
		{Type: "bottleneck", Severity: "high"},
		{Type: "bottleneck", Severity: "low"},
		{Type: "parallelism", Ratio: 0.78},
		{Type: "session_end", SessionID: "s1", Timestamp: t0.Add(300 * time.Second)},
	}
}

func TestAgentraceAdapter_InsightsAvailable_KPIPopulated(t *testing.T) {
	t.Parallel()
	body, _ := json.Marshal(sampleInsights())
	doer := &mockHTTPDoer{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
		},
	}
	adapter := NewAgentraceAdapter(AgentraceAdapterConfig{
		HTTPClient: doer,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	kpis := adapter.Read(context.Background())

	if !kpis.Available {
		t.Fatal("expected Available=true")
	}
	if kpis.SessionDurationSec != 300 {
		t.Errorf("SessionDurationSec = %f, want 300", kpis.SessionDurationSec)
	}
	if kpis.ToolCallCount != 3 {
		t.Errorf("ToolCallCount = %d, want 3", kpis.ToolCallCount)
	}
	if kpis.CostUSD != 0.42 {
		t.Errorf("CostUSD = %f, want 0.42", kpis.CostUSD)
	}
	if kpis.BottleneckCount != 2 {
		t.Errorf("BottleneckCount = %d, want 2", kpis.BottleneckCount)
	}
	if kpis.ParallelismRatio != 0.78 {
		t.Errorf("ParallelismRatio = %f, want 0.78", kpis.ParallelismRatio)
	}
	if kpis.ToolUsage["Shell"] != 2 {
		t.Errorf("ToolUsage[Shell] = %d, want 2", kpis.ToolUsage["Shell"])
	}
	if kpis.ToolErrors["Shell"] != 1 {
		t.Errorf("ToolErrors[Shell] = %d, want 1", kpis.ToolErrors["Shell"])
	}
}

func TestAgentraceAdapter_ConnectionRefused_GracefulSkip(t *testing.T) {
	t.Parallel()
	doer := &mockHTTPDoer{err: fmt.Errorf("connection refused")}
	adapter := NewAgentraceAdapter(AgentraceAdapterConfig{
		HTTPClient: doer,
		JSONLPath:  "/nonexistent/path/events.jsonl",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	kpis := adapter.Read(context.Background())

	if kpis.Available {
		t.Fatal("expected Available=false when both sources unavailable")
	}
	if kpis.ToolCallCount != 0 {
		t.Errorf("ToolCallCount = %d, want 0", kpis.ToolCallCount)
	}
}

func TestAgentraceAdapter_JSONLFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")
	insights := sampleInsights()
	var buf bytes.Buffer
	for _, ins := range insights {
		line, _ := json.Marshal(ins)
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(jsonlPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	doer := &mockHTTPDoer{err: fmt.Errorf("connection refused")}
	adapter := NewAgentraceAdapter(AgentraceAdapterConfig{
		HTTPClient: doer,
		JSONLPath:  jsonlPath,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	kpis := adapter.Read(context.Background())

	if !kpis.Available {
		t.Fatal("expected Available=true from JSONL fallback")
	}
	if kpis.ToolCallCount != 3 {
		t.Errorf("ToolCallCount = %d, want 3", kpis.ToolCallCount)
	}
	if kpis.SessionDurationSec != 300 {
		t.Errorf("SessionDurationSec = %f, want 300", kpis.SessionDurationSec)
	}
}

func TestAgentraceAdapter_FieldMappingCorrectness(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	insights := []AgentraceInsight{
		{Type: "session_start", SessionID: "a", Timestamp: t0},
		{Type: "session_start", SessionID: "b", Timestamp: t0.Add(10 * time.Second)},
		{Type: "tool_call", Tool: "Grep", Outcome: "ok"},
		{Type: "cost", CostUSD: 1.5},
		{Type: "cost", CostUSD: 0.5},
		{Type: "session_end", SessionID: "a", Timestamp: t0.Add(60 * time.Second)},
		{Type: "session_end", SessionID: "b", Timestamp: t0.Add(120 * time.Second)},
	}
	body, _ := json.Marshal(insights)
	doer := &mockHTTPDoer{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
		},
	}
	adapter := NewAgentraceAdapter(AgentraceAdapterConfig{
		HTTPClient: doer,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	kpis := adapter.Read(context.Background())

	wantDuration := 60.0 + 110.0
	if kpis.SessionDurationSec != wantDuration {
		t.Errorf("SessionDurationSec = %f, want %f", kpis.SessionDurationSec, wantDuration)
	}
	if kpis.CostUSD != 2.0 {
		t.Errorf("CostUSD = %f, want 2.0", kpis.CostUSD)
	}
	if kpis.ToolUsage["Grep"] != 1 {
		t.Errorf("ToolUsage[Grep] = %d, want 1", kpis.ToolUsage["Grep"])
	}
}

func TestAgentraceAdapter_IntegrationWithExistingSink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sink, err := NewSink(logger, Config{
		Path:   filepath.Join(dir, "evomap.ndjson"),
		Binary: "test-binary",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close(context.Background())

	body, _ := json.Marshal(sampleInsights())
	doer := &mockHTTPDoer{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
		},
	}
	adapter := NewAgentraceAdapter(AgentraceAdapterConfig{
		HTTPClient: doer,
		Logger:     logger,
	})
	aKPIs := adapter.Read(context.Background())
	if !aKPIs.Available {
		t.Fatal("expected agentrace available")
	}

	capsule := Capsule{
		Binary: "test-binary",
		KPIs: KPIs{
			ThroughputRPS:               100,
			AgentraceAvailable:          aKPIs.Available,
			AgentraceSessionDurationSec: aKPIs.SessionDurationSec,
			AgentraceToolCallCount:      aKPIs.ToolCallCount,
			AgentraceCostUSD:            aKPIs.CostUSD,
			AgentraceBottleneckCount:    aKPIs.BottleneckCount,
			AgentraceParallelismRatio:   aKPIs.ParallelismRatio,
		},
	}
	if err := sink.Write(context.Background(), capsule); err != nil {
		t.Fatalf("sink.Write: %v", err)
	}

	caps, _, err := LoadCapsules(filepath.Join(dir, "evomap.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 1 {
		t.Fatalf("expected 1 capsule, got %d", len(caps))
	}
	if !caps[0].KPIs.AgentraceAvailable {
		t.Error("expected AgentraceAvailable=true in loaded capsule")
	}
	if caps[0].KPIs.AgentraceToolCallCount != 3 {
		t.Errorf("loaded AgentraceToolCallCount = %d, want 3", caps[0].KPIs.AgentraceToolCallCount)
	}
}
