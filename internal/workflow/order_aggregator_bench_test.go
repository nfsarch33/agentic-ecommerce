// Benchmarks for the v3.5.0 EC-7-1 order aggregator activities.
// The Temporal workflow body is replay-safe deterministic; the
// bench focuses on the pure normalisation activity which is the
// per-order hot path.
package workflow

import (
	"context"
	"testing"
	"time"
)

func BenchmarkOrderAggregator_NormaliseChannelOrder(b *testing.B) {
	activities := NewOrderAggregatorActivities(OrderAggregatorActivityDeps{})
	in := ChannelOrderInput{
		TenantID:        "tenant-bench",
		Channel:         "tiktok",
		ExternalOrderID: "ord-bench",
		BuyerEmail:      "bench@example.com",
		TotalCents:      9990,
		Currency:        "AUD",
		Status:          "paid",
		Items: []ChannelOrderLine{
			{SKU: "sku-A", Quantity: 1, UnitCents: 9990},
		},
		OccurredAt: time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC),
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = activities.NormaliseChannelOrder(ctx, in)
	}
}

func BenchmarkOrderAggregator_BuildDedupKey(b *testing.B) {
	o := OrderNormalised{TenantID: "t", Channel: "tiktok", ExternalOrderID: "777"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = buildDedupKey(o)
	}
}
