package channel

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// BenchmarkTikTokListingAgent_HandleEvent measures the per-event
// publish path through the EC-3-2 agent. Exercises payload decode +
// adaptPayload + the synchronous social.Client publish call.
func BenchmarkTikTokListingAgent_HandleEvent(b *testing.B) {
	bus := eventbus.NewInMemoryBus()
	defer bus.Close()
	fc := &fakeSocialClient{createReturnID: "tt-bench"}
	agent, err := NewTikTokListingAgent(nil, TikTokListingConfig{
		Client:           fc,
		Publisher:        bus,
		Consumer:         bus,
		TenantID:         "tenant-bench",
		DefaultShipping:  "ship-default",
		Now:              func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
		CategoryMapper:   func(c string) string { return "tt-" + c },
		ShippingResolver: func(_, c string) string { return "ship-" + c },
	})
	if err != nil {
		b.Fatalf("NewTikTokListingAgent: %v", err)
	}
	defer agent.Close(context.Background())
	payload := eventbus.ProductEnrichedPayload{
		Version:            1,
		TenantID:           "tenant-bench",
		ProductID:          "p-bench",
		ExternalID:         "ext-bench",
		EnglishTitle:       "Bench Product",
		EnglishDescription: "Bench description.",
		CategoryID:         "audio",
		PriceCents:         4999,
		Currency:           "AUD",
		StockUnits:         50,
		QualityScore:       0.9,
	}
	evt, err := eventbus.NewProductEnrichedEvent("agent.enrichment", time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC), payload)
	if err != nil {
		b.Fatalf("NewProductEnrichedEvent: %v", err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := agent.HandleEvent(context.Background(), evt); err != nil {
			b.Fatalf("HandleEvent: %v", err)
		}
	}
}
