// Package margingate is the deterministic margin gate of the E2 listing
// spec (section 2.1, criteria S1-S2): reject a net margin under 30 percent
// or a break-even above 20 percent of the batch. It calls no model; the
// arithmetic is the evidence/v18960-137-ecommerce-spike/margin_check.py
// template - same fields, same order of operations - in integer cents.
package margingate

import (
	"fmt"
	"math"
)

// Inputs are the gate's numbers, all in cents (ReferralPct in percent).
type Inputs struct {
	RetailCents     int64
	UnitCostCents   int64
	FulfilCents     int64
	ReferralPct     float64
	BatchFixedCents int64
	BatchUnits      int64
}

// Verdict is the gate's answer; Reason carries the rule that decided it.
type Verdict struct {
	Pass                bool
	Reason              string
	NetMarginPct        float64
	BreakEvenUnits      int64
	BreakEvenPctOfBatch float64
}

// Gate applies the deterministic rule: per-unit margin is
// retail*(1-referral) - cost - fulfil; net margin is that over retail;
// break-even is the batch-fixed cost over the per-unit margin, rounded up.
func Gate(in Inputs) Verdict {
	if in.RetailCents <= 0 {
		return Verdict{Reason: "no retail price: the numbers cannot be gated"}
	}
	referral := in.ReferralPct / 100
	perUnit := float64(in.RetailCents)*(1-referral) - float64(in.UnitCostCents) - float64(in.FulfilCents)
	if perUnit <= 0 {
		return Verdict{Reason: "no per-unit margin: the numbers cannot pay for themselves"}
	}
	net := perUnit / float64(in.RetailCents) * 100
	breakEven := int64(math.Ceil(float64(in.BatchFixedCents) / perUnit))
	pctBatch := float64(breakEven) / float64(in.BatchUnits) * 100
	switch {
	case net < 30:
		return Verdict{NetMarginPct: net, BreakEvenUnits: breakEven, BreakEvenPctOfBatch: pctBatch,
			Reason: fmt.Sprintf("net margin %.1f%% is under the 30%% floor", net)}
	case pctBatch > 20:
		return Verdict{NetMarginPct: net, BreakEvenUnits: breakEven, BreakEvenPctOfBatch: pctBatch,
			Reason: fmt.Sprintf("break-even %d units is %.1f%% of the batch, over the 20%% ceiling", breakEven, pctBatch)}
	default:
		return Verdict{Pass: true, NetMarginPct: net, BreakEvenUnits: breakEven, BreakEvenPctOfBatch: pctBatch,
			Reason: fmt.Sprintf("net margin %.1f%%, break-even %.1f%% of the batch", net, pctBatch)}
	}
}
