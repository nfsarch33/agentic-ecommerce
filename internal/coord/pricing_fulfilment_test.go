package coord

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recordingEventPublisher struct {
	mu     sync.Mutex
	events []CoordinationDecisionEvent
}

func (r *recordingEventPublisher) publish(_ context.Context, event CoordinationDecisionEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

func (r *recordingEventPublisher) snapshot() []CoordinationDecisionEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CoordinationDecisionEvent, len(r.events))
	copy(out, r.events)
	return out
}

func newPricingFulfilmentHarness(t *testing.T) (*PricingFulfilmentCoordinator, *recordingEventPublisher) {
	t.Helper()
	publisher := &recordingEventPublisher{}
	pfc := NewPricingFulfilmentCoordinator(PricingFulfilmentConfig{
		Policy:    NewWeightedPriority(DefaultAgentPriorities()),
		Now:       func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
		Publisher: publisher.publish,
	})
	return pfc, publisher
}

func TestPricingFulfilment_PricingWinsHigherPriority(t *testing.T) {
	t.Parallel()
	pfc, publisher := newPricingFulfilmentHarness(t)
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	decisions := []PricingFulfilmentDecision{
		{AgentDecision: AgentDecision{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, DeltaPct: 0.10, Reason: "raise price", ProposedAt: ts}},
		{AgentDecision: AgentDecision{AgentName: "fulfilment", TenantID: "t1", SKU: "sku-1", Action: ActionKindInventoryDeplete, DeltaPct: -0.05, Reason: "deplete fast", ProposedAt: ts}},
	}

	winner, event, err := pfc.CoordinateDecisions(context.Background(), "t1", "sku-1", decisions)
	if err != nil {
		t.Fatalf("CoordinateDecisions: %v", err)
	}
	if winner.AgentName != "pricing" {
		t.Fatalf("winner = %s, want pricing", winner.AgentName)
	}
	if event.Resolution != "weighted_priority" {
		t.Fatalf("resolution = %s, want weighted_priority", event.Resolution)
	}
	events := publisher.snapshot()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
}

func TestPricingFulfilment_FulfilmentConstraintOverrides(t *testing.T) {
	t.Parallel()
	pfc, publisher := newPricingFulfilmentHarness(t)
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	decisions := []PricingFulfilmentDecision{
		{AgentDecision: AgentDecision{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, DeltaPct: -0.15, Reason: "discount", ProposedAt: ts}},
		{
			AgentDecision:  AgentDecision{AgentName: "fulfilment", TenantID: "t1", SKU: "sku-1", Action: ActionKindInventoryHold, Reason: "stockout detected", ProposedAt: ts},
			Constraint:     ConstraintSupplierStockout,
			ConstraintData: map[string]interface{}{"supplier": "1688"},
		},
	}

	winner, event, err := pfc.CoordinateDecisions(context.Background(), "t1", "sku-1", decisions)
	if err != nil {
		t.Fatalf("CoordinateDecisions: %v", err)
	}
	if winner.AgentName != "fulfilment" {
		t.Fatalf("winner = %s, want fulfilment (constraint override)", winner.AgentName)
	}
	if event.Resolution != "constraint_override" {
		t.Fatalf("resolution = %s, want constraint_override", event.Resolution)
	}
	if event.ConstraintUsed != string(ConstraintSupplierStockout) {
		t.Fatalf("constraint = %s, want %s", event.ConstraintUsed, ConstraintSupplierStockout)
	}
	events := publisher.snapshot()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
}

func TestPricingFulfilment_DedupSameSKU(t *testing.T) {
	t.Parallel()
	pfc, _ := newPricingFulfilmentHarness(t)
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	decisions := []PricingFulfilmentDecision{
		{AgentDecision: AgentDecision{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, DeltaPct: 0.05, Reason: "first", ProposedAt: ts}},
		{AgentDecision: AgentDecision{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, DeltaPct: 0.10, Reason: "second", ProposedAt: ts.Add(time.Second)}},
		{AgentDecision: AgentDecision{AgentName: "fulfilment", TenantID: "t1", SKU: "sku-1", Action: ActionKindInventoryDeplete, ProposedAt: ts}},
	}

	winner, _, err := pfc.CoordinateDecisions(context.Background(), "t1", "sku-1", decisions)
	if err != nil {
		t.Fatalf("CoordinateDecisions: %v", err)
	}
	if winner.AgentName != "pricing" {
		t.Fatalf("winner = %s, want pricing (deduped to latest)", winner.AgentName)
	}
	if winner.DeltaPct != 0.10 {
		t.Fatalf("delta = %f, want 0.10 (most recent pricing decision)", winner.DeltaPct)
	}
}

func TestPricingFulfilment_EventEmission(t *testing.T) {
	t.Parallel()
	pfc, publisher := newPricingFulfilmentHarness(t)
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	decisions := []PricingFulfilmentDecision{
		{AgentDecision: AgentDecision{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: ActionKindPriceChange, ProposedAt: ts}},
	}

	_, event, err := pfc.CoordinateDecisions(context.Background(), "t1", "sku-1", decisions)
	if err != nil {
		t.Fatalf("CoordinateDecisions: %v", err)
	}
	if event.TenantID != "t1" {
		t.Fatalf("event.TenantID = %s, want t1", event.TenantID)
	}
	if event.SKU != "sku-1" {
		t.Fatalf("event.SKU = %s, want sku-1", event.SKU)
	}
	events := publisher.snapshot()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
	if events[0].WinnerAgent != "pricing" {
		t.Fatalf("event winner = %s, want pricing", events[0].WinnerAgent)
	}
}
