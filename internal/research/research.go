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
	ID          string
	Title       string
	CostCents   int64
	LandedCents int64
	SourceURL   string
	Confidence  float64
}

// Result is the step's answer: the candidates that cleared the floor, the
// count line naming the floor it applied, and - when no supplier answered -
// the empty list with its reason.
type Result struct {
	Candidates []Candidate
	CountLine  string
	Reason     string
}

// MarginPct is the candidate's margin over its landed cost, as a percentage
// of its landed price.
func (c Candidate) MarginPct(floorPct float64) float64 {
	if c.LandedCents <= 0 {
		return 0
	}
	return float64(c.LandedCents-c.CostCents) / float64(c.LandedCents) * 100
}

// Run researches one category: supplier hits in, floor-checked candidates
// out. landedFor prices a hit's landed cost (shipping, duty, handling - the
// adapter's own arithmetic); floorPct is the margin rule's floor and is
// computed on the LANDED price. A hit whose margin cannot clear the floor is
// dropped and counted. No hits is an answer, not an error: the reason names
// the category and says no supplier answered.
func Run(category string, hits []SupplierHit, landedFor func(SupplierHit) int64, floorPct float64) Result {
	if len(hits) == 0 {
		return Result{Candidates: []Candidate{}, Reason: fmt.Sprintf("%s: no supplier answered", category)}
	}
	res := Result{Candidates: []Candidate{}}
	dropped := 0
	for i, h := range hits {
		landed := landedFor(h)
		c := Candidate{
			ID:          fmt.Sprintf("%s-%d", category, i+1),
			Title:       h.Title,
			CostCents:   h.CostCents,
			LandedCents: landed,
			SourceURL:   h.SourceURL,
			Confidence:  h.Confidence,
		}
		if c.MarginPct(floorPct) < floorPct {
			dropped++
			continue
		}
		res.Candidates = append(res.Candidates, c)
	}
	res.CountLine = fmt.Sprintf("%d candidates returned, %d dropped below the %.0f%% margin floor", len(res.Candidates), dropped, floorPct)
	return res
}
