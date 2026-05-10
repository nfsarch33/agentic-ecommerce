package evomap

import (
	"strings"
	"testing"
)

func TestRenderAgentraceCapsule_WithData(t *testing.T) {
	t.Parallel()
	kpis := AgentraceKPIs{
		Available:          true,
		SessionDurationSec: 300,
		ToolCallCount:      10,
		CostUSD:            0.42,
		BottleneckCount:    2,
		ParallelismRatio:   0.78,
		ToolUsage: map[string]int{
			"Shell": 5,
			"Read":  3,
			"Grep":  2,
		},
		ToolErrors: map[string]int{
			"Shell": 1,
		},
	}
	output := RenderAgentraceCapsuleExtension(kpis)

	checks := []string{
		"## Agentrace Summary",
		"total cost: $0.4200 USD",
		"avg parallelism efficiency: 0.78",
		"bottlenecks detected: 2",
		"total tool calls: 10",
		"## Agentrace Tool Usage",
		"| Shell | 5 | 1 | 20.0% |",
		"| Read | 3 | 0 | 0.0% |",
		"| Grep | 2 | 0 | 0.0% |",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("expected %q in output, got:\n%s", check, output)
		}
	}
}

func TestRenderAgentraceCapsule_WithoutData_Graceful(t *testing.T) {
	t.Parallel()
	kpis := AgentraceKPIs{Available: false}
	output := RenderAgentraceCapsuleExtension(kpis)
	if output != "" {
		t.Errorf("expected empty output for unavailable Agentrace, got:\n%s", output)
	}
}

func TestRenderAgentraceToolUsage_Aggregation(t *testing.T) {
	t.Parallel()
	kpis := AgentraceKPIs{
		Available: true,
		ToolUsage: map[string]int{
			"Shell": 100,
			"Read":  50,
			"Write": 25,
		},
		ToolErrors: map[string]int{
			"Shell": 10,
			"Write": 5,
		},
	}
	output := RenderAgentraceToolUsage(kpis)

	if !strings.Contains(output, "| Shell | 100 | 10 | 10.0% |") {
		t.Error("Shell error rate wrong")
	}
	if !strings.Contains(output, "| Read | 50 | 0 | 0.0% |") {
		t.Error("Read error rate wrong")
	}
	if !strings.Contains(output, "| Write | 25 | 5 | 20.0% |") {
		t.Error("Write error rate wrong")
	}
}

func TestRenderAgentraceSummary_Statistics(t *testing.T) {
	t.Parallel()
	kpis := AgentraceKPIs{
		Available:          true,
		SessionDurationSec: 600,
		ToolCallCount:      42,
		CostUSD:            1.234,
		BottleneckCount:    5,
		ParallelismRatio:   0.92,
	}
	output := RenderAgentraceSummary(kpis)

	if !strings.Contains(output, "total session duration: 600.0s") {
		t.Error("session duration not rendered")
	}
	if !strings.Contains(output, "total cost: $1.2340 USD") {
		t.Error("cost not rendered")
	}
	if !strings.Contains(output, "bottlenecks detected: 5") {
		t.Error("bottleneck count not rendered")
	}
}
