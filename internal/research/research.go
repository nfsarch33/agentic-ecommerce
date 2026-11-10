// Package research is the listing pipeline's first step: it turns one
// own-shop category into priced candidates. Every candidate carries cost,
// landed price, source URL and confidence; the margin floor drops what
// cannot clear it; a category with no supplier hits answers an empty list
// with the reason, never an error.
package research

import "fmt"

// SupplierHit is one supplier's answer for a category: a candidate at a
// cost, with the source to cite.
type SupplierHit struct {
	Supplier  string
	Title     string
	CostCents int64
	SourceURL string
	// Confidence is the supplier adapter's own score for the match, 0..1.
	Confidence float64
}

// Candidate is one researched, floor-checked product row.
type Candidate struct {
	ID               string
	Title            string
	CostCents        int64
	LandedPriceCents int64
	SourceURL        string
	Confidence       float64
}

// Result is the step's answer: the candidates that cleared the floor, the
// count line naming the floor it applied, and - when no supplier answered -
// the empty list with its reason.
type Result struct {
	Candidates []Candidate
	CountLine  string
	Reason     string
}

// MarginPct is the candidate's margin over its cost, as a percentage of its
// landed price (rev547 finding 3: no unread parameter).
func (c Candidate) MarginPct() float64 {
	if c.LandedPriceCents <= 0 {
		return 0
	}
	return float64(c.LandedPriceCents-c.CostCents) / float64(c.LandedPriceCents) * 100
}

// Run researches one category: supplier hits in, floor-checked candidates
// out. landedFor prices a hit's LANDED PRICE - the sale basis after
// shipping, duty and handling (the acceptance contract's words: "each
// carries cost, landed price, source url and confidence") - and floorPct is
// the margin floor computed on that price. A hit whose margin cannot clear
// the floor is dropped and counted. No hits is an answer, not an error: the
// reason names the category and says no supplier answered.
func Run(category string, hits []SupplierHit, landedFor func(SupplierHit) int64, floorPct float64) Result {
	if len(hits) == 0 {
		return Result{Candidates: []Candidate{}, Reason: fmt.Sprintf("%s: no supplier answered", category)}
	}
	res := Result{Candidates: []Candidate{}}
	dropped := 0
	for i, h := range hits {
		landed := landedFor(h)
		c := Candidate{
			ID:               fmt.Sprintf("%s-%d", category, i+1),
			Title:            h.Title,
			CostCents:        h.CostCents,
			LandedPriceCents: landed,
			SourceURL:        h.SourceURL,
			Confidence:       h.Confidence,
		}
		if c.MarginPct() < floorPct {
			dropped++
			continue
		}
		res.Candidates = append(res.Candidates, c)
	}
	res.CountLine = fmt.Sprintf("%d candidates returned, %d dropped below the %.0f%% margin floor", len(res.Candidates), dropped, floorPct)
	return res
}
