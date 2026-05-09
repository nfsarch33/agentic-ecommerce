// Benchmarks for the v3.5.0 EC-6-2 platform fee + FX calculator
// hot path. The pricing agent calls CalculateMargin once per
// product on every supplier-cost-changed event so the per-call
// allocation budget matters.
package billing

import (
	"context"
	"testing"
	"time"
)

func BenchmarkPlatformFee_CalculateMargin(b *testing.B) {
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	calc, err := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{
		FXProvider: NewStaticFXRateProvider(FXRate{AUDPerCNY: 0.21, FetchedAt: now, Source: "bench"}),
		Now:        func() time.Time { return now },
	})
	if err != nil {
		b.Fatalf("NewPlatformFeeCalculator: %v", err)
	}
	in := PriceComponents{
		Channel:              ChannelTikTok,
		SellingPriceAUDCents: 6000,
		CostCNYCents:         8000,
		ShippingEstAUDCents:  500,
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = calc.CalculateMargin(ctx, in)
	}
}

func BenchmarkPlatformFee_PerChannel(b *testing.B) {
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	calc, _ := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{
		FXProvider: NewStaticFXRateProvider(FXRate{AUDPerCNY: 0.21, FetchedAt: now, Source: "bench"}),
		Now:        func() time.Time { return now },
	})
	channels := []Channel{ChannelTikTok, ChannelFacebook, ChannelWooCommerce}
	in := PriceComponents{SellingPriceAUDCents: 5000}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in.Channel = channels[i%len(channels)]
		_, _ = calc.PlatformFee(in)
	}
}
