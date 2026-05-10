package metrics

// registerMem0Metrics extends the Registry with the v4.17.0 mem0
// memory-layer client metric surfaces. Called once during NewRegistry
// construction.
//
// Cardinality budget per series (v4.17.0):
//
//	ec_mem0_requests_total{op, status}
//	  ~ ops(3: store/search/delete) * statuses(4: ok/error/
//	    circuit_open/disabled) = 12 series.
//	ec_mem0_request_duration_seconds{op}
//	  ~ ops(3) * buckets(10) + sum + count = ~36 series.
//
// Total ~ 20 logical series; well under the per-binary 10_000 cap.
func registerMem0Metrics(r *Registry) {
	r.Mem0Requests = newCounter(
		r,
		"ec_mem0_requests_total",
		"v4.17.0 mem0 memory-layer requests by op + status.",
	)
	r.Mem0Duration = newHistogram(
		r,
		"ec_mem0_request_duration_seconds",
		"v4.17.0 mem0 memory-layer request duration by op.",
		defaultDurationBuckets,
	)
}
