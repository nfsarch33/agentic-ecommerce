package evomap_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/evomap"
)

func TestBuildMinimaxKPIsFromSnapshots(t *testing.T) {
	t.Parallel()

	prev := evomap.MinimaxSnapshot{
		RequestsByKey:     map[string]int{"1": 100, "2": 50},
		FailoverTotal:     2,
		LatencySumByKey:   map[string]float64{"1": 5000, "2": 2500},
		LatencyCountByKey: map[string]int{"1": 100, "2": 50},
		QuotaExhaustByKey: map[string]int{"1": 1, "2": 0},
	}
	curr := evomap.MinimaxSnapshot{
		RequestsByKey:     map[string]int{"1": 150, "2": 75},
		FailoverTotal:     3,
		LatencySumByKey:   map[string]float64{"1": 7500, "2": 3750},
		LatencyCountByKey: map[string]int{"1": 150, "2": 75},
		QuotaExhaustByKey: map[string]int{"1": 2, "2": 0},
	}

	kpis := evomap.BuildMinimaxKPIs(prev, curr)

	if kpis.RequestsTotal["1"] != 50 {
		t.Fatalf("key1 requests = %d, want 50", kpis.RequestsTotal["1"])
	}
	if kpis.RequestsTotal["2"] != 25 {
		t.Fatalf("key2 requests = %d, want 25", kpis.RequestsTotal["2"])
	}
	if kpis.FailoverCount != 1 {
		t.Fatalf("failover count = %d, want 1", kpis.FailoverCount)
	}
	if kpis.AvgLatencyMs["1"] != 50 {
		t.Fatalf("key1 avg latency = %f, want 50", kpis.AvgLatencyMs["1"])
	}
	if kpis.QuotaExhaustionCount["1"] != 1 {
		t.Fatalf("key1 quota exhaust = %d, want 1", kpis.QuotaExhaustionCount["1"])
	}
}

func TestBuildMinimaxKPIsZeroUsageHandled(t *testing.T) {
	t.Parallel()

	empty := evomap.MinimaxSnapshot{
		RequestsByKey:     map[string]int{},
		LatencySumByKey:   map[string]float64{},
		LatencyCountByKey: map[string]int{},
		QuotaExhaustByKey: map[string]int{},
	}

	kpis := evomap.BuildMinimaxKPIs(empty, empty)

	if kpis.FailoverCount != 0 {
		t.Fatalf("failover count = %d, want 0", kpis.FailoverCount)
	}
	if len(kpis.RequestsTotal) != 0 {
		t.Fatalf("requests = %v, want empty", kpis.RequestsTotal)
	}
	if len(kpis.AvgLatencyMs) != 0 {
		t.Fatalf("avg latency = %v, want empty", kpis.AvgLatencyMs)
	}
}

func TestDetectImbalanceGeneratesInsight(t *testing.T) {
	t.Parallel()

	kpis := evomap.MinimaxKPIs{
		QuotaExhaustionCount: map[string]int{"1": 8, "2": 1},
	}

	insight := evomap.DetectImbalance(kpis)
	if insight == "" {
		t.Fatal("expected imbalance insight, got empty")
	}
	if !containsStr(insight, "key 1") {
		t.Fatalf("insight = %q, expected to mention key 1", insight)
	}
}

func TestDetectImbalanceNoInsightWhenBalanced(t *testing.T) {
	t.Parallel()

	kpis := evomap.MinimaxKPIs{
		QuotaExhaustionCount: map[string]int{"1": 3, "2": 3},
	}

	insight := evomap.DetectImbalance(kpis)
	if insight != "" {
		t.Fatalf("expected no insight when balanced, got %q", insight)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
