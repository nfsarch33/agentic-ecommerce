// File scope: v3.5.0 EC-6-2 platform fee + AUD/CNY FX calculator
// RED tests. TDD-first per the v3.5.0 plan (story 1; pure logic
// foundation that EC-6-1 + EC-6-3 build on).
//
// 5-scenario margin acceptance per ADR-028 EC-6-2 spec:
//
//	┌────────────┬──────────┬──────────┬────────┬──────────┬─────────────┐
//	│ Scenario   │ Channel  │ Sell A$  │ Cost ¥ │ Ship A$  │ Margin %    │
//	├────────────┼──────────┼──────────┼────────┼──────────┼─────────────┤
//	│ A baseline │ WC       │ 100.00   │ 40     │ 5.00     │ 84.55%      │
//	│ B default  │ TikTok 5%│ 50.00    │ 30     │ 3.00     │ 76.40%      │
//	│ C reduced  │ TikTok 2%│ 200.00   │ 80     │ 8.00     │ 85.60%      │
//	│ D facebook │ Facebook │ 75.00    │ 50     │ 4.00     │ 75.67%      │
//	│ E low-mar  │ WC stress│ 30.00    │ 100    │ 2.00     │ 20.58%      │
//	└────────────┴──────────┴──────────┴────────┴──────────┴─────────────┘
//
// Fixed FX rate AUD/CNY = 0.21 (1 CNY = A$0.21) so callers can
// reason about every cell on a calculator.
package billing

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

// staticFXProvider is the in-memory FXRateProvider used by EC-6-2
// tests. The plan says "stub HTTP client + file-backed cache for
// now"; this struct backs the unit tests + the
// FXRateFileCacheProvider integration tests share a fixture.
type staticFXProvider struct {
	rate FXRate
	err  error
}

func (s staticFXProvider) LatestRate(_ context.Context) (FXRate, error) {
	if s.err != nil {
		return FXRate{}, s.err
	}
	return s.rate, nil
}

// fixedNow returns a stable clock for deterministic staleness
// checks. Mirrors the v2.5.0 webhook_test.go fixedNow pattern.
func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// rateAt builds an FXRate at the supplied timestamp. Default rate
// is 0.21 AUD per CNY (the documented v3.5.0 acceptance value).
func rateAt(ts time.Time) FXRate {
	return FXRate{AUDPerCNY: 0.21, FetchedAt: ts, Source: "test-static"}
}

// approxEqualPct asserts the actual margin matches expected within
// a small tolerance. The acceptance scenarios are computed in cents
// so the rounding is bounded by 1/SellingPriceAUDCents per
// scenario.
func approxEqualPct(t *testing.T, name string, expected, actual float64, tol float64) {
	t.Helper()
	if math.Abs(expected-actual) > tol {
		t.Fatalf("%s: margin pct mismatch: want %.4f got %.4f (tol %.4f)", name, expected, actual, tol)
	}
}

func TestPlatformFee_CalculatesNetMarginAfterFeesAndFX(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	rate := rateAt(now.Add(-1 * time.Hour))
	calc, err := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{
		FXProvider: staticFXProvider{rate: rate},
		Now:        fixedNow(now),
	})
	if err != nil {
		t.Fatalf("NewPlatformFeeCalculator: %v", err)
	}

	cases := []struct {
		name          string
		input         PriceComponents
		wantMarginPct float64
		wantFeeCents  int
		wantNetCents  int
	}{
		{
			name: "A_woocommerce_baseline",
			input: PriceComponents{
				Channel:              ChannelWooCommerce,
				SellingPriceAUDCents: 10000,
				CostCNYCents:         4000,
				ShippingEstAUDCents:  500,
			},
			// fee = round(10000 * 0.0175) + 30 = 175 + 30 = 205
			// cost_aud = round(4000 * 0.21) = 840
			// net = 10000 - 840 - 205 - 500 = 8455
			// margin = 8455 / 10000 = 0.8455
			wantMarginPct: 0.8455,
			wantFeeCents:  205,
			wantNetCents:  8455,
		},
		{
			name: "B_tiktok_default_5pct",
			input: PriceComponents{
				Channel:              ChannelTikTok,
				SellingPriceAUDCents: 5000,
				CostCNYCents:         3000,
				ShippingEstAUDCents:  300,
			},
			// fee = round(5000 * 0.05) = 250
			// cost_aud = round(3000 * 0.21) = 630
			// net = 5000 - 630 - 250 - 300 = 3820
			// margin = 3820 / 5000 = 0.764
			wantMarginPct: 0.7640,
			wantFeeCents:  250,
			wantNetCents:  3820,
		},
		{
			name: "C_tiktok_reduced_2pct",
			input: PriceComponents{
				Channel:              ChannelTikTok,
				SellingPriceAUDCents: 20000,
				CostCNYCents:         8000,
				ShippingEstAUDCents:  800,
				TikTokCommissionPct:  0.02,
			},
			// fee = round(20000 * 0.02) = 400
			// cost_aud = round(8000 * 0.21) = 1680
			// net = 20000 - 1680 - 400 - 800 = 17120
			// margin = 17120 / 20000 = 0.856
			wantMarginPct: 0.8560,
			wantFeeCents:  400,
			wantNetCents:  17120,
		},
		{
			name: "D_facebook_5pct_flat",
			input: PriceComponents{
				Channel:              ChannelFacebook,
				SellingPriceAUDCents: 7500,
				CostCNYCents:         5000,
				ShippingEstAUDCents:  400,
			},
			// fee = round(7500 * 0.05) = 375
			// cost_aud = round(5000 * 0.21) = 1050
			// net = 7500 - 1050 - 375 - 400 = 5675
			// margin = 5675 / 7500 = 0.756666...
			wantMarginPct: 0.7567,
			wantFeeCents:  375,
			wantNetCents:  5675,
		},
		{
			name: "E_woocommerce_low_margin",
			input: PriceComponents{
				Channel:              ChannelWooCommerce,
				SellingPriceAUDCents: 3000,
				CostCNYCents:         10000,
				ShippingEstAUDCents:  200,
			},
			// fee = round(3000 * 0.0175) + 30 = 53 + 30 = 83
			// cost_aud = round(10000 * 0.21) = 2100
			// net = 3000 - 2100 - 83 - 200 = 617
			// margin = 617 / 3000 = 0.205666...
			wantMarginPct: 0.2057,
			wantFeeCents:  83,
			wantNetCents:  617,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := calc.CalculateMargin(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("%s: CalculateMargin: %v", tc.name, err)
			}
			if got.PlatformFeeAUDCents != tc.wantFeeCents {
				t.Fatalf("%s: fee = %d, want %d", tc.name, got.PlatformFeeAUDCents, tc.wantFeeCents)
			}
			if got.NetAUDCents != tc.wantNetCents {
				t.Fatalf("%s: net = %d, want %d", tc.name, got.NetAUDCents, tc.wantNetCents)
			}
			approxEqualPct(t, tc.name, tc.wantMarginPct, got.GrossMarginPct, 0.0001)
			if got.Channel != tc.input.Channel {
				t.Fatalf("%s: channel passthrough = %s, want %s", tc.name, got.Channel, tc.input.Channel)
			}
		})
	}
}

func TestPlatformFee_FXRateStalenessFires(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	stale := rateAt(now.Add(-25 * time.Hour))
	calc, err := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{
		FXProvider: staticFXProvider{rate: stale},
		Now:        fixedNow(now),
	})
	if err != nil {
		t.Fatalf("NewPlatformFeeCalculator: %v", err)
	}
	_, err = calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 10000,
		CostCNYCents:         4000,
		ShippingEstAUDCents:  500,
	})
	if !errors.Is(err, ErrFXRateStale) {
		t.Fatalf("CalculateMargin staleness: got err=%v, want ErrFXRateStale", err)
	}
}

func TestPlatformFee_FXRateFreshAtBoundary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	// 23h59m ago: still fresh (boundary).
	fresh := rateAt(now.Add(-23*time.Hour - 59*time.Minute))
	calc, err := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{
		FXProvider: staticFXProvider{rate: fresh},
		Now:        fixedNow(now),
	})
	if err != nil {
		t.Fatalf("NewPlatformFeeCalculator: %v", err)
	}
	res, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 10000,
		CostCNYCents:         4000,
		ShippingEstAUDCents:  500,
	})
	if err != nil {
		t.Fatalf("CalculateMargin boundary: %v", err)
	}
	if res.NetAUDCents != 8455 {
		t.Fatalf("CalculateMargin boundary net = %d, want 8455", res.NetAUDCents)
	}
}

func TestPlatformFee_PerChannelFeeStructure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		channel    Channel
		sellCents  int
		commission float64
		wantFee    int
		wantErr    error
	}{
		{name: "tiktok_default_5pct", channel: ChannelTikTok, sellCents: 10000, wantFee: 500},
		{name: "tiktok_min_2pct", channel: ChannelTikTok, sellCents: 10000, commission: 0.02, wantFee: 200},
		{name: "tiktok_above_max_capped", channel: ChannelTikTok, sellCents: 10000, commission: 0.10, wantFee: 500},
		{name: "tiktok_below_min_capped", channel: ChannelTikTok, sellCents: 10000, commission: 0.005, wantFee: 200},
		{name: "facebook_flat_5pct", channel: ChannelFacebook, sellCents: 8000, wantFee: 400},
		{name: "woocommerce_stripe", channel: ChannelWooCommerce, sellCents: 10000, wantFee: 205},
		{name: "woocommerce_small", channel: ChannelWooCommerce, sellCents: 1000, wantFee: 48},
		{name: "rednote_unsupported", channel: ChannelRedNote, sellCents: 10000, wantErr: ErrUnsupportedChannel},
		{name: "unknown_channel", channel: Channel("instagram"), sellCents: 10000, wantErr: ErrUnsupportedChannel},
	}
	calc, err := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{
		FXProvider: staticFXProvider{rate: rateAt(time.Now().UTC())},
	})
	if err != nil {
		t.Fatalf("NewPlatformFeeCalculator: %v", err)
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fee, err := calc.PlatformFee(PriceComponents{
				Channel:              tc.channel,
				SellingPriceAUDCents: tc.sellCents,
				TikTokCommissionPct:  tc.commission,
			})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("%s: err=%v, want %v", tc.name, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected err: %v", tc.name, err)
			}
			if fee != tc.wantFee {
				t.Fatalf("%s: fee = %d, want %d", tc.name, fee, tc.wantFee)
			}
		})
	}
}

func TestPlatformFee_InvalidComponentsRejected(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	calc, err := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{
		FXProvider: staticFXProvider{rate: rateAt(now)},
		Now:        fixedNow(now),
	})
	if err != nil {
		t.Fatalf("NewPlatformFeeCalculator: %v", err)
	}
	cases := []struct {
		name  string
		input PriceComponents
	}{
		{name: "empty_channel", input: PriceComponents{SellingPriceAUDCents: 100}},
		{name: "zero_selling_price", input: PriceComponents{Channel: ChannelWooCommerce}},
		{name: "negative_selling_price", input: PriceComponents{Channel: ChannelWooCommerce, SellingPriceAUDCents: -10}},
		{name: "negative_cost", input: PriceComponents{Channel: ChannelWooCommerce, SellingPriceAUDCents: 100, CostCNYCents: -5}},
		{name: "negative_shipping", input: PriceComponents{Channel: ChannelWooCommerce, SellingPriceAUDCents: 100, ShippingEstAUDCents: -1}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := calc.CalculateMargin(context.Background(), tc.input)
			if !errors.Is(err, ErrInvalidPriceComponents) {
				t.Fatalf("%s: err=%v, want ErrInvalidPriceComponents", tc.name, err)
			}
		})
	}
}

func TestPlatformFee_ProviderFailureSurfaces(t *testing.T) {
	t.Parallel()
	calc, err := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{
		FXProvider: staticFXProvider{err: errors.New("provider down")},
	})
	if err != nil {
		t.Fatalf("NewPlatformFeeCalculator: %v", err)
	}
	_, err = calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 10000,
		CostCNYCents:         4000,
	})
	if err == nil {
		t.Fatal("CalculateMargin: want non-nil err for provider failure")
	}
	if errors.Is(err, ErrFXRateStale) {
		t.Fatalf("CalculateMargin: provider failure should NOT be ErrFXRateStale, got %v", err)
	}
}

func TestPlatformFee_ConstructorRejectsMissingProvider(t *testing.T) {
	t.Parallel()
	_, err := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{})
	if !errors.Is(err, ErrFXRateUnconfigured) {
		t.Fatalf("NewPlatformFeeCalculator: err=%v, want ErrFXRateUnconfigured", err)
	}
}

func TestPlatformFee_NegativeNetClampedNotZero(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	calc, err := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{
		FXProvider: staticFXProvider{rate: rateAt(now)},
		Now:        fixedNow(now),
	})
	if err != nil {
		t.Fatalf("NewPlatformFeeCalculator: %v", err)
	}
	// cost > sell: net should be allowed to go negative (operator
	// dashboard surfaces the loss).
	res, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 1000,
		CostCNYCents:         10000,
		ShippingEstAUDCents:  100,
	})
	if err != nil {
		t.Fatalf("CalculateMargin negative: %v", err)
	}
	if res.NetAUDCents >= 0 {
		t.Fatalf("CalculateMargin negative: net = %d, want negative (loss surfaced)", res.NetAUDCents)
	}
	if res.GrossMarginPct >= 0 {
		t.Fatalf("CalculateMargin negative: pct = %.4f, want < 0 (loss surfaced)", res.GrossMarginPct)
	}
}

func TestFXRate_StalenessHelpers(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		rate      FXRate
		max       time.Duration
		wantStale bool
	}{
		{name: "fresh_1h", rate: rateAt(now.Add(-time.Hour)), max: 24 * time.Hour, wantStale: false},
		{name: "boundary_24h_minus_1s", rate: rateAt(now.Add(-24*time.Hour + time.Second)), max: 24 * time.Hour, wantStale: false},
		{name: "stale_25h", rate: rateAt(now.Add(-25 * time.Hour)), max: 24 * time.Hour, wantStale: true},
		{name: "zero_fetched_at_stale", rate: FXRate{AUDPerCNY: 0.21}, max: 24 * time.Hour, wantStale: true},
		{name: "default_max_used", rate: rateAt(now.Add(-25 * time.Hour)), max: 0, wantStale: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.rate.IsStale(now, tc.max); got != tc.wantStale {
				t.Fatalf("%s: IsStale = %v, want %v", tc.name, got, tc.wantStale)
			}
		})
	}
}
