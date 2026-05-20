package ml

import "math"

type SalesRecord struct {
	Day   int
	Units int
}

type ForecastPoint struct {
	Day   int
	Units float64
}

// DemandForecast uses simple linear regression to forecast future demand.
func DemandForecast(history []SalesRecord, horizon int) []ForecastPoint {
	if len(history) == 0 {
		out := make([]ForecastPoint, horizon)
		for i := range out {
			out[i] = ForecastPoint{Day: i + 1, Units: 0}
		}
		return out
	}
	// Linear regression
	n := float64(len(history))
	var sumX, sumY, sumXY, sumX2 float64
	for _, r := range history {
		x, y := float64(r.Day), float64(r.Units)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}
	denom := n*sumX2 - sumX*sumX
	var slope, intercept float64
	if denom != 0 {
		slope = (n*sumXY - sumX*sumY) / denom
		intercept = (sumY - slope*sumX) / n
	}
	lastDay := history[len(history)-1].Day
	out := make([]ForecastPoint, horizon)
	for i := 0; i < horizon; i++ {
		day := lastDay + i + 1
		units := math.Max(0, intercept+slope*float64(day))
		out[i] = ForecastPoint{Day: day, Units: units}
	}
	return out
}

// SafetyStock computes safety stock for a given service level using z-score approximation.
func SafetyStock(forecast []ForecastPoint, serviceLevel float64) int {
	if len(forecast) == 0 {
		return 0
	}
	// Z-scores for common service levels: 0.90->1.28, 0.95->1.65, 0.99->2.33
	z := 1.65 // default 95%
	if serviceLevel >= 0.99 {
		z = 2.33
	} else if serviceLevel <= 0.90 {
		z = 1.28
	}
	// Compute std dev of forecast
	sum, sum2 := 0.0, 0.0
	for _, f := range forecast {
		sum += f.Units
		sum2 += f.Units * f.Units
	}
	n := float64(len(forecast))
	variance := sum2/n - (sum/n)*(sum/n)
	if variance < 0 {
		variance = 0
	}
	stdDev := math.Sqrt(variance)
	return int(math.Ceil(z * stdDev))
}

// ReorderPoint computes when to reorder to avoid stockout.
func ReorderPoint(dailyDemand, leadTimeDays, safetyStock int) int {
	return dailyDemand*leadTimeDays + safetyStock
}

// SeasonalAdjust multiplies each forecast point by the corresponding season factor.
func SeasonalAdjust(forecast []ForecastPoint, seasonFactors []float64) []ForecastPoint {
	if len(seasonFactors) == 0 {
		return forecast
	}
	out := make([]ForecastPoint, len(forecast))
	for i, f := range forecast {
		factor := seasonFactors[i%len(seasonFactors)]
		out[i] = ForecastPoint{Day: f.Day, Units: math.Max(0, f.Units*factor)}
	}
	return out
}
