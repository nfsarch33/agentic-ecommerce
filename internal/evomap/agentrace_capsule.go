package evomap

import (
	"fmt"
	"sort"
	"strings"
)

// AgentraceCapsuleSection extends the existing markdown capsule with
// Agentrace-derived insight sections. Called by the capsule emitter
// at sprint-end or on-demand rollup.

// RenderAgentraceSummary produces the `## Agentrace Summary` section
// for inclusion in an EvoLoop capsule markdown document.
func RenderAgentraceSummary(kpis AgentraceKPIs) string {
	if !kpis.Available {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Agentrace Summary\n\n")
	sb.WriteString(fmt.Sprintf("- sessions observed: %d\n", countSessions(kpis)))
	sb.WriteString(fmt.Sprintf("- total session duration: %.1fs\n", kpis.SessionDurationSec))
	sb.WriteString(fmt.Sprintf("- total cost: $%.4f USD\n", kpis.CostUSD))
	sb.WriteString(fmt.Sprintf("- avg parallelism efficiency: %.2f\n", kpis.ParallelismRatio))
	sb.WriteString(fmt.Sprintf("- bottlenecks detected: %d\n", kpis.BottleneckCount))
	sb.WriteString(fmt.Sprintf("- total tool calls: %d\n\n", kpis.ToolCallCount))
	return sb.String()
}

// RenderAgentraceToolUsage produces the `## Agentrace Tool Usage`
// section with per-tool call counts and error rates.
func RenderAgentraceToolUsage(kpis AgentraceKPIs) string {
	if !kpis.Available || len(kpis.ToolUsage) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Agentrace Tool Usage\n\n")
	sb.WriteString("| Tool | Calls | Errors | Error Rate |\n")
	sb.WriteString("|------|-------|--------|------------|\n")
	tools := sortedToolNames(kpis.ToolUsage)
	for _, tool := range tools {
		calls := kpis.ToolUsage[tool]
		errors := kpis.ToolErrors[tool]
		rate := 0.0
		if calls > 0 {
			rate = float64(errors) / float64(calls) * 100
		}
		fmt.Fprintf(&sb, "| %s | %d | %d | %.1f%% |\n", tool, calls, errors, rate)
	}
	sb.WriteString("\n")
	return sb.String()
}

// RenderAgentraceCapsuleExtension combines summary + tool usage for
// inclusion at the end of an existing capsule markdown document.
func RenderAgentraceCapsuleExtension(kpis AgentraceKPIs) string {
	return RenderAgentraceSummary(kpis) + RenderAgentraceToolUsage(kpis)
}

func countSessions(kpis AgentraceKPIs) int {
	if kpis.SessionDurationSec > 0 {
		return 1
	}
	return 0
}

func sortedToolNames(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
