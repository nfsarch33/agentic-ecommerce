package metrics

import "github.com/nfsarch33/helixon-ec/internal/evomap"

// RegisterAgentraceMetrics extends the Registry with Agentrace-derived
// metric surfaces. Called once during NewRegistry construction.
//
// Cardinality budget per series (v4.11.0):
//
//	ec_agentrace_session_duration_seconds histogram  ~ 1 series (10 buckets).
//	ec_agentrace_tool_calls_total{tool_name, outcome} ~ 60 series.
//	ec_agentrace_cost_usd_total{session_id}           ~ 20 series.
//	ec_agentrace_bottlenecks_total{severity}           ~ 10 series.
//	ec_agentrace_parallelism_ratio gauge               ~ 1 series.
//
// Total ~ 92 additive series; well under the per-binary 10_000 cap.
func RegisterAgentraceMetrics(r *Registry) {
	r.AgentraceSessionDuration = newHistogram(
		r,
		"ec_agentrace_session_duration_seconds",
		"v4.11.0 Agentrace session duration histogram.",
		defaultAgentraceSessionBuckets,
	)
	r.AgentraceToolCallsTotal = newCounter(
		r,
		"ec_agentrace_tool_calls_total",
		"v4.11.0 Agentrace tool call count by tool_name + outcome.",
	)
	r.AgentraceCostUSDTotal = newCounter(
		r,
		"ec_agentrace_cost_usd_total",
		"v4.11.0 Agentrace session cost (USD) by session_id.",
	)
	r.AgentraceBottlenecksTotal = newCounter(
		r,
		"ec_agentrace_bottlenecks_total",
		"v4.11.0 Agentrace bottleneck detections by severity.",
	)
	r.AgentraceParallelismRatio = newGauge(
		r,
		"ec_agentrace_parallelism_ratio",
		"v4.11.0 Agentrace agent parallelism efficiency (0..1).",
	)
	r.AgentraceStoryWallSeconds = newGauge(
		r,
		"ec_agentrace_story_wall_seconds",
		"v5.0.9r Agentrace story wall-clock seconds by sprint_id + story_id + repo + branch + remote_target + session_id.",
	)
	r.AgentraceStoryActiveSeconds = newGauge(
		r,
		"ec_agentrace_story_active_seconds",
		"v5.0.9r Agentrace story active execution seconds by sprint_id + story_id + repo + branch + remote_target + session_id.",
	)
	r.AgentraceStoryBlockedSeconds = newGauge(
		r,
		"ec_agentrace_story_blocked_seconds",
		"v5.0.9r Agentrace story blocked seconds by sprint_id + story_id + repo + branch + remote_target + session_id.",
	)
	r.AgentraceStoryOutcomesTotal = newCounter(
		r,
		"ec_agentrace_story_outcomes_total",
		"v5.0.9r Agentrace story outcome reductions by sprint_id + story_id + repo + branch + remote_target + session_id + outcome.",
	)
}

var defaultAgentraceSessionBuckets = []float64{
	10, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200,
}

func ApplyAgentraceKPIs(r *Registry, k evomap.AgentraceKPIs) {
	if r == nil || !k.Available {
		return
	}

	if k.SessionDurationSec > 0 {
		r.AgentraceSessionDuration.Observe(k.SessionDurationSec, Labels{})
	}
	if k.CostUSD > 0 {
		r.AgentraceCostUSDTotal.Add(k.CostUSD, Labels{"session_id": costSessionLabel(k.Stories)})
	}
	if k.BottleneckCount > 0 {
		r.AgentraceBottlenecksTotal.Add(float64(k.BottleneckCount), Labels{"severity": "all"})
	}
	if k.ParallelismRatio > 0 {
		r.AgentraceParallelismRatio.Set(k.ParallelismRatio, Labels{})
	}
	for tool, count := range k.ToolUsage {
		errors := k.ToolErrors[tool]
		if okCalls := count - errors; okCalls > 0 {
			r.AgentraceToolCallsTotal.Add(float64(okCalls), Labels{"tool_name": tool, "outcome": "ok"})
		}
		if errors > 0 {
			r.AgentraceToolCallsTotal.Add(float64(errors), Labels{"tool_name": tool, "outcome": "error"})
		}
	}
	for _, story := range k.Stories {
		labels := agentraceStoryLabels(story)
		r.AgentraceStoryWallSeconds.Set(story.WallSeconds, labels)
		r.AgentraceStoryActiveSeconds.Set(story.ActiveSeconds, labels)
		r.AgentraceStoryBlockedSeconds.Set(story.BlockedSeconds, labels)
		if story.Outcome != "" {
			outcomeLabels := copyLabels(labels)
			outcomeLabels["outcome"] = story.Outcome
			r.AgentraceStoryOutcomesTotal.Inc(outcomeLabels)
		}
	}
}

func agentraceStoryLabels(story evomap.AgentraceStoryKPI) Labels {
	return Labels{
		"session_id":    fallbackLabel(story.SessionID, "unknown"),
		"sprint_id":     fallbackLabel(story.SprintID, "unknown"),
		"story_id":      fallbackLabel(story.StoryID, "unknown"),
		"repo":          fallbackLabel(story.Repo, "unknown"),
		"branch":        fallbackLabel(story.Branch, "unknown"),
		"remote_target": fallbackLabel(story.RemoteTarget, "unknown"),
	}
}

func fallbackLabel(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func copyLabels(src Labels) Labels {
	dst := make(Labels, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func costSessionLabel(stories []evomap.AgentraceStoryKPI) string {
	if len(stories) == 1 && stories[0].SessionID != "" {
		return stories[0].SessionID
	}
	return "aggregate"
}
