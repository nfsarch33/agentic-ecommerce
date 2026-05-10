package metrics

// RegisterMinimaxMetrics extends the Registry with MiniMax quota-aware
// adapter metric surfaces. Called once during NewRegistry construction.
//
// Cardinality budget per series (v4.13.0):
//
//	ec_minimax_requests_total{key_alias, status}
//	  ~ key_aliases(2) * statuses(4: success|rate_limited|quota_exhausted|error) = 8 series.
//	ec_minimax_request_duration_seconds{key_alias} histogram
//	  ~ key_aliases(2) = 2 series (10 buckets each = 20 text lines).
//	ec_minimax_active_key{key_alias} gauge
//	  ~ key_aliases(2) = 2 series.
//	ec_minimax_key_cooldown_remaining_seconds{key_alias} gauge
//	  ~ key_aliases(2) = 2 series.
//	ec_minimax_failover_events_total{from_key, to_key}
//	  ~ from(2) * to(2) = 4 series.
//
// Total ~ 36-38 additive series; well under the per-binary 10_000 cap.
func RegisterMinimaxMetrics(r *Registry) {
	r.MinimaxRequestsTotal = newCounter(
		r,
		"ec_minimax_requests_total",
		"v4.13.0 MiniMax API requests by key_alias + status (success|rate_limited|quota_exhausted|error).",
	)
	r.MinimaxRequestDuration = newHistogram(
		r,
		"ec_minimax_request_duration_seconds",
		"v4.13.0 MiniMax API request duration histogram by key_alias.",
		defaultDurationBuckets,
	)
	r.MinimaxActiveKey = newGauge(
		r,
		"ec_minimax_active_key",
		"v4.13.0 MiniMax active key indicator (1=active, 0=standby) by key_alias.",
	)
	r.MinimaxKeyCooldownRemaining = newGauge(
		r,
		"ec_minimax_key_cooldown_remaining_seconds",
		"v4.13.0 MiniMax key cooldown remaining seconds by key_alias.",
	)
	r.MinimaxFailoverEventsTotal = newCounter(
		r,
		"ec_minimax_failover_events_total",
		"v4.13.0 MiniMax failover events by from_key + to_key.",
	)
}
