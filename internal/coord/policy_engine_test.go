package coord

import (
	"testing"
	"time"
)

func TestWeightedPriority_HighestPriorityWins(t *testing.T) {
	t.Parallel()
	wp := NewWeightedPriority(DefaultAgentPriorities())
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	decisions := []AgentDecision{
		{AgentName: "fulfilment", TenantID: "t1", SKU: "sku-1", Action: ActionKindInventoryDeplete, ProposedAt: ts},
		{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, ProposedAt: ts},
	}

	winner := wp.Resolve(decisions)
	if winner.AgentName != "pricing" {
		t.Fatalf("winner = %s, want pricing (weight 0.8 > 0.6)", winner.AgentName)
	}
}

func TestWeightedPriority_TieBreakByRecency(t *testing.T) {
	t.Parallel()
	wp := NewWeightedPriority([]AgentPriority{
		{AgentName: "agent_a", Weight: 0.5},
		{AgentName: "agent_b", Weight: 0.5},
	})
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	decisions := []AgentDecision{
		{AgentName: "agent_a", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, ProposedAt: ts},
		{AgentName: "agent_b", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, ProposedAt: ts.Add(time.Second)},
	}

	winner := wp.Resolve(decisions)
	if winner.AgentName != "agent_b" {
		t.Fatalf("winner = %s, want agent_b (more recent at equal weight)", winner.AgentName)
	}
}

func TestWeightedPriority_ThreeAgentConflict(t *testing.T) {
	t.Parallel()
	wp := NewWeightedPriority(DefaultAgentPriorities())
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	decisions := []AgentDecision{
		{AgentName: "content", TenantID: "t1", SKU: "sku-1", Action: ActionKindContentRefresh, ProposedAt: ts},
		{AgentName: "fulfilment", TenantID: "t1", SKU: "sku-1", Action: ActionKindInventoryDeplete, ProposedAt: ts},
		{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, ProposedAt: ts},
	}

	winner := wp.Resolve(decisions)
	if winner.AgentName != "pricing" {
		t.Fatalf("winner = %s, want pricing (highest weight 0.8)", winner.AgentName)
	}
}

func TestWeightedPriority_ResolveConflicts(t *testing.T) {
	t.Parallel()
	wp := NewWeightedPriority(DefaultAgentPriorities())
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	conflicts := []Conflict{
		{
			TenantID: "t1", SKU: "sku-1",
			Decisions: []AgentDecision{
				{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, ProposedAt: ts},
				{AgentName: "fulfilment", TenantID: "t1", SKU: "sku-1", Action: ActionKindInventoryDeplete, ProposedAt: ts},
			},
		},
	}

	res, err := wp.ResolveConflicts(conflicts)
	if err != nil {
		t.Fatalf("ResolveConflicts: %v", err)
	}
	if res.PolicyName != "weighted_priority" {
		t.Fatalf("policy = %s, want weighted_priority", res.PolicyName)
	}
	if len(res.Outcomes) != 1 {
		t.Fatalf("outcomes = %d, want 1", len(res.Outcomes))
	}
	if res.Outcomes[0].Chosen.AgentName != "pricing" {
		t.Fatalf("chosen = %s, want pricing", res.Outcomes[0].Chosen.AgentName)
	}
}

func TestWeightedPriority_UnknownAgentDefaultsToZero(t *testing.T) {
	t.Parallel()
	wp := NewWeightedPriority([]AgentPriority{
		{AgentName: "pricing", Weight: 0.8},
	})
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	decisions := []AgentDecision{
		{AgentName: "unknown_agent", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, ProposedAt: ts},
		{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, ProposedAt: ts},
	}

	winner := wp.Resolve(decisions)
	if winner.AgentName != "pricing" {
		t.Fatalf("winner = %s, want pricing (known agent wins over unknown)", winner.AgentName)
	}
}

func TestWeightedPriority_EmptyDecisions(t *testing.T) {
	t.Parallel()
	wp := NewWeightedPriority(DefaultAgentPriorities())
	winner := wp.Resolve(nil)
	if winner.AgentName != "" {
		t.Fatalf("winner = %s, want empty for nil input", winner.AgentName)
	}
}
