// Package coord -- v4.7.0 Story 2: concrete pricing/fulfilment
// coordinator handling the three plan scenarios:
//
//  1. Pricing wants to raise price + fulfilment wants to deplete
//     stock fast -> pricing wins (higher priority weight 0.8 > 0.6).
//  2. Fulfilment detects supplier stockout + pricing suggests
//     discount -> fulfilment constraint overrides pricing suggestion.
//  3. Both agents propose actions for same SKU within same tick ->
//     coordinator deduplicates + sequences.
//
// Emits CoordinationDecisionEvent to the eventbus for the SSE agent
// activity feed (v3.6.0 EC-9-2).
//
// Decomposition discipline (HARD GATE: complex_fn=4):
//
//   - Coordinate         -> detectConflict + applyPriorityRule
//   - detectConflict     -> pair comparison (pure)
//   - applyPriorityRule  -> weighted priority or constraint override
//   - handleConstraintOverride -> stockout / safety check
//   - deduplicateSameSKU -> group + sequence
package coord

import (
	"context"
	"time"
)

// ConstraintType enumerates the override constraints that can
// supersede priority-based resolution.
type ConstraintType string

const (
	ConstraintSupplierStockout ConstraintType = "supplier_stockout"
	ConstraintSafetyHold       ConstraintType = "safety_hold"
)

// PricingFulfilmentDecision extends AgentDecision with optional
// constraint metadata. When a constraint is active, it overrides
// the priority-based resolution.
type PricingFulfilmentDecision struct {
	AgentDecision
	Constraint     ConstraintType
	ConstraintData map[string]interface{}
}

// CoordinationDecisionEvent is the eventbus payload emitted after
// each pricing/fulfilment coordination decision for the SSE agent
// activity feed.
type CoordinationDecisionEvent struct {
	TenantID       string    `json:"tenant_id"`
	SKU            string    `json:"sku"`
	WinnerAgent    string    `json:"winner_agent"`
	LoserAgent     string    `json:"loser_agent"`
	Resolution     string    `json:"resolution"`
	Reason         string    `json:"reason"`
	ConstraintUsed string    `json:"constraint_used,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

// PricingFulfilmentCoordinator handles the conflict surface between
// the pricing agent (v3.5.0 EC-6-3) and fulfilment agent (v3.5.0
// EC-7-2). Uses WeightedPriority as the base resolver with
// constraint overrides for safety scenarios.
type PricingFulfilmentCoordinator struct {
	policy    *WeightedPriority
	now       func() time.Time
	publisher func(ctx context.Context, event CoordinationDecisionEvent) error
}

// PricingFulfilmentConfig wires the coordinator.
type PricingFulfilmentConfig struct {
	Policy    *WeightedPriority
	Now       func() time.Time
	Publisher func(ctx context.Context, event CoordinationDecisionEvent) error
}

// NewPricingFulfilmentCoordinator creates a new coordinator.
func NewPricingFulfilmentCoordinator(cfg PricingFulfilmentConfig) *PricingFulfilmentCoordinator {
	if cfg.Policy == nil {
		cfg.Policy = NewWeightedPriority(DefaultAgentPriorities())
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &PricingFulfilmentCoordinator{
		policy:    cfg.Policy,
		now:       cfg.Now,
		publisher: cfg.Publisher,
	}
}

// CoordinateDecisions resolves a set of pricing/fulfilment decisions
// for a given tenant + SKU. Returns the chosen decision, the
// resolution event, and any constraint that was applied.
func (pfc *PricingFulfilmentCoordinator) CoordinateDecisions(ctx context.Context, tenantID, sku string, decisions []PricingFulfilmentDecision) (PricingFulfilmentDecision, CoordinationDecisionEvent, error) {
	deduped := deduplicateSameSKU(decisions)
	override, hasOverride := detectConstraintOverride(deduped)
	if hasOverride {
		event := pfc.buildEvent(tenantID, sku, override, "constraint_override", string(override.Constraint))
		pfc.publishEvent(ctx, event)
		return override, event, nil
	}
	chosen := pfc.applyPriorityRule(deduped)
	event := pfc.buildEvent(tenantID, sku, chosen, "weighted_priority", "")
	pfc.publishEvent(ctx, event)
	return chosen, event, nil
}

func (pfc *PricingFulfilmentCoordinator) applyPriorityRule(decisions []PricingFulfilmentDecision) PricingFulfilmentDecision {
	if len(decisions) == 0 {
		return PricingFulfilmentDecision{}
	}
	base := make([]AgentDecision, len(decisions))
	for i, d := range decisions {
		base[i] = d.AgentDecision
	}
	winner := pfc.policy.Resolve(base)
	for _, d := range decisions {
		if d.AgentName == winner.AgentName {
			return d
		}
	}
	return decisions[0]
}

// detectConstraintOverride checks if any decision carries an active
// constraint that should override priority-based resolution.
// Fulfilment constraints (stockout, safety) override pricing.
func detectConstraintOverride(decisions []PricingFulfilmentDecision) (PricingFulfilmentDecision, bool) {
	for _, d := range decisions {
		if d.Constraint == ConstraintSupplierStockout || d.Constraint == ConstraintSafetyHold {
			return d, true
		}
	}
	return PricingFulfilmentDecision{}, false
}

// deduplicateSameSKU removes duplicate decisions from the same
// agent for the same SKU, keeping the most recent.
func deduplicateSameSKU(decisions []PricingFulfilmentDecision) []PricingFulfilmentDecision {
	seen := make(map[string]int, len(decisions))
	result := make([]PricingFulfilmentDecision, 0, len(decisions))
	for _, d := range decisions {
		if idx, exists := seen[d.AgentName]; exists {
			if d.ProposedAt.After(result[idx].ProposedAt) {
				result[idx] = d
			}
			continue
		}
		seen[d.AgentName] = len(result)
		result = append(result, d)
	}
	return result
}

func (pfc *PricingFulfilmentCoordinator) buildEvent(tenantID, sku string, winner PricingFulfilmentDecision, resolution, constraint string) CoordinationDecisionEvent {
	return CoordinationDecisionEvent{
		TenantID:       tenantID,
		SKU:            sku,
		WinnerAgent:    winner.AgentName,
		Resolution:     resolution,
		Reason:         winner.Reason,
		ConstraintUsed: constraint,
		Timestamp:      pfc.now(),
	}
}

func (pfc *PricingFulfilmentCoordinator) publishEvent(ctx context.Context, event CoordinationDecisionEvent) {
	if pfc.publisher == nil {
		return
	}
	_ = pfc.publisher(ctx, event)
}
