package sourcing

import (
	"github.com/nfsarch33/helixon-ec/internal/adapter/china"
	"github.com/nfsarch33/helixon-ec/internal/domain"
)

// scoreInput pairs a candidate product with its supplier-score so
// the ranker can read both without reaching back into the domain
// scorer twice.
type scoreInput struct {
	product       china.Product
	supplier      domain.Supplier
	supplierScore float64
}

// rank computes the composite score for a single candidate. The
// composite is a weighted blend of three independent scores:
//
//   - supplier (0..1): EC-1-5 supplier score
//   - margin   (0..1): clamp(1 - cost / sellPrice)
//   - trend    (0..1): pgvector cosine similarity vs. trend signal
//
// Weights mirror the ADR-026 sourcing weighting (40 / 35 / 25). They
// live as named constants so the next sprint can A/B-tune them
// without re-reading the loop logic.
const (
	supplierWeight = 0.40
	marginWeight   = 0.35
	trendWeight    = 0.25
)

// rankComposite returns the blended composite score for the inputs.
func rankComposite(supplierScore, marginScore, trendScore float64) float64 {
	return supplierWeight*clamp01(supplierScore) +
		marginWeight*clamp01(marginScore) +
		trendWeight*clamp01(trendScore)
}

// computeMarginScore returns the relative margin in [0, 1] given the
// expected sell price and the supplier cost. A 1.0 margin means the
// product is free at supplier; values below 0 are clamped to 0.
//
// expectedSellPriceCNYCents and priceCNYCents share the same units
// so the ratio is dimensionless.
func computeMarginScore(priceCNYCents, expectedSellPriceCNYCents int) float64 {
	if expectedSellPriceCNYCents <= 0 {
		return 0
	}
	margin := 1 - float64(priceCNYCents)/float64(expectedSellPriceCNYCents)
	return clamp01(margin)
}

// expectedSellPriceCNY returns a default operator-side sell price
// estimate from the supplier-side cost. v3.1.0 baseline: 2.5x markup
// (matches typical 60% gross margin target from the EC-1 acceptance
// criteria). Real pricing comes from EC-6-3 dynamic pricing agent.
func expectedSellPriceCNY(priceCNYCents int) int {
	return priceCNYCents * 5 / 2
}

// clamp01 (declared in agent.go) is reused here.
