package research

import (
	"strings"
	"testing"
)

func hit(title string, cost int64) SupplierHit {
	return SupplierHit{Supplier: "s1", Title: title, CostCents: cost, SourceURL: "https://supplier.example/" + title, Confidence: 0.8}
}

func landedFlat(h SupplierHit) int64 { return h.CostCents }

// Scenario 1: five hits with healthy margins come back as five priced
// candidates, each carrying cost, landed price, source URL and confidence.
func TestFivePricedCandidates(t *testing.T) {
	hits := []SupplierHit{hit("a", 1000), hit("b", 1100), hit("c", 1200), hit("d", 1300), hit("e", 1400)}
	// landed at twice the cost: a 50 percent margin, comfortably over the floor
	res := Run("apparel", hits, func(h SupplierHit) int64 { return h.CostCents * 2 }, 30)
	if len(res.Candidates) < 5 {
		t.Fatalf("candidates = %d, want at least five", len(res.Candidates))
	}
	for _, c := range res.Candidates {
		if c.CostCents == 0 || c.LandedPriceCents == 0 || c.SourceURL == "" || c.Confidence == 0 {
			t.Fatalf("candidate %+v missing a field the feature names", c)
		}
		if c.ID == "" {
			t.Fatalf("candidate %+v has no id", c)
		}
	}
}

// Scenario 2: a candidate whose margin cannot clear the floor is not
// returned, and the count line names the floor it applied.
func TestMarginFloorDropsSubFloorCandidates(t *testing.T) {
	// ok1 1000->2000 (50 percent) and ok2 3000->5000 (40 percent) clear the
	// floor; thin 500->600 (16.7 percent) cannot.
	hits := []SupplierHit{hit("ok1", 1000), hit("thin", 500), hit("ok2", 3000)}
	res := Run("electronics", hits, func(h SupplierHit) int64 {
		if h.Title == "thin" {
			return 600
		}
		if h.Title == "ok1" {
			return 2000
		}
		return 5000
	}, 30)
	for _, c := range res.Candidates {
		if strings.Contains(c.Title, "thin") {
			t.Fatalf("the sub-floor candidate was returned: %+v", c)
		}
	}
	if !strings.Contains(res.CountLine, "30") {
		t.Fatalf("the count line must name the floor it applied: %q", res.CountLine)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates = %d, want the two that clear the floor", len(res.Candidates))
	}
}

// Scenario 3: a category with no supplier hits answers an empty list with
// the reason naming the category and saying no supplier answered - no error.
func TestEmptyCategoryAnswersReasonNotError(t *testing.T) {
	res := Run("garden", nil, landedFlat, 30)
	if len(res.Candidates) != 0 {
		t.Fatalf("candidates = %d, want an empty list", len(res.Candidates))
	}
	if !strings.Contains(res.Reason, "garden") || !strings.Contains(res.Reason, "no supplier answered") {
		t.Fatalf("reason = %q, want the category and the words 'no supplier answered'", res.Reason)
	}
}

// The margin is computed on the LANDED price, not the supplier cost.
func TestMarginUsesLandedPrice(t *testing.T) {
	hits := []SupplierHit{hit("x", 6000)}
	res := Run("apparel", hits, func(h SupplierHit) int64 { return 12000 }, 30) // margin 50 on landed 12000
	if len(res.Candidates) != 1 {
		t.Fatalf("candidates = %d, want the landed-margin candidate kept", len(res.Candidates))
	}
	if got := res.Candidates[0].MarginPct(); got < 49.9 || got > 50.1 {
		t.Fatalf("margin = %v, want 50 on the landed price", got)
	}
	if !strings.HasPrefix(res.Candidates[0].ID, "apparel-") {
		t.Fatalf("id = %q, want the category prefix", res.Candidates[0].ID)
	}
}
