// File scope: v6.1.0 CF-17 -- ErrInvalidFXRate typed sentinel.
//
// Carry-forward CF-17 (35-sprint debt) called out that the v3.5.0
// shipped surface conflated two distinct invariants under a single
// sentinel:
//
//   - PriceComponents shape errors (negative cost, zero sell, etc.)
//   - FX rate non-positive (rate.AUDPerCNY <= 0)
//
// The former is caller-supplied input; the latter is provider
// pollution. They have different ownership and different remediation
// (caller fix vs. provider fix), so v6.1.0 extracts ErrInvalidFXRate
// as its own typed sentinel.
//
// These tests pin the new contract via TDD: the calculator's
// resolveFX and the billing.WriteFXRateCacheFile helper must
// surface ErrInvalidFXRate on rate non-positive AND must continue
// to be distinguishable from ErrInvalidPriceComponents.
package billing

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestCF17_FXProviderReturnsZeroRate_SurfacesErrInvalidFXRate
// verifies the new typed sentinel for rate non-positive.
func TestCF17_FXProviderReturnsZeroRate_SurfacesErrInvalidFXRate(t *testing.T) {
	t.Parallel()
	rate := FXRate{AUDPerCNY: 0, FetchedAt: edgeFixedNow.Add(-1 * time.Hour), Source: "cf17"}
	calc := edgeCalculator(t, rate)
	_, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelWooCommerce,
		SellingPriceAUDCents: 10000,
		CostCNYCents:         4000,
	})
	if !errors.Is(err, ErrInvalidFXRate) {
		t.Fatalf("CalculateMargin fx=0: err=%v, want ErrInvalidFXRate", err)
	}
	if errors.Is(err, ErrInvalidPriceComponents) {
		t.Fatalf("CalculateMargin fx=0: err=%v should NOT alias ErrInvalidPriceComponents (CF-17 separates FX from PC errors)", err)
	}
}

// TestCF17_FXProviderReturnsNegativeRate_SurfacesErrInvalidFXRate
// covers the other half of the rate.AUDPerCNY <= 0 guard.
func TestCF17_FXProviderReturnsNegativeRate_SurfacesErrInvalidFXRate(t *testing.T) {
	t.Parallel()
	rate := FXRate{AUDPerCNY: -0.21, FetchedAt: edgeFixedNow.Add(-1 * time.Hour), Source: "cf17"}
	calc := edgeCalculator(t, rate)
	_, err := calc.CalculateMargin(context.Background(), PriceComponents{
		Channel:              ChannelTikTok,
		SellingPriceAUDCents: 10000,
		CostCNYCents:         4000,
	})
	if !errors.Is(err, ErrInvalidFXRate) {
		t.Fatalf("CalculateMargin fx<0: err=%v, want ErrInvalidFXRate", err)
	}
}

// TestCF17_PriceComponentsErrorStaysOnPriceComponentsSentinel
// pins the boundary: a malformed PriceComponents input must still
// surface ErrInvalidPriceComponents and must NOT alias the new
// ErrInvalidFXRate sentinel.
func TestCF17_PriceComponentsErrorStaysOnPriceComponentsSentinel(t *testing.T) {
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
	if errors.Is(err, ErrInvalidFXRate) {
		t.Fatalf("CalculateMargin sell=0: err=%v should NOT alias ErrInvalidFXRate (CF-17 separation)", err)
	}
}

// TestCF17_WriteFXRateCacheFile_NonPositiveRate_SurfacesErrInvalidFXRate
// pins the helper-side behavior: the on-disk cache writer routes
// rate <= 0 through the new FX sentinel.
func TestCF17_WriteFXRateCacheFile_NonPositiveRate_SurfacesErrInvalidFXRate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "fx.json")
	err := WriteFXRateCacheFile(path, FXRate{AUDPerCNY: 0})
	if !errors.Is(err, ErrInvalidFXRate) {
		t.Fatalf("WriteFXRateCacheFile rate=0: err=%v, want ErrInvalidFXRate", err)
	}
	if errors.Is(err, ErrInvalidPriceComponents) {
		t.Fatalf("WriteFXRateCacheFile rate=0: err=%v should NOT alias ErrInvalidPriceComponents (CF-17 separation)", err)
	}
}

// TestCF17_ErrInvalidFXRateMessageContainsBillingPrefix pins the
// error text so operator logs match the v6.1.0 documented surface.
func TestCF17_ErrInvalidFXRateMessageContainsBillingPrefix(t *testing.T) {
	t.Parallel()
	if ErrInvalidFXRate == nil {
		t.Fatal("ErrInvalidFXRate must be exported and non-nil")
	}
	if got := ErrInvalidFXRate.Error(); got == "" {
		t.Fatalf("ErrInvalidFXRate.Error() is empty")
	}
	if !contains(ErrInvalidFXRate.Error(), "billing:") {
		t.Fatalf("ErrInvalidFXRate.Error() = %q, want billing: prefix", ErrInvalidFXRate.Error())
	}
	if !contains(ErrInvalidFXRate.Error(), "fx rate") {
		t.Fatalf("ErrInvalidFXRate.Error() = %q, want 'fx rate' phrase", ErrInvalidFXRate.Error())
	}
}

// contains is a tiny local helper to keep these tests free of the
// strings.Contains import where the rest of the file does not need
// it.
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
