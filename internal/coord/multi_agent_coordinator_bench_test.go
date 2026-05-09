// File scope: v3.5.1 MADRL coordinator microbench. Tracks the
// per-Coordinate latency for the seed pipeline so the future
// MADRL/CTDE replacement can be A/B'd on the same fixture.
//
// Benchmarks intentionally cover the three canonical fan-out
// shapes the v3.5.x sprints produce:
//
//   - 2 decisions on 1 SKU (the canonical conflict surface)
//   - 5 decisions on 5 SKUs (orthogonal pass-through)
//   - 10 decisions on 1 SKU (worst-case n*(n-1)/2 pair scan)
package coord

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func benchCoordinator(b *testing.B) *Coordinator {
	b.Helper()
	c, err := NewCoordinator(nil, CoordinatorConfig{
		TenantID: "tenant-bench",
		Now:      func() time.Time { return time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		b.Fatalf("NewCoordinator: %v", err)
	}
	b.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func BenchmarkCoordinator_TwoDecisionsOneSKU(b *testing.B) {
	c := benchCoordinator(b)
	ts := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	decisions := []AgentDecision{
		{AgentName: "pricing", TenantID: "tenant-bench", SKU: "sku-B1", Action: ActionKindPriceChange, ProposedAt: ts},
		{AgentName: "fulfilment", TenantID: "tenant-bench", SKU: "sku-B1", Action: ActionKindInventoryDeplete, ProposedAt: ts.Add(time.Second)},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Coordinate(context.Background(), decisions); err != nil {
			b.Fatalf("Coordinate: %v", err)
		}
	}
}

func BenchmarkCoordinator_FiveDecisionsFiveSKUs(b *testing.B) {
	c := benchCoordinator(b)
	ts := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	decisions := make([]AgentDecision, 0, 5)
	for i := 0; i < 5; i++ {
		decisions = append(decisions, AgentDecision{
			AgentName:  fmt.Sprintf("agent-%d", i),
			TenantID:   "tenant-bench",
			SKU:        fmt.Sprintf("sku-B%d", i),
			Action:     ActionKindContentRefresh,
			ProposedAt: ts.Add(time.Duration(i) * time.Second),
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Coordinate(context.Background(), decisions); err != nil {
			b.Fatalf("Coordinate: %v", err)
		}
	}
}

func BenchmarkCoordinator_TenDecisionsOneSKU(b *testing.B) {
	c := benchCoordinator(b)
	ts := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	decisions := make([]AgentDecision, 0, 10)
	for i := 0; i < 10; i++ {
		action := ActionKindPriceChange
		if i%2 == 1 {
			action = ActionKindInventoryDeplete
		}
		decisions = append(decisions, AgentDecision{
			AgentName:  fmt.Sprintf("agent-%02d", i),
			TenantID:   "tenant-bench",
			SKU:        "sku-WORST",
			Action:     action,
			ProposedAt: ts.Add(time.Duration(i) * time.Second),
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Coordinate(context.Background(), decisions); err != nil {
			b.Fatalf("Coordinate: %v", err)
		}
	}
}
