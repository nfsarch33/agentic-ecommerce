// Package domain is the canonical home for v3.1.0 Story EC-1-5
// supplier value objects and scoring.
//
// The Supplier struct + Score() method are pure-function domain logic.
// They have NO infrastructure dependencies (no DB, no HTTP, no
// goroutines) and are safe to call from any layer (sourcing agent,
// compliance gate, billing, dashboard).
//
// Scoring rules (v3.1.0 baseline):
//   - MOQ > 50 units carries a linear penalty (the higher the MOQ the
//     larger the penalty), capped so a single dimension cannot zero
//     the score on its own.
//   - LeadTimeDays > 20 carries a similar linear penalty.
//   - VerifiedGold suppliers receive a bonus.
//   - PositiveReviewRatio above 0.85 receives an additional bonus.
//
// All thresholds are exported as package-level constants so tests and
// consumers can cite them directly instead of duplicating magic numbers.
package domain

import "errors"

// Score boundaries.
const (
	SupplierScoreMin = 0.0
	SupplierScoreMax = 1.0

	// MOQPenaltyThreshold is the inclusive upper bound at which no MOQ
	// penalty applies. Suppliers with MOQ above this threshold lose
	// MOQPenaltySlope per unit of MOQ above the threshold (clamped).
	MOQPenaltyThreshold = 50

	// LeadTimePenaltyThresholdDays is the lead-time threshold above
	// which the linear penalty kicks in.
	LeadTimePenaltyThresholdDays = 20

	// MOQPenaltyCap caps the MOQ penalty so very high MOQs cannot drive
	// the score below zero on their own. The remaining score deltas
	// come from lead-time and review ratio.
	MOQPenaltyCap = 0.45

	// LeadTimePenaltyCap caps the lead-time penalty similarly.
	LeadTimePenaltyCap = 0.45

	// VerifiedGoldBonus rewards 1688 Gold-verified suppliers.
	VerifiedGoldBonus = 0.15

	// ReviewBonusThreshold is the minimum positive-review ratio at
	// which the review bonus applies.
	ReviewBonusThreshold = 0.85

	// ReviewBonus is added when ReviewBonusThreshold is met.
	ReviewBonus = 0.10

	// SupplierScoreFloor is the operational floor used when filtering
	// suppliers. Anything below this fails the gate in EC-1-3.
	SupplierScoreFloor = 0.50
)

// ErrSupplierBelowScore is the sentinel returned by FilterByScore
// when a supplier fails the floor. Wrapped with %w when returned.
var ErrSupplierBelowScore = errors.New("supplier: score below floor")

// ErrInvalidSupplier is returned by Validate when required identity
// fields are missing.
var ErrInvalidSupplier = errors.New("supplier: invalid")

// Supplier is the canonical sourcing-side supplier value object.
// TenantID scopes the row per ADR-026 D5 (RLS on every domain entity).
type Supplier struct {
	ID                  string
	TenantID            string
	Name                string
	Country             string
	Platform            string
	MOQ                 int
	LeadTimeDays        int
	VerifiedGold        bool
	PositiveReviewRatio float64
}

// Validate enforces required identity fields. Score() does NOT call
// Validate so the scorer can be reused for synthetic shoplifting
// scenarios in pricing tests; callers persisting suppliers MUST run
// Validate first.
func (s Supplier) Validate() error {
	if s.ID == "" {
		return errors.Join(ErrInvalidSupplier, errors.New("id required"))
	}
	if s.TenantID == "" {
		return errors.Join(ErrInvalidSupplier, errors.New("tenant_id required"))
	}
	return nil
}

// Score returns a value in [SupplierScoreMin, SupplierScoreMax]. The
// computation is intentionally simple so reviewers can audit it by
// hand: start at 1.0, subtract penalties (capped), add bonuses
// (clamped). Returns the score and a boolean flag indicating whether
// the supplier passes SupplierScoreFloor.
func (s Supplier) Score() (float64, bool) {
	score := 1.0
	score -= moqPenalty(s.MOQ)
	score -= leadTimePenalty(s.LeadTimeDays)
	if s.VerifiedGold {
		score += VerifiedGoldBonus
	}
	if s.PositiveReviewRatio >= ReviewBonusThreshold {
		score += ReviewBonus
	}
	score = clamp(score, SupplierScoreMin, SupplierScoreMax)
	return score, score >= SupplierScoreFloor
}

// FilterByScore returns the subset of suppliers whose Score is at or
// above the floor. Used by the EC-1-3 sourcing agent to drop weak
// suppliers before the LLM ranking step. Inputs are not mutated.
func FilterByScore(suppliers []Supplier, floor float64) []Supplier {
	if floor < SupplierScoreMin {
		floor = SupplierScoreFloor
	}
	out := make([]Supplier, 0, len(suppliers))
	for _, supplier := range suppliers {
		if score, _ := supplier.Score(); score >= floor {
			out = append(out, supplier)
		}
	}
	return out
}

func moqPenalty(moq int) float64 {
	if moq <= MOQPenaltyThreshold {
		return 0
	}
	excess := float64(moq - MOQPenaltyThreshold)
	// Slope: 0.45 cap reached at MOQ = threshold + 450.
	penalty := excess * 0.001
	if penalty > MOQPenaltyCap {
		penalty = MOQPenaltyCap
	}
	return penalty
}

func leadTimePenalty(days int) float64 {
	if days <= LeadTimePenaltyThresholdDays {
		return 0
	}
	excess := float64(days - LeadTimePenaltyThresholdDays)
	// Slope: 0.45 cap reached at lead time = threshold + 45 days.
	penalty := excess * 0.01
	if penalty > LeadTimePenaltyCap {
		penalty = LeadTimePenaltyCap
	}
	return penalty
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
