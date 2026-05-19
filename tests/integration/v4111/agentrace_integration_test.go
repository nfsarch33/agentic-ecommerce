//go:build v4111_smoke

package v4111

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/evomap"
	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

func TestAgentraceE2E_MockHTTP_AdapterReads_KPIEmitted_CapsuleIncludesSummary(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	insights := []evomap.AgentraceInsight{
		{Type: "session_start", SessionID: "e2e-1", Timestamp: t0},
		{Type: "tool_call", Tool: "Shell", Outcome: "ok"},
		{Type: "tool_call", Tool: "Read", Outcome: "ok"},
		{Type: "tool_call", Tool: "Shell", Outcome: "error"},
		{Type: "cost", CostUSD: 0.55},
		{Type: "bottleneck", Severity: "high"},
		{Type: "parallelism", Ratio: 0.82},
		{Type: "session_end", SessionID: "e2e-1", Timestamp: t0.Add(180 * time.Second)},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(insights)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter := evomap.NewAgentraceAdapter(evomap.AgentraceAdapterConfig{
		HTTPURL:    srv.URL,
		HTTPClient: srv.Client(),
		Logger:     logger,
	})

	aKPIs := adapter.Read(context.Background())
	if !aKPIs.Available {
		t.Fatal("expected agentrace available from mock server")
	}
	if aKPIs.ToolCallCount != 3 {
		t.Errorf("ToolCallCount = %d, want 3", aKPIs.ToolCallCount)
	}
	if aKPIs.CostUSD != 0.55 {
		t.Errorf("CostUSD = %f, want 0.55", aKPIs.CostUSD)
	}

	dir := t.TempDir()
	sink, err := evomap.NewSink(logger, evomap.Config{
		Path:   filepath.Join(dir, "evomap.ndjson"),
		Binary: "e2e-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	capsule := evomap.Capsule{
		Binary: "e2e-test",
		KPIs: evomap.KPIs{
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
	sink.Close(context.Background())

	caps, _, err := evomap.LoadCapsules(filepath.Join(dir, "evomap.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 1 {
		t.Fatalf("expected 1 capsule, got %d", len(caps))
	}
	if !caps[0].KPIs.AgentraceAvailable {
		t.Error("AgentraceAvailable should be true in loaded capsule")
	}

	md := evomap.RenderAgentraceCapsuleExtension(aKPIs)
	if !strings.Contains(md, "## Agentrace Summary") {
		t.Error("capsule markdown missing Agentrace Summary section")
	}
	if !strings.Contains(md, "## Agentrace Tool Usage") {
		t.Error("capsule markdown missing Agentrace Tool Usage section")
	}

	reg := metrics.NewRegistry("e2e-test")
	reg.AgentraceSessionDuration.Observe(aKPIs.SessionDurationSec, metrics.Labels{})
	reg.AgentraceParallelismRatio.Set(aKPIs.ParallelismRatio, metrics.Labels{})
	for tool, count := range aKPIs.ToolUsage {
		reg.AgentraceToolCallsTotal.Add(float64(count), metrics.Labels{
			"tool_name": tool, "outcome": "ok",
		})
	}
}

func TestAgentraceE2E_GracefulDegradation_NoAgentrace(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	adapter := evomap.NewAgentraceAdapter(evomap.AgentraceAdapterConfig{
		HTTPURL:    "http://127.0.0.1:1/nonexistent",
		JSONLPath:  "/nonexistent/path/events.jsonl",
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
		Logger:     logger,
	})

	aKPIs := adapter.Read(context.Background())
	if aKPIs.Available {
		t.Fatal("expected Available=false when agentrace unreachable")
	}
	if aKPIs.ToolCallCount != 0 {
		t.Errorf("ToolCallCount = %d, want 0", aKPIs.ToolCallCount)
	}

	dir := t.TempDir()
	sink, err := evomap.NewSink(logger, evomap.Config{
		Path:   filepath.Join(dir, "evomap.ndjson"),
		Binary: "e2e-graceful",
	})
	if err != nil {
		t.Fatal(err)
	}
	capsule := evomap.Capsule{
		Binary: "e2e-graceful",
		KPIs: evomap.KPIs{
			ThroughputRPS:      50,
			AgentraceAvailable: false,
		},
	}
	if err := sink.Write(context.Background(), capsule); err != nil {
		t.Fatalf("sink.Write: %v", err)
	}
	sink.Close(context.Background())

	caps, _, err := evomap.LoadCapsules(filepath.Join(dir, "evomap.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 1 {
		t.Fatalf("expected 1 capsule, got %d", len(caps))
	}
	if caps[0].KPIs.AgentraceAvailable {
		t.Error("AgentraceAvailable should be false in degraded capsule")
	}

	md := evomap.RenderAgentraceCapsuleExtension(aKPIs)
	if md != "" {
		t.Errorf("expected empty capsule extension for unavailable agentrace, got:\n%s", md)
	}
}

func TestGrafanaDashboard_JSONStructureValid(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../monitoring/grafana/agentrace-insights.json")
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	var dashboard map[string]any
	if err := json.Unmarshal(data, &dashboard); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	uid, ok := dashboard["uid"].(string)
	if !ok || uid != "agentrace-insights-v4110" {
		t.Errorf("uid = %v, want agentrace-insights-v4110", dashboard["uid"])
	}

	panels, ok := dashboard["panels"].([]any)
	if !ok {
		t.Fatal("panels field missing or not array")
	}
	if len(panels) != 6 {
		t.Errorf("expected 6 panels, got %d", len(panels))
	}

	expectedTitles := []string{
		"Session Timeline (Duration Distribution)",
		"Tool Call Heatmap (by Tool)",
		"Cost per Session (USD)",
		"Bottleneck Timeline (by Severity)",
		"Parallelism Efficiency (%)",
		"Error Rate per Tool",
	}
	for i, panel := range panels {
		p, ok := panel.(map[string]any)
		if !ok {
			t.Errorf("panel %d is not object", i)
			continue
		}
		title, _ := p["title"].(string)
		if title != expectedTitles[i] {
			t.Errorf("panel %d title = %q, want %q", i, title, expectedTitles[i])
		}
		if _, hasDatasource := p["datasource"]; !hasDatasource {
			t.Errorf("panel %d missing datasource", i)
		}
		if _, hasTargets := p["targets"]; !hasTargets {
			t.Errorf("panel %d missing targets", i)
		}
	}

	sv, ok := dashboard["schemaVersion"].(float64)
	if !ok || sv < 30 {
		t.Errorf("schemaVersion = %v, want >= 30", dashboard["schemaVersion"])
	}
}

// suppress unused import warnings
var _ = fmt.Sprint
var _ = bytes.NewReader
