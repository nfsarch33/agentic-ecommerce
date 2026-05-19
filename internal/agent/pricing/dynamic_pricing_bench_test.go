// Benchmarks for the v3.5.0 EC-6-3 dynamic pricing agent hot path.
// The agent's Decide method is on the LLM path; the bench drives
// the pure helpers + the LLM-failover path with a deterministic
// rule fallback so the per-call allocation budget surfaces.
package pricing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/billing"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

type benchPublisher struct{}

func (benchPublisher) Publish(_ context.Context, _ eventbus.Event) error { return nil }
func (benchPublisher) Close() error                                      { return nil }

type benchLLMUnavailable struct{}

func (benchLLMUnavailable) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	return port.AICompletionResponse{}, errors.New("benchmark: llm unavailable")
}

func BenchmarkPricingAgent_DecideRuleFallback(b *testing.B) {
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	calc, _ := billing.NewPlatformFeeCalculator(billing.PlatformFeeCalculatorConfig{
		FXProvider: billing.NewStaticFXRateProvider(billing.FXRate{AUDPerCNY: 0.21, FetchedAt: now, Source: "bench"}),
		Now:        func() time.Time { return now },
	})
	agent, err := NewPricingAgent(nil, PricingAgentConfig{
		TenantID:                "tenant-bench",
		FeeCalculator:           calc,
		Publisher:               benchPublisher{},
		LLM:                     benchLLMUnavailable{},
		MarginFloorPct:          0.35,
		LargeChangeThresholdPct: 0.15,
		Now:                     func() time.Time { return now },
	})
	if err != nil {
		b.Fatalf("NewPricingAgent: %v", err)
	}
	defer agent.Close(context.Background())
	in := PriceDecisionInput{
		ProductID:               "p-bench",
		Channel:                 billing.ChannelTikTok,
		CurrentPriceAUDCents:    6000,
		CostCNYCents:            8000,
		ShippingEstAUDCents:     300,
		CompetitorPriceAUDCents: 5800,
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Decide(ctx, in)
	}
}

func BenchmarkPricingAgent_MinPriceForMarginFloor(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = minPriceForMarginFloor(2000, 200, billing.ChannelTikTok, 0.05, 0.35)
	}
}
