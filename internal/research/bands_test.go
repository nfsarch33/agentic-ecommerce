package research

import (
	"strings"
	"testing"
)

func rec(kind BandKind, low, high, moq int64, src, query string) BandRecord {
	return BandRecord{Kind: kind, LowCents: low, HighCents: high, MOQUnits: moq, SourceURL: src, Query: query}
}

// Acceptance scenario 1: no retail band in four searches -> not established,
// naming the band, after exactly four searches.
func TestFourSearchesMissingRetailStops(t *testing.T) {
	res := Establish([]BandRecord{
		rec(BandWholesale, 500, 600, 500, "https://s.example/wh", "q1"),
		rec(BandDemand, 40, 60, 0, "https://s.example/dm", "q2"),
		rec(BandWholesale, 520, 610, 500, "https://s.example/wh2", "q3"),
		rec(BandDemand, 41, 59, 0, "https://s.example/dm2", "q4"),
	})
	if res.NotEstablished == "" {
		t.Fatalf("four searches with no retail band must be not established: %+v", res)
	}
	if res.Searches != 4 {
		t.Fatalf("searches = %d, want 4", res.Searches)
	}
	if !strings.Contains(res.NotEstablished, "retail") {
		t.Fatalf("the stop must name the missing band: %q", res.NotEstablished)
	}
}

// Fewer than four searches with a missing band is still establishing.
func TestUnderFourSearchesContinues(t *testing.T) {
	res := Establish([]BandRecord{rec(BandWholesale, 500, 600, 500, "https://s.example/wh", "q1")})
	if res.NotEstablished != "" || res.Done {
		t.Fatalf("one search with retail pending must continue: %+v", res)
	}
}

// Both bands present in two searches: established; the demand signal rides
// along without blocking.
func TestTwoSearchesBothBandsEstablished(t *testing.T) {
	res := Establish([]BandRecord{
		rec(BandWholesale, 500, 600, 500, "https://s.example/wh", "q1"),
		rec(BandRetail, 2900, 3100, 0, "https://s.example/rt", "q2"),
	})
	if !res.Done || res.NotEstablished != "" {
		t.Fatalf("both bands present must be established: %+v", res)
	}
	if res.Wholesale == nil || res.Wholesale.MOQUnits != 500 || res.Wholesale.SourceURL == "" {
		t.Fatalf("the wholesale band must carry MOQ and source: %+v", res.Wholesale)
	}
	if res.Retail == nil || res.Retail.SourceURL == "" {
		t.Fatalf("the retail band must carry its source: %+v", res.Retail)
	}
}

// A number without a source url is a defect in the run, never a band.
func TestUnsourcedNumberIsADefect(t *testing.T) {
	res := Establish([]BandRecord{
		rec(BandWholesale, 500, 600, 500, "", "q1"),
		rec(BandRetail, 2900, 3100, 0, "https://s.example/rt", "q2"),
	})
	if res.Defect == "" {
		t.Fatal("an unsourced number must be recorded as a defect")
	}
	if res.Done {
		t.Fatal("an unsourced run must not establish bands")
	}
}
