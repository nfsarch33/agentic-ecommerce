// File scope: v3.5.1 Existing #4 MADRL coordinator RED tests.
// TDD-first per the v3.5.1 plan (Task 2; seed scope only).
//
// Acceptance per ADR-028 v4 roadmap "Existing #4":
//
//   - Coordinator detects conflicting decisions for the same SKU
//     (e.g. pricing agent says "raise price 10%" while fulfilment
//     agent says "deplete inventory fast at -5%").
//   - Orthogonal actions across the same SKU produce no conflict.
//   - LastWriteWins picks the most recent ProposedAt as the chosen
//     decision; ties break alphabetically on AgentName.
//   - The conflict event surface (KPI hook + Prometheus counter)
//     fires once per detected conflict.
//
// Pure Go; no chromedp / RL deps. The seed sets the seam the
// future MADRL/CTDE learner plugs into (ResolutionPolicy port).
package coord

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// recordingCoordMetrics captures every coord_conflict counter
// invocation so tests can pivot on (tenant, agent_a, agent_b,
// resolution).
type recordingCoordMetrics struct {
	mu    sync.Mutex
	calls []recordedCoordCall
}

type recordedCoordCall struct {
	tenantID, agentA, agentB, resolution string
}

func (r *recordingCoordMetrics) RecordCoordinationConflict(tenantID, agentA, agentB, resolution string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedCoordCall{tenantID, agentA, agentB, resolution})
}

func (r *recordingCoordMetrics) snapshot() []recordedCoordCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedCoordCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// newCoordHarness returns a coordinator + metrics + KPI hook
// wired against tenant-1 with the LastWriteWins policy.
func newCoordHarness(t *testing.T) (*Coordinator, *recordingCoordMetrics, *[]CoordinatorKPISample) {
	t.Helper()
	metrics := &recordingCoordMetrics{}
	var hookCalls []CoordinatorKPISample
	var hookMu sync.Mutex
	hook := func(s CoordinatorKPISample) {
		hookMu.Lock()
		defer hookMu.Unlock()
		hookCalls = append(hookCalls, s)
	}
	c, err := NewCoordinator(nil, CoordinatorConfig{
		TenantID: "tenant-1",
		Policy:   LastWriteWins{},
		Metrics:  metrics,
		KPIHook:  hook,
		Now:      func() time.Time { return time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c, metrics, &hookCalls
}

// TestCoordinator_DetectsConflictBetweenPricingAndFulfilment is
// the canonical v3.5.1 acceptance scenario: two agents propose
// contradictory actions for the same SKU.
func TestCoordinator_DetectsConflictBetweenPricingAndFulfilment(t *testing.T) {
	t.Parallel()
	c, _, _ := newCoordHarness(t)
	ts := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	pricing := AgentDecision{
		AgentName: "pricing", TenantID: "tenant-1", SKU: "sku-A",
		Action: ActionKindPriceChange, DeltaPct: 0.10, Reason: "raise 10%",
		ProposedAt: ts,
	}
	fulfilment := AgentDecision{
		AgentName: "fulfilment", TenantID: "tenant-1", SKU: "sku-A",
		Action: ActionKindInventoryDeplete, DeltaPct: -0.05, Reason: "deplete fast",
		ProposedAt: ts.Add(time.Second),
	}
	out, err := c.Coordinate(context.Background(), []AgentDecision{pricing, fulfilment})
	if err != nil {
		t.Fatalf("Coordinate: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("CoordinatedAction count = %d, want 1", len(out))
	}
	got := out[0]
	if got.SKU != "sku-A" {
		t.Fatalf("SKU = %s, want sku-A", got.SKU)
	}
	if len(got.Conflicts) != 1 {
		t.Fatalf("Conflicts = %+v, want 1 entry", got.Conflicts)
	}
	conflict := got.Conflicts[0]
	if conflict.AgentA != "fulfilment" || conflict.AgentB != "pricing" {
		t.Fatalf("conflict tuple = (%s, %s), want (fulfilment, pricing) sorted", conflict.AgentA, conflict.AgentB)
	}
	if conflict.Resolution != "last_write_wins" {
		t.Fatalf("resolution = %s, want last_write_wins", conflict.Resolution)
	}
}

// TestCoordinator_NoConflictForOrthogonalActions verifies that
// orthogonal actions on the same SKU pass through without
// triggering a conflict event.
func TestCoordinator_NoConflictForOrthogonalActions(t *testing.T) {
	t.Parallel()
	c, metrics, hookCalls := newCoordHarness(t)
	ts := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	cases := []AgentDecision{
		{AgentName: "content", TenantID: "tenant-1", SKU: "sku-O",
			Action: ActionKindContentRefresh, ProposedAt: ts},
		{AgentName: "sourcing", TenantID: "tenant-1", SKU: "sku-O",
			Action: ActionKindSourcingRefresh, ProposedAt: ts.Add(time.Second)},
	}
	out, err := c.Coordinate(context.Background(), cases)
	if err != nil {
		t.Fatalf("Coordinate: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("count = %d, want 1", len(out))
	}
	if len(out[0].Conflicts) != 0 {
		t.Fatalf("Conflicts = %+v, want empty (orthogonal actions)", out[0].Conflicts)
	}
	if metrics.snapshot() != nil && len(metrics.snapshot()) != 0 {
		t.Fatalf("metrics calls = %+v, want empty", metrics.snapshot())
	}
	if len(*hookCalls) != 0 {
		t.Fatalf("hook calls = %+v, want empty", *hookCalls)
	}
}

// TestCoordinator_LastWriteWinsResolution verifies that the
// LastWriteWins policy picks the most recent ProposedAt as the
// CoordinatedAction.Chosen.
func TestCoordinator_LastWriteWinsResolution(t *testing.T) {
	t.Parallel()
	c, _, _ := newCoordHarness(t)
	ts := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	cases := []AgentDecision{
		{AgentName: "pricing", TenantID: "tenant-1", SKU: "sku-L",
			Action: ActionKindPriceChange, DeltaPct: 0.05,
			ProposedAt: ts},
		{AgentName: "fulfilment", TenantID: "tenant-1", SKU: "sku-L",
			Action: ActionKindInventoryDeplete, DeltaPct: -0.10,
			ProposedAt: ts.Add(2 * time.Second)},
		{AgentName: "pricing-v2", TenantID: "tenant-1", SKU: "sku-L",
			Action: ActionKindPriceChange, DeltaPct: 0.15,
			ProposedAt: ts.Add(time.Second)},
	}
	out, err := c.Coordinate(context.Background(), cases)
	if err != nil {
		t.Fatalf("Coordinate: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("count = %d, want 1", len(out))
	}
	if out[0].Chosen.AgentName != "fulfilment" {
		t.Fatalf("chosen agent = %s, want fulfilment (latest ProposedAt)", out[0].Chosen.AgentName)
	}
	if out[0].Chosen.DeltaPct != -0.10 {
		t.Fatalf("chosen delta_pct = %.4f, want -0.10", out[0].Chosen.DeltaPct)
	}
}

// TestCoordinator_LastWriteWinsTieBreaksAlphabetically asserts
// the deterministic tie-breaker so replay traces stay stable.
func TestCoordinator_LastWriteWinsTieBreaksAlphabetically(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	policy := LastWriteWins{}
	winner := policy.Resolve([]AgentDecision{
		{AgentName: "pricing", TenantID: "tenant-1", SKU: "sku-T", Action: ActionKindPriceChange, ProposedAt: ts},
		{AgentName: "fulfilment", TenantID: "tenant-1", SKU: "sku-T", Action: ActionKindInventoryDeplete, ProposedAt: ts},
	})
	if winner.AgentName != "fulfilment" {
		t.Fatalf("tie winner = %s, want fulfilment (alphabetical)", winner.AgentName)
	}
}

// TestCoordinator_EmitsConflictEvent verifies the metrics +
// EvoMap KPI hook fire exactly once per detected conflict.
func TestCoordinator_EmitsConflictEvent(t *testing.T) {
	t.Parallel()
	c, metrics, hookCalls := newCoordHarness(t)
	ts := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	pricing := AgentDecision{
		AgentName: "pricing", TenantID: "tenant-1", SKU: "sku-E",
		Action: ActionKindPriceChange, DeltaPct: 0.20,
		ProposedAt: ts,
	}
	fulfilment := AgentDecision{
		AgentName: "fulfilment", TenantID: "tenant-1", SKU: "sku-E",
		Action: ActionKindInventoryDeplete, DeltaPct: -0.05,
		ProposedAt: ts.Add(time.Second),
	}
	if _, err := c.Coordinate(context.Background(), []AgentDecision{pricing, fulfilment}); err != nil {
		t.Fatalf("Coordinate: %v", err)
	}
	calls := metrics.snapshot()
	if len(calls) != 1 {
		t.Fatalf("metrics calls = %+v, want 1", calls)
	}
	want := recordedCoordCall{tenantID: "tenant-1", agentA: "fulfilment", agentB: "pricing", resolution: "last_write_wins"}
	if calls[0] != want {
		t.Fatalf("metrics call = %+v, want %+v", calls[0], want)
	}
	if len(*hookCalls) != 1 {
		t.Fatalf("hook calls = %+v, want 1", *hookCalls)
	}
	if (*hookCalls)[0].SKU != "sku-E" {
		t.Fatalf("hook sku = %s, want sku-E", (*hookCalls)[0].SKU)
	}
}

// TestCoordinator_RejectsTenantMismatch ensures cross-tenant
// decisions are rejected before any policy fires (tenant-aware
// per the v3.5.1 hard rule).
func TestCoordinator_RejectsTenantMismatch(t *testing.T) {
	t.Parallel()
	c, _, _ := newCoordHarness(t)
	ts := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	d := AgentDecision{
		AgentName: "pricing", TenantID: "tenant-other", SKU: "sku-X",
		Action: ActionKindPriceChange, ProposedAt: ts,
	}
	_, err := c.Coordinate(context.Background(), []AgentDecision{d})
	if !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("Coordinate: err=%v, want ErrTenantMismatch", err)
	}
}

// TestCoordinator_RejectsInvalidDecision verifies the validation
// gate fires for missing fields.
func TestCoordinator_RejectsInvalidDecision(t *testing.T) {
	t.Parallel()
	c, _, _ := newCoordHarness(t)
	cases := []struct {
		name string
		d    AgentDecision
	}{
		{name: "no_agent_name", d: AgentDecision{TenantID: "tenant-1", SKU: "sku-V", Action: ActionKindPriceChange}},
		{name: "no_sku", d: AgentDecision{AgentName: "pricing", TenantID: "tenant-1", Action: ActionKindPriceChange}},
		{name: "no_action", d: AgentDecision{AgentName: "pricing", TenantID: "tenant-1", SKU: "sku-V"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := c.Coordinate(context.Background(), []AgentDecision{tc.d})
			if !errors.Is(err, ErrInvalidAgentDecision) {
				t.Fatalf("%s: err=%v, want ErrInvalidAgentDecision", tc.name, err)
			}
		})
	}
}

// TestCoordinator_RejectsMissingTenant verifies the constructor
// guard.
func TestCoordinator_RejectsMissingTenant(t *testing.T) {
	t.Parallel()
	_, err := NewCoordinator(nil, CoordinatorConfig{})
	if !errors.Is(err, ErrCoordinatorUnconfigured) {
		t.Fatalf("NewCoordinator: err=%v, want ErrCoordinatorUnconfigured", err)
	}
}

// TestCoordinator_CoordinateAfterCloseRejects verifies the
// closed-state guard surfaces ErrCoordinatorClosed.
func TestCoordinator_CoordinateAfterCloseRejects(t *testing.T) {
	t.Parallel()
	c, _, _ := newCoordHarness(t)
	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := c.Coordinate(context.Background(), nil)
	if !errors.Is(err, ErrCoordinatorClosed) {
		t.Fatalf("Coordinate after Close: err=%v, want ErrCoordinatorClosed", err)
	}
}

// TestCoordinator_PolicyNamePassthrough surfaces the policy name
// for dashboards. Asserts the seed default + the swap-in surface.
func TestCoordinator_PolicyNamePassthrough(t *testing.T) {
	t.Parallel()
	c, _, _ := newCoordHarness(t)
	if c.PolicyName() != "last_write_wins" {
		t.Fatalf("PolicyName = %s, want last_write_wins", c.PolicyName())
	}
}

// TestActionKind_ConflictsMatrix asserts the conflict matrix
// stays stable across the v3.5.1 supported actions.
func TestActionKind_ConflictsMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b ActionKind
		want bool
	}{
		{ActionKindPriceChange, ActionKindInventoryDeplete, true},
		{ActionKindInventoryDeplete, ActionKindPriceChange, true},
		{ActionKindInventoryHold, ActionKindInventoryDeplete, true},
		{ActionKindContentRefresh, ActionKindSourcingRefresh, false},
		{ActionKindPriceChange, ActionKindContentRefresh, false},
		{ActionKindContentRefresh, ActionKindContentRefresh, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.a)+"_vs_"+string(tc.b), func(t *testing.T) {
			t.Parallel()
			if got := tc.a.Conflicts(tc.b); got != tc.want {
				t.Fatalf("%s.Conflicts(%s) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
