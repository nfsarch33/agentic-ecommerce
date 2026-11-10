// Package pricerule turns one landed cost into a listing price under the
// margin rule: the floor sets the minimum, the competitor band steers inside
// it, the ceiling caps it, and the record names the rule and the band it
// applied. The rule's SHAPE is the contract (config/price-rule-placeholder.yml);
// the numbers are the founder's to replace.
package pricerule

import "math"

// Rule is the margin rule table (the placeholder config's shape).
type Rule struct {
	RuleName       string
	MarginFloorPct float64
	BandSteering   string
	BandCeilingCap bool
	Currency       string
}

// Band is a competitor price band, in cents.
type Band struct {
	Low  int64
	High int64
}

// Result is one priced row and the reason the rule chose it.
type Result struct {
	PriceCents int64
	RuleName   string
	Reason     string
}

// Apply prices one landed cost under the rule:
//
//   - the margin floor price is the first cent at which the margin over the
//     landed cost clears MarginFloorPct OF THE PRICE (a floor on margin, not
//     a markup), rounded up;
//   - a band above the floor leaves the floor price in charge ("below band");
//   - a band containing the floor steers to the band's midpoint ("inside
//     band") - the placeholder steering point;
//   - a band below the floor is capped at its ceiling when BandCeilingCap is
//     set ("above band: capped"): never price above the band's ceiling;
//   - no band at all applies the margin price and says so.
//
// The reason strings are the feature's exact words: acceptance/price-rule
// .feature is the contract, and the listing console's row text parses them.
func Apply(r Rule, costCents int64, band *Band) Result {
	floor := floorPrice(costCents, r.MarginFloorPct)
	res := Result{RuleName: r.RuleName, PriceCents: floor}
	switch {
	case band == nil:
		res.Reason = "no competitor data: margin price applied"
	case band.Low > floor:
		res.Reason = "below band: floor applied"
	case floor > band.High:
		res.PriceCents = band.High
		res.Reason = "above band: capped"
	default:
		// rev547 finding 1: the floor sets the minimum, so a midpoint that
		// lands below the floor price steers UP to the floor.
		mid := (band.Low + band.High) / 2
		res.PriceCents = mid
		if floor > mid {
			res.PriceCents = floor
		}
		res.Reason = "inside band"
	}
	return res
}

// floorPrice is the first cent whose margin over cost clears floorPct of the
// price: price = cost / (1 - floor/100), rounded UP so the floor is a floor
// (the cent below it is a 29.99 percent margin at floor 30).
func floorPrice(costCents int64, floorPct float64) int64 {
	if costCents <= 0 || floorPct <= 0 {
		return costCents
	}
	if floorPct >= 100 {
		return math.MaxInt64
	}
	f := float64(costCents) / (1 - floorPct/100)
	return int64(math.Ceil(f - 1e-9))
}
