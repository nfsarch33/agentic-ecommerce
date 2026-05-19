package metrics

import (
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/evomap"
)

func TestAgentraceMetrics_Registered(t *testing.T) {
	t.Parallel()
	r := NewRegistry("test")

	if r.AgentraceSessionDuration == nil {
		t.Error("AgentraceSessionDuration not registered")
	}
	if r.AgentraceToolCallsTotal == nil {
		t.Error("AgentraceToolCallsTotal not registered")
	}
	if r.AgentraceCostUSDTotal == nil {
		t.Error("AgentraceCostUSDTotal not registered")
	}
	if r.AgentraceBottlenecksTotal == nil {
		t.Error("AgentraceBottlenecksTotal not registered")
	}
	if r.AgentraceParallelismRatio == nil {
		t.Error("AgentraceParallelismRatio not registered")
	}
}

func TestAgentraceMetrics_ValuesUpdatedAfterAdapterRun(t *testing.T) {
	t.Parallel()
	r := NewRegistry("test")

	r.AgentraceSessionDuration.Observe(300.0, Labels{})
	r.AgentraceToolCallsTotal.Add(5, Labels{"tool_name": "Shell", "outcome": "ok"})
	r.AgentraceToolCallsTotal.Add(2, Labels{"tool_name": "Read", "outcome": "ok"})
	r.AgentraceToolCallsTotal.Add(1, Labels{"tool_name": "Shell", "outcome": "error"})
	r.AgentraceCostUSDTotal.Add(0.42, Labels{"session_id": "s1"})
	r.AgentraceBottlenecksTotal.Inc(Labels{"severity": "high"})
	r.AgentraceBottlenecksTotal.Inc(Labels{"severity": "low"})
	r.AgentraceParallelismRatio.Set(0.78, Labels{})

	var sb strings.Builder
	r.AgentraceSessionDuration.write(&sb)
	r.AgentraceToolCallsTotal.write(&sb)
	r.AgentraceCostUSDTotal.write(&sb)
	r.AgentraceBottlenecksTotal.write(&sb)
	r.AgentraceParallelismRatio.write(&sb)
	output := sb.String()

	checks := []string{
		"ec_agentrace_session_duration_seconds",
		"ec_agentrace_tool_calls_total",
		"ec_agentrace_cost_usd_total",
		"ec_agentrace_bottlenecks_total",
		"ec_agentrace_parallelism_ratio",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("expected %q in metrics output", check)
		}
	}
}

func TestAgentraceMetrics_ZeroWhenUnavailable(t *testing.T) {
	t.Parallel()
	r := NewRegistry("test")

	var sb strings.Builder
	r.AgentraceSessionDuration.write(&sb)
	r.AgentraceToolCallsTotal.write(&sb)
	r.AgentraceCostUSDTotal.write(&sb)
	r.AgentraceBottlenecksTotal.write(&sb)
	r.AgentraceParallelismRatio.write(&sb)
	output := sb.String()

	if output != "" {
		t.Errorf("expected empty output when no observations, got:\n%s", output)
	}
}

func TestApplyAgentraceKPIs_ExportsStoryMetrics(t *testing.T) {
	t.Parallel()
	r := NewRegistry("test")

	ApplyAgentraceKPIs(r, evomap.AgentraceKPIs{
		Available:          true,
		SessionDurationSec: 300,
		ToolCallCount:      2,
		CostUSD:            0.42,
		BottleneckCount:    1,
		ParallelismRatio:   0.75,
		ToolUsage: map[string]int{
			"Shell": 2,
		},
		ToolErrors: map[string]int{
			"Shell": 1,
		},
		Stories: []evomap.AgentraceStoryKPI{
			{
				SessionID:      "agentic-ecommerce__v5009r-2",
				SprintID:       "v5009r",
				StoryID:        "v5009r-2",
				Repo:           "agentic-ecommerce",
				Branch:         "feat/v5009r-agentrace-story-metrics",
				RemoteTarget:   "node-a-travel",
				WallSeconds:    300,
				ActiveSeconds:  120,
				BlockedSeconds: 180,
				Outcome:        "blocked",
			},
		},
	})

	var sb strings.Builder
	r.AgentraceSessionDuration.write(&sb)
	r.AgentraceToolCallsTotal.write(&sb)
	r.AgentraceCostUSDTotal.write(&sb)
	r.AgentraceBottlenecksTotal.write(&sb)
	r.AgentraceParallelismRatio.write(&sb)
	r.AgentraceStoryWallSeconds.write(&sb)
	r.AgentraceStoryActiveSeconds.write(&sb)
	r.AgentraceStoryBlockedSeconds.write(&sb)
	r.AgentraceStoryOutcomesTotal.write(&sb)
	output := sb.String()

	checks := []string{
		"ec_agentrace_story_wall_seconds",
		"ec_agentrace_story_active_seconds",
		"ec_agentrace_story_blocked_seconds",
		"ec_agentrace_story_outcomes_total",
		"story_id=\"v5009r-2\"",
		"remote_target=\"node-a-travel\"",
		"outcome=\"blocked\"",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Fatalf("expected %q in metrics output:\n%s", check, output)
		}
	}
}
