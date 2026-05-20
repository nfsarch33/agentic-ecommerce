package ml_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/ml"
)

func TestInventory_DemandForecastLinearTrend(t *testing.T) {
	t.Parallel()
	history := []ml.SalesRecord{
		{Day: 1, Units: 10},
		{Day: 2, Units: 20},
		{Day: 3, Units: 30},
	}
	pts := ml.DemandForecast(history, 2)
	if len(pts) != 2 {
		t.Fatalf("expected 2 forecast points, got %d", len(pts))
	}
	// Linear trend: slope=10, intercept=0 => day4=40, day5=50
	if pts[0].Units < 35 || pts[0].Units > 45 {
		t.Fatalf("expected ~40 for day4, got %f", pts[0].Units)
	}
}

func TestInventory_DemandForecastEmptyHistoryReturnsZeroes(t *testing.T) {
	t.Parallel()
	pts := ml.DemandForecast(nil, 3)
	if len(pts) != 3 {
		t.Fatalf("expected 3 points for empty history, got %d", len(pts))
	}
	for _, p := range pts {
		if p.Units != 0 {
			t.Fatalf("expected 0 units for empty history, got %f", p.Units)
		}
	}
}

func TestInventory_DemandForecastNegativeClamped(t *testing.T) {
	t.Parallel()
	// Declining trend that would go negative
	history := []ml.SalesRecord{
		{Day: 1, Units: 100},
		{Day: 2, Units: 50},
		{Day: 3, Units: 10},
	}
	pts := ml.DemandForecast(history, 5)
	for _, p := range pts {
		if p.Units < 0 {
			t.Fatalf("expected non-negative forecast, got %f on day %d", p.Units, p.Day)
		}
	}
}

func TestInventory_SafetyStockIncreasesWithServiceLevel(t *testing.T) {
	t.Parallel()
	forecast := []ml.ForecastPoint{
		{Day: 1, Units: 50},
		{Day: 2, Units: 200},
		{Day: 3, Units: 30},
		{Day: 4, Units: 180},
		{Day: 5, Units: 10},
		{Day: 6, Units: 250},
	}
	s90 := ml.SafetyStock(forecast, 0.90)
	s95 := ml.SafetyStock(forecast, 0.95)
	s99 := ml.SafetyStock(forecast, 0.99)
	if s90 >= s95 {
		t.Fatalf("expected s90 < s95, got %d >= %d", s90, s95)
	}
	if s95 >= s99 {
		t.Fatalf("expected s95 < s99, got %d >= %d", s95, s99)
	}
}

func TestInventory_SafetyStockEmptyForecast(t *testing.T) {
	t.Parallel()
	ss := ml.SafetyStock(nil, 0.95)
	if ss != 0 {
		t.Fatalf("expected 0 for empty forecast, got %d", ss)
	}
}

func TestInventory_ReorderPointCalculation(t *testing.T) {
	t.Parallel()
	// dailyDemand=10, leadTime=5, safetyStock=20 => 10*5+20=70
	rp := ml.ReorderPoint(10, 5, 20)
	if rp != 70 {
		t.Fatalf("expected reorder point 70, got %d", rp)
	}
}

func TestInventory_ReorderPointLeadTimeZero(t *testing.T) {
	t.Parallel()
	rp := ml.ReorderPoint(100, 0, 50)
	if rp != 50 {
		t.Fatalf("expected reorder point 50 with lead time 0, got %d", rp)
	}
}

func TestInventory_SeasonalAdjustMultipliesFactors(t *testing.T) {
	t.Parallel()
	forecast := []ml.ForecastPoint{
		{Day: 1, Units: 100},
		{Day: 2, Units: 100},
		{Day: 3, Units: 100},
		{Day: 4, Units: 100},
	}
	factors := []float64{1.2, 0.8} // alternating high/low
	adj := ml.SeasonalAdjust(forecast, factors)
	if len(adj) != 4 {
		t.Fatalf("expected 4 adjusted points, got %d", len(adj))
	}
	// Day 0 (index 0): factor 1.2 => 120
	if adj[0].Units < 119 || adj[0].Units > 121 {
		t.Fatalf("expected ~120, got %f", adj[0].Units)
	}
	// Day 1 (index 1): factor 0.8 => 80
	if adj[1].Units < 79 || adj[1].Units > 81 {
		t.Fatalf("expected ~80, got %f", adj[1].Units)
	}
}

func TestInventory_SeasonalAdjustEmptyFactorsReturnsForecast(t *testing.T) {
	t.Parallel()
	forecast := []ml.ForecastPoint{{Day: 1, Units: 50}}
	adj := ml.SeasonalAdjust(forecast, nil)
	if len(adj) != 1 || adj[0].Units != 50 {
		t.Fatalf("expected unchanged forecast with empty factors")
	}
}
