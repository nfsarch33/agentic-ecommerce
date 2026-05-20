package ml

import "math"

type RevenuePoint struct {
	Period  int
	Revenue float64
}

type RevenueForecast struct {
	Period     int
	Predicted  float64
	LowerBound float64
	UpperBound float64
}

// TimeSeriesModel holds fitted linear regression parameters.
type TimeSeriesModel struct {
	Slope     float64
	Intercept float64
	StdErr    float64
}

// Fit performs OLS linear regression on revenue history.
func (m *TimeSeriesModel) Fit(history []RevenuePoint) {
	n := float64(len(history))
	if n == 0 {
		return
	}
	var sumX, sumY, sumXY, sumX2 float64
	for _, p := range history {
		x, y := float64(p.Period), p.Revenue
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}
	denom := n*sumX2 - sumX*sumX
	if denom != 0 {
		m.Slope = (n*sumXY - sumX*sumY) / denom
		m.Intercept = (sumY - m.Slope*sumX) / n
	}
	// Compute residual std error
	var sse float64
	for _, p := range history {
		pred := m.Intercept + m.Slope*float64(p.Period)
		diff := p.Revenue - pred
		sse += diff * diff
	}
	if n > 2 {
		m.StdErr = math.Sqrt(sse / (n - 2))
	}
}

// Predict returns the point forecast for a future period.
func (m *TimeSeriesModel) Predict(period int) float64 {
	return math.Max(0, m.Intercept+m.Slope*float64(period))
}

// ConfidenceInterval returns a forecast with lower/upper bounds at ~95% (1.96 * stdErr).
func ConfidenceInterval(model TimeSeriesModel, period int, zScore float64) RevenueForecast {
	pred := model.Predict(period)
	margin := zScore * model.StdErr
	return RevenueForecast{
		Period:     period,
		Predicted:  pred,
		LowerBound: math.Max(0, pred-margin),
		UpperBound: pred + margin,
	}
}

// SeasonalDecomp decomposes revenue into trend, seasonal, and residual components.
// seasonPeriod is the number of periods in one season (e.g. 12 for monthly).
func SeasonalDecomp(history []RevenuePoint, seasonPeriod int) (trend, seasonal, residual []float64) {
	n := len(history)
	if n == 0 || seasonPeriod <= 0 {
		return nil, nil, nil
	}
	trend = make([]float64, n)
	seasonal = make([]float64, n)
	residual = make([]float64, n)

	// Simple moving average for trend
	half := seasonPeriod / 2
	for i := range history {
		start := i - half
		end := i + half
		if start < 0 {
			start = 0
		}
		if end >= n {
			end = n - 1
		}
		var sum float64
		count := 0
		for j := start; j <= end; j++ {
			sum += history[j].Revenue
			count++
		}
		trend[i] = sum / float64(count)
	}

	// Seasonal factor: average deviation from trend by position in season
	seasonalAvg := make([]float64, seasonPeriod)
	seasonalCount := make([]int, seasonPeriod)
	for i, p := range history {
		if trend[i] != 0 {
			idx := i % seasonPeriod
			seasonalAvg[idx] += p.Revenue / trend[i]
			seasonalCount[idx]++
		}
	}
	for i := range seasonalAvg {
		if seasonalCount[i] > 0 {
			seasonalAvg[i] /= float64(seasonalCount[i])
		} else {
			seasonalAvg[i] = 1.0
		}
	}
	for i := range history {
		seasonal[i] = seasonalAvg[i%seasonPeriod]
		if seasonal[i] != 0 {
			residual[i] = history[i].Revenue / (trend[i] * seasonal[i])
		}
	}
	return trend, seasonal, residual
}

// ScenarioModel applies a growth rate multiplier to a base forecast.
func ScenarioModel(base []RevenueForecast, growthRate float64) []RevenueForecast {
	out := make([]RevenueForecast, len(base))
	for i, f := range base {
		out[i] = RevenueForecast{
			Period:     f.Period,
			Predicted:  f.Predicted * (1 + growthRate),
			LowerBound: f.LowerBound * (1 + growthRate),
			UpperBound: f.UpperBound * (1 + growthRate),
		}
	}
	return out
}
