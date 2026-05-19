package bundlepricing_test

import (
	"math"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/bundlepricing"
)

const epsilon = 1e-9

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestCalculator_BundleWithDiscount(t *testing.T) {
	t.Parallel()
	c := bundlepricing.NewCalculator()
	b := bundlepricing.Bundle{
		ID:          "b1",
		Name:        "Starter Pack",
		DiscountPct: 20,
		Products: []bundlepricing.BundleItem{
			{ProductID: "p1", Quantity: 2},
			{ProductID: "p2", Quantity: 1},
		},
	}
	prices := map[string]float64{"p1": 10.00, "p2": 5.00}
	got := c.Calculate(b, prices)

	if !almostEqual(got.Subtotal, 25.00) {
		t.Errorf("Subtotal = %.4f, want 25.00", got.Subtotal)
	}
	if !almostEqual(got.Discount, 5.00) {
		t.Errorf("Discount = %.4f, want 5.00", got.Discount)
	}
	if !almostEqual(got.Total, 20.00) {
		t.Errorf("Total = %.4f, want 20.00", got.Total)
	}
}

func TestCalculator_ZeroDiscount(t *testing.T) {
	t.Parallel()
	c := bundlepricing.NewCalculator()
	b := bundlepricing.Bundle{
		DiscountPct: 0,
		Products:    []bundlepricing.BundleItem{{ProductID: "p1", Quantity: 3}},
	}
	prices := map[string]float64{"p1": 10.00}
	got := c.Calculate(b, prices)
	if !almostEqual(got.Total, 30.00) {
		t.Errorf("Total = %.4f, want 30.00", got.Total)
	}
	if got.Discount != 0 {
		t.Errorf("Discount = %.4f, want 0", got.Discount)
	}
}

func TestCalculator_EmptyBundle(t *testing.T) {
	t.Parallel()
	c := bundlepricing.NewCalculator()
	got := c.Calculate(bundlepricing.Bundle{}, nil)
	if got.Subtotal != 0 || got.Total != 0 {
		t.Errorf("empty bundle: Subtotal=%.2f Total=%.2f, want both 0", got.Subtotal, got.Total)
	}
}

func TestDiscountStack_Compound(t *testing.T) {
	t.Parallel()
	// 10% then 5%: 100 -> 90 -> 85.5 (effective 14.5% off)
	ds := bundlepricing.NewDiscountStack()
	ds.Add(10)
	ds.Add(5)
	got := ds.Apply(100.00)
	if !almostEqual(got, 85.50) {
		t.Errorf("Apply = %.4f, want 85.50", got)
	}
}

func TestDiscountStack_SingleDiscount(t *testing.T) {
	t.Parallel()
	ds := bundlepricing.NewDiscountStack()
	ds.Add(25)
	got := ds.Apply(200.00)
	if !almostEqual(got, 150.00) {
		t.Errorf("Apply = %.4f, want 150.00", got)
	}
}

func TestDiscountStack_Empty(t *testing.T) {
	t.Parallel()
	ds := bundlepricing.NewDiscountStack()
	got := ds.Apply(99.99)
	if !almostEqual(got, 99.99) {
		t.Errorf("Apply(empty) = %.4f, want 99.99", got)
	}
}

func TestDiscountStack_ZeroDiscount(t *testing.T) {
	t.Parallel()
	ds := bundlepricing.NewDiscountStack()
	ds.Add(0)
	got := ds.Apply(50.00)
	if !almostEqual(got, 50.00) {
		t.Errorf("Apply(0%%) = %.4f, want 50.00", got)
	}
}

func TestDiscountStack_EffectiveRate(t *testing.T) {
	t.Parallel()
	// Verify compound is NOT the same as adding discounts linearly.
	// 10% + 5% linear = 15% off => 85.00
	// 10% then 5% compound => 85.50 (already covered above, sanity check)
	ds := bundlepricing.NewDiscountStack()
	ds.Add(10)
	ds.Add(5)
	compound := ds.Apply(100.00)
	linear := 100.00 * (1 - 0.15)
	if almostEqual(compound, linear) {
		t.Error("compound discount should not equal linear discount")
	}
}
