package pricerule

import (
	"strings"
	"testing"
)

// The four scenarios of acceptance/price-rule.feature. Cost 6000 cents,
// floor 30 percent: the margin floor price is 6000/(1-0.30) = 8571.428...
// rounded to 8572 cents - the minimum price whose margin over the landed
// cost clears 30 percent of the price itself.
func rule() Rule {
	return Rule{RuleName: "placeholder-margin-v0", MarginFloorPct: 30, BandSteering: "midpoint", BandCeilingCap: true, Currency: "AUD"}
}

func TestBelowBandFloorApplied(t *testing.T) {
	// floor price 8572 sits BELOW the band 9000..12000: the floor prices it.
	got := Apply(rule(), 6000, &Band{Low: 9000, High: 12000})
	if got.PriceCents != 8572 {
		t.Fatalf("price = %d, want the floor price 8572", got.PriceCents)
	}
	if got.Reason != "below band: floor applied" {
		t.Fatalf("reason = %q, want the feature's exact words", got.Reason)
	}
	if got.RuleName != "placeholder-margin-v0" {
		t.Fatalf("the record must name the rule: %q", got.RuleName)
	}
}

func TestInsideBandMidpointSteers(t *testing.T) {
	// band 8000..9200 contains the floor price 8572: the midpoint 8600 steers.
	got := Apply(rule(), 6000, &Band{Low: 8000, High: 9200})
	if got.PriceCents != 8600 {
		t.Fatalf("price = %d, want the band midpoint 8600", got.PriceCents)
	}
	if got.Reason != "inside band" {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestAboveBandCeilingCaps(t *testing.T) {
	// band 7000..8000 sits below the floor price 8572: the ceiling caps it.
	got := Apply(rule(), 6000, &Band{Low: 7000, High: 8000})
	if got.PriceCents != 8000 {
		t.Fatalf("price = %d, want the band ceiling 8000", got.PriceCents)
	}
	if got.Reason != "above band: capped" {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestNoCompetitorMarginPrice(t *testing.T) {
	got := Apply(rule(), 6000, nil)
	if got.PriceCents != 8572 || got.Reason != "no competitor data: margin price applied" {
		t.Fatalf("got %d %q, want the margin price and the fallback words", got.PriceCents, got.Reason)
	}
}

// The floor is a FLOOR on margin, not a markup: 30 percent of the PRICE, so
// price 8571 on cost 6000 is a 29.99 percent margin and must round UP.
func TestFloorRoundsUpOnTheMargin(t *testing.T) {
	got := Apply(rule(), 6000, &Band{Low: 90000, High: 99000})
	if got.PriceCents != 8572 {
		t.Fatalf("price = %d, want 8572 - the first cent that clears the floor", got.PriceCents)
	}
	if !strings.Contains(got.Reason, "floor applied") {
		t.Fatalf("reason = %q", got.Reason)
	}
}

// The band CONTAINS its own bottom edge: a band whose Low equals the floor
// price steers inside (the feature's "a band that contains the floor price"),
// it does not fall to the "below band" case.
func TestBandEdgeIsContained(t *testing.T) {
	got := Apply(rule(), 6000, &Band{Low: 8572, High: 9200})
	if got.PriceCents != 8886 || got.Reason != "inside band" {
		t.Fatalf("got %d %q, want the midpoint 8886 inside band", got.PriceCents, got.Reason)
	}
}
