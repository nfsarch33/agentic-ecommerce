package ml_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/ml"
)

func TestRevenue_FitAndPredict(t *testing.T) {
	t.Parallel()
	history := []ml.RevenuePoint{
		{Period: 1, Revenue: 100},
		{Period: 2, Revenue: 200},
		{Period: 3, Revenue: 300},
	}
	var m ml.TimeSeriesModel
	m.Fit(history)
	pred := m.Predict(4)
	if pred < 380 || pred > 420 {
		t.Fatalf("expected ~400 for period 4, got %f", pred)
	}
}

func TestRevenue_FitEmptyHistory(t *testing.T) {
	t.Parallel()
	var m ml.TimeSeriesModel
	m.Fit(nil)
	pred := m.Predict(1)
	if pred != 0 {
		t.Fatalf("expected 0 prediction for empty history, got %f", pred)
	}
}

func TestRevenue_PredictNegativeClamped(t *testing.T) {
	t.Parallel()
	// Declining revenue that goes negative
	history := []ml.RevenuePoint{
		{Period: 1, Revenue: 300},
		{Period: 2, Revenue: 200},
		{Period: 3, Revenue: 100},
	}
	var m ml.TimeSeriesModel
	m.Fit(history)
	pred := m.Predict(10)
	if pred < 0 {
		t.Fatalf("expected non-negative prediction, got %f", pred)
	}
}

func TestRevenue_ConfidenceIntervalBounds(t *testing.T) {
	t.Parallel()
	// noisy data ensures non-zero residual std error
	history := []ml.RevenuePoint{
		{Period: 1, Revenue: 100},
		{Period: 2, Revenue: 130},
		{Period: 3, Revenue: 90},
		{Period: 4, Revenue: 160},
		{Period: 5, Revenue: 110},
	}
	var m ml.TimeSeriesModel
	m.Fit(history)
	fc := ml.ConfidenceInterval(m, 6, 1.96)
	if fc.Period != 6 {
		t.Fatalf("expected period 6, got %d", fc.Period)
	}
	if m.StdErr == 0 {
		t.Fatal("expected non-zero StdErr from noisy data")
	}
	if fc.LowerBound >= fc.Predicted {
		t.Fatalf("expected lower < predicted, got %f >= %f", fc.LowerBound, fc.Predicted)
	}
	if fc.UpperBound <= fc.Predicted {
		t.Fatalf("expected upper > predicted, got %f <= %f", fc.UpperBound, fc.Predicted)
	}
}

func TestRevenue_SeasonalDecomp(t *testing.T) {
	t.Parallel()
	history := []ml.RevenuePoint{
		{Period: 1, Revenue: 100},
		{Period: 2, Revenue: 150},
		{Period: 3, Revenue: 100},
		{Period: 4, Revenue: 150},
	}
	trend, seasonal, _ := ml.SeasonalDecomp(history, 2)
	if len(trend) != 4 {
		t.Fatalf("expected 4 trend values, got %d", len(trend))
	}
	if len(seasonal) != 4 {
		t.Fatalf("expected 4 seasonal values, got %d", len(seasonal))
	}
}

func TestRevenue_SeasonalDecompEmptyReturnsNil(t *testing.T) {
	t.Parallel()
	trend, seasonal, residual := ml.SeasonalDecomp(nil, 12)
	if trend != nil || seasonal != nil || residual != nil {
		t.Fatal("expected nil slices for empty history")
	}
}

func TestRevenue_ScenarioModelAppliesGrowth(t *testing.T) {
	t.Parallel()
	base := []ml.RevenueForecast{
		{Period: 1, Predicted: 100, LowerBound: 80, UpperBound: 120},
		{Period: 2, Predicted: 200, LowerBound: 160, UpperBound: 240},
	}
	scenarios := ml.ScenarioModel(base, 0.10) // 10% growth
	if len(scenarios) != 2 {
		t.Fatalf("expected 2 scenarios, got %d", len(scenarios))
	}
	if scenarios[0].Predicted < 109 || scenarios[0].Predicted > 111 {
		t.Fatalf("expected ~110 with 10%% growth, got %f", scenarios[0].Predicted)
	}
}
