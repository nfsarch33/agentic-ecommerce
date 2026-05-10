package metrics

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
}

var defaultAgentraceSessionBuckets = []float64{
	10, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200,
}
