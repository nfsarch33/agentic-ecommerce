// Benchmarks for the v3.5.0 EC-7-2 dropship agent. The Place
// pipeline is the per-order hot path; the bench drives the
// happy-path with a deterministic supplier double so the
// allocation budget surfaces.
package fulfilment

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

type benchSupplier struct{ name string }

func (b *benchSupplier) SupplierName() string { return b.name }
func (b *benchSupplier) PlaceOrder(_ context.Context, _ SupplierOrderRequest) (SupplierOrderResult, error) {
	return SupplierOrderResult{SupplierOrderID: "ord-1"}, nil
}

type benchPublisher struct{}

func (benchPublisher) Publish(_ context.Context, _ eventbus.Event) error { return nil }
func (benchPublisher) Close() error                                      { return nil }

type benchTrigger struct{}

func (benchTrigger) Trigger(_ context.Context, _ string) error  { return nil }
func (benchTrigger) Rollback(_ context.Context, _ string) error { return nil }

func BenchmarkDropshipAgent_Place(b *testing.B) {
	agent, err := NewDropshipAgent(nil, DropshipAgentConfig{
		TenantID:                 "tenant-bench",
		Primary:                  &benchSupplier{name: "1688"},
		Publisher:                benchPublisher{},
		FulfilmentTrigger:        benchTrigger{},
		LargeOrderThresholdCents: 50000,
		Now:                      func() time.Time { return time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		b.Fatalf("NewDropshipAgent: %v", err)
	}
	defer agent.Close(context.Background())
	order := NormalisedOrder{
		TenantID:        "tenant-bench",
		OrderID:         "ord-bench",
		ExternalOrderID: "bench",
		Channel:         "tiktok",
		BuyerEmail:      "bench@example.com",
		TotalAUDCents:   20000,
		Currency:        "AUD",
		Items:           []NormalisedOrderLine{{SKU: "sku-A", Quantity: 1, UnitCents: 20000}},
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Place(ctx, order)
	}
}
