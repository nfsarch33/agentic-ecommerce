package evomap

// MinimaxKPIs holds MiniMax usage measurements for a single EvoMap
// capsule emission cycle. Additive fields on the existing KPIs struct
// so prior schema readers keep working.
type MinimaxKPIs struct {
	RequestsTotal        map[string]int     `json:"minimax_requests_total,omitempty"`
	FailoverCount        int                `json:"minimax_failover_count,omitempty"`
	AvgLatencyMs         map[string]float64 `json:"minimax_avg_latency_ms,omitempty"`
	QuotaExhaustionCount map[string]int     `json:"minimax_quota_exhaustion_count,omitempty"`
}

// MinimaxSnapshot captures the raw counters from the observability
// layer at a point in time. The KPI builder diffs snapshots to
// compute per-period deltas.
type MinimaxSnapshot struct {
	RequestsByKey     map[string]int
	FailoverTotal     int
	LatencySumByKey   map[string]float64
	LatencyCountByKey map[string]int
	QuotaExhaustByKey map[string]int
}

// BuildMinimaxKPIs computes period-delta KPIs from two snapshots.
func BuildMinimaxKPIs(prev, curr MinimaxSnapshot) MinimaxKPIs {
	kpis := MinimaxKPIs{
		RequestsTotal:        make(map[string]int),
		AvgLatencyMs:         make(map[string]float64),
		QuotaExhaustionCount: make(map[string]int),
	}

	for key, count := range curr.RequestsByKey {
		delta := count - prev.RequestsByKey[key]
		if delta > 0 {
			kpis.RequestsTotal[key] = delta
		}
	}

	kpis.FailoverCount = curr.FailoverTotal - prev.FailoverTotal
	if kpis.FailoverCount < 0 {
		kpis.FailoverCount = 0
	}

	for key, sumMs := range curr.LatencySumByKey {
		countDelta := curr.LatencyCountByKey[key] - prev.LatencyCountByKey[key]
		sumDelta := sumMs - prev.LatencySumByKey[key]
		if countDelta > 0 && sumDelta > 0 {
			kpis.AvgLatencyMs[key] = sumDelta / float64(countDelta)
		}
	}

	for key, count := range curr.QuotaExhaustByKey {
		delta := count - prev.QuotaExhaustByKey[key]
		if delta > 0 {
			kpis.QuotaExhaustionCount[key] = delta
		}
	}

	return kpis
}

// DetectImbalance generates an insight if one key exhausts quota
// significantly more often than the other. Returns empty string if
// balanced or insufficient data.
func DetectImbalance(kpis MinimaxKPIs) string {
	ex1 := kpis.QuotaExhaustionCount["1"]
	ex2 := kpis.QuotaExhaustionCount["2"]
	total := ex1 + ex2
	if total < 2 {
		return ""
	}
	ratio := float64(max(ex1, ex2)) / float64(total)
	if ratio > 0.75 {
		heavyKey := "1"
		if ex2 > ex1 {
			heavyKey = "2"
		}
		return "minimax_quota_imbalance: key " + heavyKey +
			" exhausts quota disproportionately; consider rebalancing sticky key preference"
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
