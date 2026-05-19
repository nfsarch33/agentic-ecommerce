package pricing_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/domain/pricing"
)

func TestDynamic_HighDemandIncreasesFactor(t *testing.T) {
	t.Parallel()
	factor := pricing.DemandFactor(100, 20, time.Hour) // 20% conversion
	if factor <= 1.0 {
		t.Fatalf("expected factor > 1.0, got %.2f", factor)
	}
}

func TestDynamic_LowDemandDecreasesFactor(t *testing.T) {
	t.Parallel()
	factor := pricing.DemandFactor(100, 0, time.Hour) // 0% conversion
	if factor >= 1.0 {
		t.Fatalf("expected factor < 1.0, got %.2f", factor)
	}
}

func TestDynamic_CompetitorUndercutAdjustment(t *testing.T) {
	t.Parallel()
	adjusted := pricing.CompetitorAdjust(1000, []int{900, 950, 1100})
	// Should be 5% below 900 = 855
	if adjusted >= 900 {
		t.Fatalf("expected price below competitor min, got %d", adjusted)
	}
}

func TestDynamic_FloorEnforced(t *testing.T) {
	t.Parallel()
	result := pricing.FloorCeiling(500, 800, 2000)
	if result != 800 {
		t.Fatalf("expected floor 800, got %d", result)
	}
}

func TestDynamic_CeilingEnforced(t *testing.T) {
	t.Parallel()
	result := pricing.FloorCeiling(3000, 500, 2000)
	if result != 2000 {
		t.Fatalf("expected ceiling 2000, got %d", result)
	}
}

func TestDynamic_TimeDecayReducesOverTime(t *testing.T) {
	t.Parallel()
	original := 1000
	decayed := pricing.TimeDecay(original, 24*time.Hour, 12*time.Hour) // 2 half-lives
	if decayed >= original {
		t.Fatalf("expected decayed price < %d, got %d", original, decayed)
	}
}

func TestDynamic_ZeroViewsReturnsFactor1(t *testing.T) {
	t.Parallel()
	factor := pricing.DemandFactor(0, 0, time.Hour)
	if factor != 1.0 {
		t.Fatalf("expected 1.0 for zero views, got %.2f", factor)
	}
}
