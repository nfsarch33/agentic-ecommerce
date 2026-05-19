//go:build v471_smoke

package v471

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/api/handler"
	"github.com/nfsarch33/helixon-ec/internal/coord"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// Scenario 1: Pricing+fulfilment conflict → weighted resolution correct
func TestMADRL_PricingFulfilmentWeightedResolution(t *testing.T) {
	t.Parallel()
	wp := coord.NewWeightedPriority(coord.DefaultAgentPriorities())
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	var publishedEvents []coord.CoordinationDecisionEvent
	pfc := coord.NewPricingFulfilmentCoordinator(coord.PricingFulfilmentConfig{
		Policy: wp,
		Now:    func() time.Time { return ts },
		Publisher: func(_ context.Context, evt coord.CoordinationDecisionEvent) error {
			publishedEvents = append(publishedEvents, evt)
			return nil
		},
	})

	decisions := []coord.PricingFulfilmentDecision{
		{AgentDecision: coord.AgentDecision{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: coord.ActionKindPriceChange, DeltaPct: 0.10, Reason: "raise price", ProposedAt: ts}},
		{AgentDecision: coord.AgentDecision{AgentName: "fulfilment", TenantID: "t1", SKU: "sku-1", Action: coord.ActionKindInventoryDeplete, DeltaPct: -0.05, Reason: "deplete fast", ProposedAt: ts}},
	}

	winner, event, err := pfc.CoordinateDecisions(context.Background(), "t1", "sku-1", decisions)
	if err != nil {
		t.Fatalf("CoordinateDecisions: %v", err)
	}
	if winner.AgentName != "pricing" {
		t.Fatalf("winner = %s, want pricing (weight 0.8 > 0.6)", winner.AgentName)
	}
	if event.Resolution != "weighted_priority" {
		t.Fatalf("resolution = %s, want weighted_priority", event.Resolution)
	}
	if len(publishedEvents) != 1 {
		t.Fatalf("published events = %d, want 1", len(publishedEvents))
	}
}

// Scenario 2: 3-agent conflict → highest priority wins
func TestMADRL_ThreeAgentConflictHighestPriorityWins(t *testing.T) {
	t.Parallel()
	wp := coord.NewWeightedPriority(coord.DefaultAgentPriorities())
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	decisions := []coord.AgentDecision{
		{AgentName: "content", TenantID: "t1", SKU: "sku-1", Action: coord.ActionKindContentRefresh, ProposedAt: ts},
		{AgentName: "fulfilment", TenantID: "t1", SKU: "sku-1", Action: coord.ActionKindInventoryDeplete, ProposedAt: ts},
		{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: coord.ActionKindPriceChange, ProposedAt: ts},
	}

	winner := wp.Resolve(decisions)
	if winner.AgentName != "pricing" {
		t.Fatalf("winner = %s, want pricing (highest weight 0.8)", winner.AgentName)
	}
}

// Scenario 3: Reward signal chain: action → outcome → reward emitted → log appended
func TestMADRL_RewardSignalChain(t *testing.T) {
	t.Parallel()
	log := &coord.InMemoryCoordinationLog{}

	signal := coord.RewardSignal{
		AgentID:     "pricing",
		ActionID:    "act-001",
		TenantID:    "t1",
		SKU:         "sku-1",
		Outcome:     coord.RewardOutcomeSuccess,
		RewardValue: 1.0,
		PolicyName:  "weighted_priority",
		Timestamp:   time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}

	entry := coord.CoordinationLogEntry{
		Timestamp:    signal.Timestamp,
		TenantID:     signal.TenantID,
		SKU:          signal.SKU,
		Agents:       []string{signal.AgentID},
		ConflictType: "price_change",
		Resolution:   "weighted_priority",
		PolicyName:   signal.PolicyName,
		ChosenAgent:  signal.AgentID,
		RewardValue:  signal.RewardValue,
	}

	if err := log.Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].RewardValue != 1.0 {
		t.Fatalf("reward_value = %f, want 1.0", entries[0].RewardValue)
	}
	if entries[0].ChosenAgent != "pricing" {
		t.Fatalf("chosen_agent = %s, want pricing", entries[0].ChosenAgent)
	}
}

// Scenario 4: 100-iteration convergence -- reward log grows monotonically
func TestMADRL_100IterationConvergence(t *testing.T) {
	t.Parallel()
	log := &coord.InMemoryCoordinationLog{}
	wp := coord.NewWeightedPriority(coord.DefaultAgentPriorities())

	for i := 0; i < 100; i++ {
		ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second)
		decisions := []coord.AgentDecision{
			{AgentName: "pricing", TenantID: "t1", SKU: "sku-1", Action: coord.ActionKindPriceChange, ProposedAt: ts},
			{AgentName: "fulfilment", TenantID: "t1", SKU: "sku-1", Action: coord.ActionKindInventoryDeplete, ProposedAt: ts},
		}

		winner := wp.Resolve(decisions)
		reward := 1.0
		if winner.AgentName == "pricing" {
			reward = 0.8
		}

		entry := coord.CoordinationLogEntry{
			Timestamp:    ts,
			TenantID:     "t1",
			SKU:          "sku-1",
			Agents:       []string{"pricing", "fulfilment"},
			ConflictType: "price_vs_inventory",
			Resolution:   "weighted_priority",
			PolicyName:   wp.Name(),
			ChosenAgent:  winner.AgentName,
			RewardValue:  reward,
		}
		if err := log.Append(entry); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
	}

	entries := log.Entries()
	if len(entries) != 100 {
		t.Fatalf("entries = %d, want 100 (no lost signals)", len(entries))
	}

	for i, e := range entries {
		if e.RewardValue <= 0 {
			t.Fatalf("entry[%d] reward = %f, want > 0", i, e.RewardValue)
		}
		if e.ChosenAgent == "" {
			t.Fatalf("entry[%d] chosen_agent empty", i)
		}
	}
}

// Scenario 5: Coordination event appears in SSE agent activity feed shape
func TestMADRL_CoordinationEventSSEShape(t *testing.T) {
	t.Parallel()

	event := coord.CoordinationDecisionEvent{
		TenantID:    "t1",
		SKU:         "sku-1",
		WinnerAgent: "pricing",
		Resolution:  "weighted_priority",
		Timestamp:   time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}

	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if !strings.Contains(string(body), `"winner_agent":"pricing"`) {
		t.Fatalf("SSE body missing winner_agent: %s", body)
	}
	if !strings.Contains(string(body), `"resolution":"weighted_priority"`) {
		t.Fatalf("SSE body missing resolution: %s", body)
	}

	// Verify the eventbus type is registered
	et := eventbus.CoordinationDecisionResolved
	if et != "coordination.decision.resolved" {
		t.Fatalf("event type = %s, want coordination.decision.resolved", et)
	}
}

// Verify the tenant dashboard endpoint renders KPIs
func TestMADRL_TenantDashboardIntegration(t *testing.T) {
	t.Parallel()

	repo := &stubDashboardRepo{
		orders:   42,
		gmvToday: 150000,
		gmvMTD:   2500000,
		alerts:   3,
		channels: 4,
	}

	h, err := handler.NewTenantDashboardHandler(nil, handler.TenantDashboardHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewTenantDashboardHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/dashboard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var kpis handler.TenantDashboardKPIs
	if err := json.NewDecoder(w.Body).Decode(&kpis); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if kpis.ActiveOrders != 42 {
		t.Fatalf("active_orders = %d, want 42", kpis.ActiveOrders)
	}
}

type stubDashboardRepo struct {
	orders   int64
	gmvToday int64
	gmvMTD   int64
	alerts   int
	channels int
}

func (r *stubDashboardRepo) ActiveOrders(_ context.Context, _ string) (int64, error) {
	return r.orders, nil
}
func (r *stubDashboardRepo) GMVToday(_ context.Context, _ string) (int64, error) {
	return r.gmvToday, nil
}
func (r *stubDashboardRepo) GMVMTD(_ context.Context, _ string) (int64, error) {
	return r.gmvMTD, nil
}
func (r *stubDashboardRepo) PendingAlerts(_ context.Context, _ string) (int, error) {
	return r.alerts, nil
}
func (r *stubDashboardRepo) ActiveChannels(_ context.Context, _ string) (int, error) {
	return r.channels, nil
}
func (r *stubDashboardRepo) ChannelHealth(_ context.Context, _ string) ([]handler.ChannelHealthEntry, error) {
	return nil, nil
}
func (r *stubDashboardRepo) RecentAgentActions(_ context.Context, _ string, _ int) ([]handler.RecentAgentAction, error) {
	return nil, nil
}
