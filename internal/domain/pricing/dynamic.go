package pricing

import (
	"math"
	"time"
)

// DemandFactor returns a price multiplier based on views/purchases ratio.
func DemandFactor(views, purchases int, window time.Duration) float64 {
	if views == 0 {
		return 1.0
	}
	convRate := float64(purchases) / float64(views)
	// Normalize: 5% conversion = neutral (1.0). Higher rates up-price.
	factor := 1.0 + (convRate-0.05)*2
	if factor < 0.5 {
		return 0.5
	}
	if factor > 2.0 {
		return 2.0
	}
	return factor
}

// CompetitorAdjust returns a price undercut by 5% below the lowest competitor.
func CompetitorAdjust(basePrice int, competitorPrices []int) int {
	if len(competitorPrices) == 0 {
		return basePrice
	}
	minComp := competitorPrices[0]
	for _, p := range competitorPrices[1:] {
		if p < minComp {
			minComp = p
		}
	}
	adjusted := int(float64(minComp) * 0.95)
	return adjusted
}

// FloorCeiling clamps price within [floor, ceiling].
func FloorCeiling(price, floor, ceiling int) int {
	if price < floor {
		return floor
	}
	if price > ceiling {
		return ceiling
	}
	return price
}

// TimeDecay reduces price over time with exponential decay.
func TimeDecay(price int, age, halfLife time.Duration) int {
	if halfLife <= 0 {
		return price
	}
	decayFactor := math.Pow(0.5, float64(age)/float64(halfLife))
	return int(float64(price) * decayFactor)
}
