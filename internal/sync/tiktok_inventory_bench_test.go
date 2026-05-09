package sync

import (
	"context"
	"strconv"
	"testing"
)

// BenchmarkInventorySync_ApplyWCFulfilment measures the per-call
// cost of the EC-3-4 saga happy path: idempotency reserve, source
// adjust, target adjust, metric record.
func BenchmarkInventorySync_ApplyWCFulfilment(b *testing.B) {
	wc := &recordingAdjuster{}
	tt := &recordingAdjuster{}
	saga, err := NewTikTokInventorySync(nil, InventorySyncConfig{
		WC:      wc,
		TikTok:  tt,
		Metrics: &recordingInventoryMetrics{},
	})
	if err != nil {
		b.Fatalf("NewTikTokInventorySync: %v", err)
	}
	defer saga.Close(context.Background())
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := StockAdjustRequest{
			TenantID: "tenant-bench",
			SKU:      "SKU-bench",
			Delta:    -1,
			OrderID:  "bench-order-" + strconv.Itoa(i),
		}
		if err := saga.ApplyWCFulfilment(context.Background(), req); err != nil {
			b.Fatalf("ApplyWCFulfilment: %v", err)
		}
	}
}
