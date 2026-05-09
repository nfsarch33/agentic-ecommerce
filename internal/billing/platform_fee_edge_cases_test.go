// File scope: v3.5.1 QA Task 4 -- platform fee + margin formula
// edge-case validation (EC-6-2 hardening).
//
// 8 scenarios beyond the 5-scenario margin baseline shipped in
// v3.5.0:
//
//  1. Zero cost (free sample) -- margin = 100% - fee_pct
//  2. Negative margin (selling below cost) -- as-is + log gate
//     (caller decides; no typed-error promotion in v3.5.0)
//  3. FX rate exactly 24h old -- boundary test; NOT stale yet
//  4. FX rate 24h+1s old -- ErrFXRateStale fires
//  5. FX rate exactly 0 -- ErrInvalidPriceComponents (the v3.5.0
//     shipped surface uses ErrInvalidPriceComponents for the FX
//     non-positive guard; the task spec called it ErrInvalidFXRate
//     for clarity but the shipped sentinel is the broader one)
//  6. Selling price exactly 0 -- ErrInvalidPriceComponents
//  7. Multiple decimal precision (¥39.99 cost / A$99.95 sell /
//     4.50% fee) -- rounding consistency
//  8. Negative shipping cost -- ErrInvalidPriceComponents
//
// Each scenario asserts a specific typed error OR a specific
// margin output. Hardening only -- no production code changes.
package billing

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

// edgeFixedNow is the canonical clock for every edge case so the
// staleness assertions are deterministic.
var edgeFixedNow = time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)

// edgeRate builds an FXRate at the supplied timestamp with the
// canonical 0.21 AUD/CNY rate (matches the v3.5.0 baseline).
func edgeRate(ts time.Time) FXRate {
	return FXRate{AUDPerCNY: 0.21, FetchedAt: ts, Source: "v351-edge-test"}
}

// edgeCalculator wires a calculator pinned to edgeFixedNow with
// the supplied rate (or staticFXProvider with err set when
// rateErr != nil).
func edgeCalculator(t *testing.T, rate FXRate) *PlatformFeeCalculator {
	t.Helper()
	calc, err := NewPlatformFeeCalculator(PlatformFeeCalculatorConfig{
		FXProvider: staticFXProvider{rate: rate},
		Now:        fixedNow(edgeFixedNow),
	})
	if err != nil {
		t.Fatalf("NewPlatformFeeCalculator: %v", err)
	}
	return calc
}

// TestPlatformFeeEdgeCase1_ZeroCostFreeSample verifies that a
// CostCNYCents=0 input (e.g. a free sample / referral promo) lands
// at margin = 1 - fee_pct. TikTok at the default 5% fee leaves
// 95% net margin.
func TestPlatformFeeEdgeCase1_ZeroCostFreeSample(t *testing.T) {
	t.Parallel()
	calc := edgeCalculator(t, edgeRate(edgeFixedNow.Add(-1*time.Hour)))
	res, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelTikTok,
		SellingPriceAUDCents: 10000,
		CostCNYCents:         0,
		ShippingEstAUDCents:  0,
	})
	if err != nil {
		t.Fatalf("CalculateMargin zero-cost: %v", err)
	}
	if res.CostAUDCents != 0 {
		t.Fatalf("CostAUDCents = %d, want 0", res.CostAUDCents)
	}
	if res.PlatformFeeAUDCents != 500 {
		t.Fatalf("PlatformFee = %d, want 500 (5%% of 10000)", res.PlatformFeeAUDCents)
	}
	if res.NetAUDCents != 9500 {
		t.Fatalf("Net = %d, want 9500 (10000 - 0 - 500 - 0)", res.NetAUDCents)
	}
	if math.Abs(res.GrossMarginPct-0.95) > 0.0001 {
		t.Fatalf("Margin = %.4f, want 0.9500 (= 1 - 0.05 fee)", res.GrossMarginPct)
	}
}

// TestPlatformFeeEdgeCase2_NegativeMarginSurfacedNotErrored
// verifies the v3.5.0 shipped contract: selling below cost
// produces a NEGATIVE margin (net < 0, pct < 0), NOT a typed
// error. Pricing agent decides downstream (per the v3.5.1 plan
// "return as-is, log warning, let pricing agent decide").
func TestPlatformFeeEdgeCase2_NegativeMarginSurfacedNotErrored(t *testing.T) {
	t.Parallel()
	calc := edgeCalculator(t, edgeRate(edgeFixedNow.Add(-1*time.Hour)))
	// Selling A$10.00 with cost ¥100 -> A$21 cost (210 cents)
	// Net = 1000 - 210 - 50 - 0 = 740 (positive). Stress harder:
	// Selling A$5.00 with cost ¥500 -> A$105 cost (10500 cents).
	// Net = 500 - 10500 - 25 - 0 = -10025 (clearly negative).
	res, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 500,
		CostCNYCents:         50000,
		ShippingEstAUDCents:  0,
	})
	if err != nil {
		t.Fatalf("CalculateMargin negative-margin: %v", err)
	}
	if res.NetAUDCents >= 0 {
		t.Fatalf("Net = %d, want negative (loss surfaced)", res.NetAUDCents)
	}
	if res.GrossMarginPct >= 0 {
		t.Fatalf("Margin pct = %.4f, want negative (loss surfaced)", res.GrossMarginPct)
	}
}

// TestPlatformFeeEdgeCase3_FXRateExactly24hOldNotStale verifies
// the boundary: FetchedAt = now - 24h is the inclusive freshness
// boundary. ErrFXRateStale should NOT fire (the production gate
// uses `> max`, exclusive).
func TestPlatformFeeEdgeCase3_FXRateExactly24hOldNotStale(t *testing.T) {
	t.Parallel()
	rate := edgeRate(edgeFixedNow.Add(-24 * time.Hour))
	calc := edgeCalculator(t, rate)
	res, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 10000,
		CostCNYCents:         4000,
		ShippingEstAUDCents:  500,
	})
	if err != nil {
		t.Fatalf("CalculateMargin boundary 24h: got err=%v, want fresh", err)
	}
	if res.NetAUDCents != 8455 {
		t.Fatalf("Net = %d, want 8455 (boundary FX still applies)", res.NetAUDCents)
	}
}

// TestPlatformFeeEdgeCase4_FXRateOneSecondOver24hStale verifies
// the boundary fires ErrFXRateStale at 24h+1s.
func TestPlatformFeeEdgeCase4_FXRateOneSecondOver24hStale(t *testing.T) {
	t.Parallel()
	rate := edgeRate(edgeFixedNow.Add(-24*time.Hour - time.Second))
	calc := edgeCalculator(t, rate)
	_, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 10000,
		CostCNYCents:         4000,
		ShippingEstAUDCents:  500,
	})
	if !errors.Is(err, ErrFXRateStale) {
		t.Fatalf("CalculateMargin 24h+1s: err=%v, want ErrFXRateStale", err)
	}
}

// TestPlatformFeeEdgeCase5_FXRateZeroRejected verifies the FX
// non-positive guard. The v3.5.0 shipped surface returns
// ErrInvalidPriceComponents (the broader sentinel; the task spec
// called it ErrInvalidFXRate but the shipped code routes through
// the price-components guard).
func TestPlatformFeeEdgeCase5_FXRateZeroRejected(t *testing.T) {
	t.Parallel()
	rate := FXRate{AUDPerCNY: 0, FetchedAt: edgeFixedNow.Add(-1 * time.Hour), Source: "v351-edge-test"}
	calc := edgeCalculator(t, rate)
	_, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 10000,
		CostCNYCents:         4000,
		ShippingEstAUDCents:  500,
	})
	if !errors.Is(err, ErrInvalidPriceComponents) {
		t.Fatalf("CalculateMargin fx=0: err=%v, want ErrInvalidPriceComponents (v3.5.0 shipped sentinel for fx non-positive)", err)
	}
}

// TestPlatformFeeEdgeCase6_SellingPriceExactlyZeroRejected
// verifies the validate gate rejects SellingPriceAUDCents=0
// before any FX provider call fires.
func TestPlatformFeeEdgeCase6_SellingPriceExactlyZeroRejected(t *testing.T) {
	t.Parallel()
	calc := edgeCalculator(t, edgeRate(edgeFixedNow.Add(-1*time.Hour)))
	_, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 0,
		CostCNYCents:         4000,
	})
	if !errors.Is(err, ErrInvalidPriceComponents) {
		t.Fatalf("CalculateMargin sell=0: err=%v, want ErrInvalidPriceComponents", err)
	}
}

// TestPlatformFeeEdgeCase7_DecimalPrecisionRoundingConsistent
// verifies that the integer-cents arithmetic stays bit-stable
// across reasonable decimal inputs.
//
// Inputs (cents): cost ¥39.99 = 3999, sell A$99.95 = 9995,
// TikTok 4.5% commission, ship A$0 = 0.
//   - fee = round(9995 * 0.045) = round(449.775) = 450
//   - cost_aud = round(3999 * 0.21) = round(839.79) = 840
//   - net = 9995 - 840 - 450 - 0 = 8705
//   - margin = 8705 / 9995 = 0.87093547... -> round4 = 0.8709
func TestPlatformFeeEdgeCase7_DecimalPrecisionRoundingConsistent(t *testing.T) {
	t.Parallel()
	calc := edgeCalculator(t, edgeRate(edgeFixedNow.Add(-1*time.Hour)))
	res, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelTikTok,
		SellingPriceAUDCents: 9995,
		CostCNYCents:         3999,
		ShippingEstAUDCents:  0,
		TikTokCommissionPct:  0.045,
	})
	if err != nil {
		t.Fatalf("CalculateMargin decimal: %v", err)
	}
	if res.PlatformFeeAUDCents != 450 {
		t.Fatalf("Fee = %d, want 450 (round(9995 * 0.045))", res.PlatformFeeAUDCents)
	}
	if res.CostAUDCents != 840 {
		t.Fatalf("CostAUD = %d, want 840 (round(3999 * 0.21))", res.CostAUDCents)
	}
	if res.NetAUDCents != 8705 {
		t.Fatalf("Net = %d, want 8705", res.NetAUDCents)
	}
	if math.Abs(res.GrossMarginPct-0.8709) > 0.0001 {
		t.Fatalf("Margin = %.4f, want 0.8709", res.GrossMarginPct)
	}
	// Idempotency: a second invocation MUST produce the exact
	// same values (rounding determinism).
	res2, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelTikTok,
		SellingPriceAUDCents: 9995,
		CostCNYCents:         3999,
		ShippingEstAUDCents:  0,
		TikTokCommissionPct:  0.045,
	})
	if err != nil {
		t.Fatalf("CalculateMargin decimal-repeat: %v", err)
	}
	if res != res2 {
		t.Fatalf("rounding non-deterministic: %+v vs %+v", res, res2)
	}
}

// TestPlatformFeeEdgeCase8_NegativeShippingRejected verifies
// shipping refund / credit shapes are rejected at the validate
// gate (the v3.5.0 shipped contract treats shipping as a non-
// negative cost).
func TestPlatformFeeEdgeCase8_NegativeShippingRejected(t *testing.T) {
	t.Parallel()
	calc := edgeCalculator(t, edgeRate(edgeFixedNow.Add(-1*time.Hour)))
	_, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 10000,
		CostCNYCents:         4000,
		ShippingEstAUDCents:  -1,
	})
	if !errors.Is(err, ErrInvalidPriceComponents) {
		t.Fatalf("CalculateMargin negative-shipping: err=%v, want ErrInvalidPriceComponents", err)
	}
}
