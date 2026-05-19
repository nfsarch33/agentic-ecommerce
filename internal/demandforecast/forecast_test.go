package demandforecast

import (
	"math"
	"testing"
)

var sampleData = []DataPoint{
	{Period: "2026-01", Value: 100},
	{Period: "2026-02", Value: 110},
	{Period: "2026-03", Value: 120},
	{Period: "2026-04", Value: 130},
	{Period: "2026-05", Value: 140},
}

func TestMovingAverage(t *testing.T) {
	t.Parallel()

	data := []DataPoint{
		{Period: "2026-01", Value: 1},
		{Period: "2026-02", Value: 2},
		{Period: "2026-03", Value: 3},
		{Period: "2026-04", Value: 4},
		{Period: "2026-05", Value: 5},
	}

	ma := MovingAverage(data, 3)
	// Expected: [(1+2+3)/3=2, (2+3+4)/3=3, (3+4+5)/3=4]
	expected := []float64{2, 3, 4}
	if len(ma) != len(expected) {
		t.Fatalf("len = %d, want %d", len(ma), len(expected))
	}
	for i, dp := range ma {
		if math.Abs(dp.Value-expected[i]) > 1e-9 {
			t.Errorf("ma[%d] = %f, want %f", i, dp.Value, expected[i])
		}
	}
}

func TestMovingAverage_WindowTooLarge(t *testing.T) {
	t.Parallel()

	data := []DataPoint{{Period: "2026-01", Value: 1}}
	if ma := MovingAverage(data, 5); ma != nil {
		t.Error("expected nil for window > data length")
	}
}

func TestLinearTrend(t *testing.T) {
	t.Parallel()

	// y = 2x + 5 with x in {0,1,2,3,4}
	data := []DataPoint{
		{Period: "1", Value: 5},
		{Period: "2", Value: 7},
		{Period: "3", Value: 9},
		{Period: "4", Value: 11},
		{Period: "5", Value: 13},
	}

	te := TrendExtractor{}
	slope, intercept := te.LinearTrend(data)

	if math.Abs(slope-2.0) > 1e-9 {
		t.Errorf("slope = %f, want 2.0", slope)
	}
	if math.Abs(intercept-5.0) > 1e-9 {
		t.Errorf("intercept = %f, want 5.0", intercept)
	}
}

func TestSeasonalIndex(t *testing.T) {
	t.Parallel()

	// Constant data => all seasonal indices should be 1.0
	data := []DataPoint{
		{Period: "2026-01", Value: 100},
		{Period: "2026-02", Value: 100},
		{Period: "2026-03", Value: 100},
		{Period: "2026-04", Value: 100},
	}

	si := SeasonalIndex(data, 4)
	if len(si) != 4 {
		t.Fatalf("len = %d, want 4", len(si))
	}
	for i, v := range si {
		if math.Abs(v-1.0) > 1e-9 {
			t.Errorf("si[%d] = %f, want 1.0", i, v)
		}
	}
}

func TestSeasonalIndex_Varying(t *testing.T) {
	t.Parallel()

	// Two full seasons: high in position 0, low in position 1.
	data := []DataPoint{
		{Period: "2026-01", Value: 150},
		{Period: "2026-02", Value: 50},
		{Period: "2026-03", Value: 150},
		{Period: "2026-04", Value: 50},
	}
	// overall mean = 100; si[0] = 150/100 = 1.5; si[1] = 50/100 = 0.5

	si := SeasonalIndex(data, 2)
	if len(si) != 2 {
		t.Fatalf("len = %d, want 2", len(si))
	}
	if math.Abs(si[0]-1.5) > 1e-9 {
		t.Errorf("si[0] = %f, want 1.5", si[0])
	}
	if math.Abs(si[1]-0.5) > 1e-9 {
		t.Errorf("si[1] = %f, want 0.5", si[1])
	}
}

func TestProjectNextN(t *testing.T) {
	t.Parallel()

	// Linear data with no seasonality variation (all same value).
	data := []DataPoint{
		{Period: "2026-01", Value: 100},
		{Period: "2026-02", Value: 100},
		{Period: "2026-03", Value: 100},
		{Period: "2026-04", Value: 100},
	}

	forecasts := ProjectNextN(data, 3, 4)
	if len(forecasts) != 3 {
		t.Fatalf("len = %d, want 3", len(forecasts))
	}

	// Slope should be ~0, intercept ~100 => predicted ~100 each.
	for i, f := range forecasts {
		if math.Abs(f.Predicted-100) > 1.0 {
			t.Errorf("forecasts[%d].Predicted = %f, want ~100", i, f.Predicted)
		}
	}
}

func TestProjectNextN_Trend(t *testing.T) {
	t.Parallel()

	// Increasing data y=10*i+100, season=1 (no seasonality).
	data := []DataPoint{
		{Period: "2026-01", Value: 100},
		{Period: "2026-02", Value: 110},
		{Period: "2026-03", Value: 120},
		{Period: "2026-04", Value: 130},
	}

	forecasts := ProjectNextN(data, 3, 1)
	if len(forecasts) != 3 {
		t.Fatalf("len = %d, want 3", len(forecasts))
	}

	// With slope=10 and intercept=100, next 3 are: 140, 150, 160.
	expected := []float64{140, 150, 160}
	for i, f := range forecasts {
		if math.Abs(f.Predicted-expected[i]) > 1e-6 {
			t.Errorf("forecasts[%d].Predicted = %f, want %f", i, f.Predicted, expected[i])
		}
	}
}
