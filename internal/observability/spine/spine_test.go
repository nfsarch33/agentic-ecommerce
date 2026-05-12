package spine

import (
	"reflect"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/evomap"
)

func TestMetricInventoryDeclaresDashboardAndIngestionContracts(t *testing.T) {
	t.Parallel()

	inventory := MetricInventory()
	if err := ValidateMetricInventory(inventory); err != nil {
		t.Fatalf("inventory validation failed: %v", err)
	}

	for _, name := range []string{
		"ec_http_duration_seconds",
		"ec_oom_alarms_total",
		"ec_goroutine_count",
		"ec_heap_bytes",
		"ec_agentrace_session_duration_seconds",
		"ec_agentrace_tool_calls_total",
		"ec_workerpool_rejected_total",
		"ec_breaker_open_total",
	} {
		if _, ok := FindMetric(inventory, name); !ok {
			t.Fatalf("missing required metric %s", name)
		}
	}

	toolCalls, ok := FindMetric(inventory, "ec_agentrace_tool_calls_total")
	if !ok {
		t.Fatal("missing agentrace tool-call metric")
	}
	if toolCalls.Kind != Counter {
		t.Fatalf("agentrace tool-call kind = %s, want %s", toolCalls.Kind, Counter)
	}
	wantLabels := []LabelContract{
		{Name: "tool_name", MaxCardinality: 30},
		{Name: "outcome", MaxCardinality: 2, Values: []string{"ok", "error"}},
	}
	if !reflect.DeepEqual(toolCalls.Labels, wantLabels) {
		t.Fatalf("agentrace tool-call labels = %#v, want %#v", toolCalls.Labels, wantLabels)
	}
}

func TestDashboardSnapshotFromCapsuleUsesStableSchema(t *testing.T) {
	t.Parallel()

	recordedAt := time.Date(2026, 5, 12, 1, 2, 3, 0, time.UTC)
	eventAt := recordedAt.Add(-time.Minute)
	snapshot := SnapshotFromCapsule(evomap.Capsule{
		RecordedAt: recordedAt,
		EventAt:    eventAt,
		Binary:     "mc-api",
		KPIs: evomap.KPIs{
			ThroughputRPS:             120.5,
			P95Ms:                     8.75,
			ErrorRate:                 0.001,
			OOMAlarms:                 2,
			GoroutineCount:            321,
			HeapInUseBytes:            42_000_000,
			AgentraceAvailable:        true,
			AgentraceToolCallCount:    17,
			AgentraceCostUSD:          0.34,
			AgentraceBottleneckCount:  3,
			AgentraceParallelismRatio: 0.82,
			UIAutoRateLimitDropsTotal: 4,
			WorkerpoolRejectedTotal:   5,
			BreakerOpenTotal:          6,
			CoordConflictsTotal:       7,
		},
	})

	if err := ValidateDashboardSnapshot(snapshot); err != nil {
		t.Fatalf("snapshot validation failed: %v", err)
	}
	if snapshot.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %q, want %q", snapshot.SchemaVersion, SchemaVersion)
	}
	if snapshot.Binary != "mc-api" {
		t.Fatalf("binary = %q, want mc-api", snapshot.Binary)
	}

	gotNames := make([]string, 0, len(snapshot.Fields))
	for _, field := range snapshot.Fields {
		gotNames = append(gotNames, field.Name)
	}
	wantNames := []string{
		"throughput_rps",
		"p95_ms",
		"error_rate",
		"oom_alarms",
		"goroutine_count",
		"heap_in_use_bytes",
		"agentrace_available",
		"agentrace_tool_call_count",
		"agentrace_cost_usd",
		"agentrace_bottleneck_count",
		"agentrace_parallelism_efficiency",
		"uiauto_rate_limit_drops_total",
		"workerpool_rejected_total",
		"breaker_open_total",
		"coord_conflicts_total",
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("field order = %#v, want %#v", gotNames, wantNames)
	}
	if got := snapshot.Fields[6].Value; got != 1 {
		t.Fatalf("agentrace_available value = %v, want 1", got)
	}
}

func TestAgentraceMetricSamplesAreDeterministicAndBounded(t *testing.T) {
	t.Parallel()

	samples := AgentraceMetricSamples(evomap.AgentraceKPIs{
		Available:          true,
		SessionDurationSec: 125,
		CostUSD:            0.42,
		BottleneckCount:    2,
		ParallelismRatio:   0.76,
		ToolUsage: map[string]int{
			"Shell": 5,
			"Read":  3,
		},
		ToolErrors: map[string]int{
			"Shell": 1,
		},
	})

	got := make([]string, 0, len(samples))
	for _, sample := range samples {
		got = append(got, sample.Key())
		if err := ValidateMetricSample(sample); err != nil {
			t.Fatalf("sample %s invalid: %v", sample.Key(), err)
		}
	}
	want := []string{
		"ec_agentrace_bottlenecks_total{severity=all}",
		"ec_agentrace_cost_usd_total{}",
		"ec_agentrace_parallelism_ratio{}",
		"ec_agentrace_session_duration_seconds{}",
		"ec_agentrace_tool_calls_total{outcome=error,tool_name=Shell}",
		"ec_agentrace_tool_calls_total{outcome=ok,tool_name=Read}",
		"ec_agentrace_tool_calls_total{outcome=ok,tool_name=Shell}",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("samples = %#v, want %#v", got, want)
	}
}
