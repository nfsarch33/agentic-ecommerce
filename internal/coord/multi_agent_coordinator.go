// Package coord seeds the v3.5.1 Existing #4 MADRL multi-agent
// coordination foundation per the ADR-028 v4 roadmap "Existing #4"
// item. The seed scope is intentionally narrow:
//
//   - A small Coordinator port that takes a slice of AgentDecision
//     proposals (e.g. pricing agent says "raise price 10%" while
//     fulfilment agent says "deplete inventory fast at -5%") and
//     returns a single CoordinatedAction.
//   - Conflict detection across decisions targeting the same SKU.
//   - "last-write-wins" resolution stub. The full MADRL/CTDE policy
//     (Centralised Training, Decentralised Execution) is deferred
//     to post-v4.0.0 per the v3.5.1 plan; this package keeps the
//     seam clean so the future RL learner can swap in via the
//     ResolutionPolicy interface.
//   - Typed CoordinationConflict so the EC-9-5 operator alert
//     centre + dashboards can pivot per (agent_a, agent_b) pair.
//   - Prometheus-shaped metrics hook (interface only; the v3.5.0
//     observability spine wires the concrete adapter when this
//     coordinator graduates from seed to production).
//
// Reuse evidence:
//   - The KPI hook pattern mirrors v3.5.0 EC-6-1
//     SupplierCostKPISample + EC-6-3 PricingAgent.
//   - The typed sentinel error pattern follows v3.4.0 EC-4-3
//     channel.router.go.
//   - The "config + nil-safe defaults" wiring follows the v3.5.0
//     EC-7-2 dropship_agent constructor.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4 -- 9-sprint streak target):
//
//   - Coordinate (envelope; iterate proposals; per-SKU group)
//   - groupProposalsBySKU (pure)
//   - detectConflict (pure; returns []CoordinationConflict)
//   - resolveLastWriteWins (pure)
//   - emitConflictMetrics (hook fan-out)
//
// Each helper stays under cyclomatic 6.
//
// HARD RULES honoured:
//   - Pure Go logic; NO chromedp / RL deps. The seed is computation
//   - interface only.
//   - Tenant-aware: every conflict carries TenantID + the (agent_a,
//     agent_b) tuple.
//   - Errors typed + %w-wrapped via package sentinels.
//
// Cite skill: go-clean-architecture (port + adapter; the resolver
// depends on ResolutionPolicy not on a concrete RL learner).
package coord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// MADRL coordination typed sentinels.
var (
	// ErrCoordinatorUnconfigured is returned by NewCoordinator when
	// a required dependency (TenantID) is missing.
	ErrCoordinatorUnconfigured = errors.New("coord: coordinator unconfigured")

	// ErrCoordinatorClosed is returned by Coordinate after Close.
	ErrCoordinatorClosed = errors.New("coord: coordinator closed")

	// ErrInvalidAgentDecision is returned when an input decision
	// fails the validate gate (missing AgentName / SKU / Action).
	ErrInvalidAgentDecision = errors.New("coord: invalid agent decision")

	// ErrTenantMismatch is returned when an input decision carries
	// a TenantID different from the coordinator's tenant.
	ErrTenantMismatch = errors.New("coord: tenant mismatch")
)

// ActionKind enumerates the orthogonality classes the v3.5.1 seed
// coordinator understands. The list is intentionally small; the
// future RL policy will extend it via a registry on the
// ResolutionPolicy port.
type ActionKind string

// ActionKind enum values. Pricing/Fulfilment are the two v3.5.0
// agents that surface conflict on shared SKUs; Content/Sourcing
// are listed for forward compatibility (the v3.5.x sprints will
// emit them as orthogonal actions).
const (
	ActionKindPriceChange      ActionKind = "price_change"
	ActionKindInventoryDeplete ActionKind = "inventory_deplete"
	ActionKindInventoryHold    ActionKind = "inventory_hold"
	ActionKindContentRefresh   ActionKind = "content_refresh"
	ActionKindSourcingRefresh  ActionKind = "sourcing_refresh"
)

// Conflicts returns true when two action kinds operate on the same
// SKU in a way that requires the coordinator to choose. The seed
// considers price_change <-> inventory_deplete the canonical
// pricing-vs-fulfilment conflict (raising price slows velocity;
// depleting inventory fast wants velocity high). Other pairs are
// considered orthogonal for the v3.5.1 seed.
func (k ActionKind) Conflicts(other ActionKind) bool {
	if k == other {
		return true
	}
	if (k == ActionKindPriceChange && other == ActionKindInventoryDeplete) ||
		(k == ActionKindInventoryDeplete && other == ActionKindPriceChange) {
		return true
	}
	if (k == ActionKindInventoryDeplete && other == ActionKindInventoryHold) ||
		(k == ActionKindInventoryHold && other == ActionKindInventoryDeplete) {
		return true
	}
	return false
}

// AgentDecision is the per-agent proposal submitted to the
// coordinator. Pure value type; no IO. ProposedAt drives the
// last-write-wins tie-breaker.
type AgentDecision struct {
	AgentName  string
	TenantID   string
	SKU        string
	Action     ActionKind
	DeltaPct   float64
	Reason     string
	ProposedAt time.Time
}

// CoordinatedAction is the coordinator's single chosen output for
// a SKU. The Conflicts slice surfaces every detected pair so
// downstream observability (the EC-9-5 alert centre) can render
// the resolution context.
type CoordinatedAction struct {
	TenantID  string
	SKU       string
	Chosen    AgentDecision
	Conflicts []CoordinationConflict
	DecidedAt time.Time
}

// CoordinationConflict is the typed (agent_a, agent_b) tuple per
// detected conflict. Resolution captures the resolver decision
// (e.g. "last_write_wins") so dashboards can pivot. AgentA + AgentB
// are sorted alphabetically so the metric labels stay stable
// regardless of input order.
type CoordinationConflict struct {
	TenantID   string
	SKU        string
	AgentA     string
	AgentB     string
	Resolution string
}

// CoordinatorMetrics is the small port the coordinator emits the
// ec_coord_conflicts_total counter through. Mirrors the v3.5.0
// EC-7-2 DropshipAgentMetrics pattern -- one method per metric.
type CoordinatorMetrics interface {
	// RecordCoordinationConflict increments the
	// ec_coord_conflicts_total counter with labels {tenant_id,
	// agent_a, agent_b, resolution}. Cardinality budget: ~50
	// series per tenant (5 agents x 4 conflicting pairs x ~2
	// resolution policies x ~10 tenants stays well under the
	// per-binary 10_000 cap).
	RecordCoordinationConflict(tenantID, agentA, agentB, resolution string)
}

// CoordinatorKPISample is the EvoMap KPI hook payload emitted for
// every detected conflict. Pure value type so cmd/* drivers can
// pump conflict-rate aggregates.
type CoordinatorKPISample struct {
	TenantID   string
	SKU        string
	AgentA     string
	AgentB     string
	Resolution string
}

// CoordinatorKPIHook is the optional EvoMap emission hook.
type CoordinatorKPIHook func(CoordinatorKPISample)

// ResolutionPolicy is the seam the future MADRL/CTDE learner will
// implement. The v3.5.1 seed ships LastWriteWins; the production
// learner will plug in a Centralised-Training Decentralised-
// Execution policy without touching the Coordinator surface.
type ResolutionPolicy interface {
	// Name identifies the policy in metric labels and log lines
	// (e.g. "last_write_wins", "madrl_v1").
	Name() string
	// Resolve picks one decision from the conflicting set. The
	// implementation MUST NOT mutate the input slice.
	Resolve(decisions []AgentDecision) AgentDecision
}

// LastWriteWins is the v3.5.1 seed policy -- the most recent
// ProposedAt wins. Ties break on AgentName lexical order so the
// outcome is deterministic across replays.
type LastWriteWins struct{}

// Name satisfies ResolutionPolicy.
func (LastWriteWins) Name() string { return "last_write_wins" }

// Resolve satisfies ResolutionPolicy.
func (LastWriteWins) Resolve(decisions []AgentDecision) AgentDecision {
	if len(decisions) == 0 {
		return AgentDecision{}
	}
	winner := decisions[0]
	for _, d := range decisions[1:] {
		if d.ProposedAt.After(winner.ProposedAt) {
			winner = d
			continue
		}
		if d.ProposedAt.Equal(winner.ProposedAt) && d.AgentName < winner.AgentName {
			winner = d
		}
	}
	return winner
}

// CoordinatorConfig wires a Coordinator. TenantID is REQUIRED.
// Policy defaults to LastWriteWins. Now defaults to time.Now().UTC().
type CoordinatorConfig struct {
	TenantID string
	Policy   ResolutionPolicy
	Metrics  CoordinatorMetrics
	KPIHook  CoordinatorKPIHook
	Now      func() time.Time
}

// Coordinator is the v3.5.1 EC roadmap "Existing #4" MADRL seed.
type Coordinator struct {
	cfg    CoordinatorConfig
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewCoordinator constructs a coordinator. The closure of the
// surface stays small so the future MADRL learner can replace
// `Policy` without rewiring the cmd/* composition root.
func NewCoordinator(logger *slog.Logger, cfg CoordinatorConfig) (*Coordinator, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := validateCoordinatorConfig(cfg); err != nil {
		return nil, err
	}
	applyCoordinatorDefaults(&cfg)
	return &Coordinator{cfg: cfg, logger: logger}, nil
}

func validateCoordinatorConfig(cfg CoordinatorConfig) error {
	if strings.TrimSpace(cfg.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrCoordinatorUnconfigured)
	}
	return nil
}

func applyCoordinatorDefaults(cfg *CoordinatorConfig) {
	if cfg.Policy == nil {
		cfg.Policy = LastWriteWins{}
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
}

// Close marks the coordinator closed.
func (c *Coordinator) Close(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// PolicyName returns the active resolution policy name. Useful for
// dashboards + the EvoMap capsule.
func (c *Coordinator) PolicyName() string { return c.cfg.Policy.Name() }

// Coordinate runs the v3.5.1 seed pipeline:
// validate -> group by SKU -> detect conflict -> resolve.
//
// Returns ONE CoordinatedAction per distinct SKU. SKUs with a
// single decision pass through unchanged with empty Conflicts.
//
// Decomposition: per-SKU work splits into helpers so the loop
// body stays cyclomatic 4.
func (c *Coordinator) Coordinate(ctx context.Context, decisions []AgentDecision) ([]CoordinatedAction, error) {
	if err := c.guard(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.validateInputs(decisions); err != nil {
		return nil, err
	}
	groups := groupProposalsBySKU(decisions)
	out := make([]CoordinatedAction, 0, len(groups))
	for _, sku := range sortedSKUs(groups) {
		action := c.coordinateOne(sku, groups[sku])
		out = append(out, action)
	}
	return out, nil
}

// validateInputs runs the per-decision validate gate. Returns the
// first error so callers see a deterministic failure surface.
func (c *Coordinator) validateInputs(decisions []AgentDecision) error {
	for i, d := range decisions {
		if err := validateDecision(d); err != nil {
			return fmt.Errorf("%w: decision[%d]: %v", ErrInvalidAgentDecision, i, err)
		}
		if d.TenantID != c.cfg.TenantID {
			return fmt.Errorf("%w: decision[%d] tenant=%s coordinator=%s", ErrTenantMismatch, i, d.TenantID, c.cfg.TenantID)
		}
	}
	return nil
}

// coordinateOne runs the per-SKU pipeline (detect + resolve) and
// fans out the metric + KPI hook for every detected conflict.
func (c *Coordinator) coordinateOne(sku string, decisions []AgentDecision) CoordinatedAction {
	conflicts := detectConflict(c.cfg.TenantID, sku, decisions, c.cfg.Policy.Name())
	chosen := c.resolveDecisions(decisions)
	c.emitConflictMetrics(conflicts)
	return CoordinatedAction{
		TenantID:  c.cfg.TenantID,
		SKU:       sku,
		Chosen:    chosen,
		Conflicts: conflicts,
		DecidedAt: c.cfg.Now(),
	}
}

// resolveDecisions returns the policy's chosen decision. Pulled
// out so coordinateOne stays cyclomatic 1.
func (c *Coordinator) resolveDecisions(decisions []AgentDecision) AgentDecision {
	return c.cfg.Policy.Resolve(decisions)
}

// emitConflictMetrics fans the metrics + KPI hook for the
// detected conflicts. nil-safe so tests can omit either.
func (c *Coordinator) emitConflictMetrics(conflicts []CoordinationConflict) {
	for _, conflict := range conflicts {
		if c.cfg.Metrics != nil {
			c.cfg.Metrics.RecordCoordinationConflict(conflict.TenantID, conflict.AgentA, conflict.AgentB, conflict.Resolution)
		}
		if c.cfg.KPIHook != nil {
			c.cfg.KPIHook(CoordinatorKPISample{
				TenantID:   conflict.TenantID,
				SKU:        conflict.SKU,
				AgentA:     conflict.AgentA,
				AgentB:     conflict.AgentB,
				Resolution: conflict.Resolution,
			})
		}
	}
}

func (c *Coordinator) guard() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrCoordinatorClosed
	}
	return nil
}

// validateDecision enforces the per-decision contract. Pure;
// cyclomatic stays at 4 (one branch per invariant).
func validateDecision(d AgentDecision) error {
	if strings.TrimSpace(d.AgentName) == "" {
		return fmt.Errorf("agent_name required")
	}
	if strings.TrimSpace(d.TenantID) == "" {
		return fmt.Errorf("tenant_id required")
	}
	if strings.TrimSpace(d.SKU) == "" {
		return fmt.Errorf("sku required")
	}
	if strings.TrimSpace(string(d.Action)) == "" {
		return fmt.Errorf("action required")
	}
	return nil
}

// groupProposalsBySKU buckets decisions by SKU so the per-SKU
// pipeline can run independently. Pure; no IO.
func groupProposalsBySKU(decisions []AgentDecision) map[string][]AgentDecision {
	out := make(map[string][]AgentDecision)
	for _, d := range decisions {
		out[d.SKU] = append(out[d.SKU], d)
	}
	return out
}

// sortedSKUs returns the map keys in lexical order so iteration is
// deterministic across replays.
func sortedSKUs(groups map[string][]AgentDecision) []string {
	out := make([]string, 0, len(groups))
	for k := range groups {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// detectConflict returns the typed (agent_a, agent_b) tuple per
// pair of decisions whose Action values conflict per
// ActionKind.Conflicts. Each pair counted once; agents sorted.
// Pure; cyclomatic stays at 4.
func detectConflict(tenantID, sku string, decisions []AgentDecision, resolution string) []CoordinationConflict {
	if len(decisions) < 2 {
		return nil
	}
	out := make([]CoordinationConflict, 0)
	for i := 0; i < len(decisions); i++ {
		for j := i + 1; j < len(decisions); j++ {
			if !decisions[i].Action.Conflicts(decisions[j].Action) {
				continue
			}
			a, b := decisions[i].AgentName, decisions[j].AgentName
			if a > b {
				a, b = b, a
			}
			out = append(out, CoordinationConflict{
				TenantID:   tenantID,
				SKU:        sku,
				AgentA:     a,
				AgentB:     b,
				Resolution: resolution,
			})
		}
	}
	return out
}
