// Package coord -- v4.7.0 PolicyEngine interface + WeightedPriority
// resolution policy.
//
// The PolicyEngine is the pluggable seam for conflict resolution.
// WeightedPriority replaces the v3.5.1 LastWriteWins stub with a
// priority-weight-based resolver: each agent carries a Priority
// weight; conflicting actions resolve to the highest priority, with
// tie-breaking by recency (ProposedAt) then agent name.
//
// Decomposition discipline (HARD GATE: complex_fn=4):
//
//   - WeightedPriority.Resolve -> rankByPriority + breakTies
//   - rankByPriority           -> sort by weight descending (pure)
//   - breakTies                -> recency then lexical (pure)
package coord

import "sort"

// PolicyEngine is the v4.7.0 pluggable conflict resolution interface.
// Implementations receive detected conflicts and return a Resolution
// describing how each was resolved. The weighted-priority policy is
// the default; future RL policies plug in here.
type PolicyEngine interface {
	Resolve(conflicts []Conflict) (Resolution, error)
}

// Conflict describes a detected clash between two or more agent
// decisions targeting the same SKU. Pure value type.
type Conflict struct {
	TenantID  string
	SKU       string
	Decisions []AgentDecision
}

// Resolution is the output of a PolicyEngine.Resolve call. It maps
// each conflict to the chosen decision plus the policy name used.
type Resolution struct {
	PolicyName string
	Outcomes   []ResolutionOutcome
}

// ResolutionOutcome captures the resolution for a single conflict.
type ResolutionOutcome struct {
	SKU        string
	Chosen     AgentDecision
	Reason     string
	Candidates []AgentDecision
}

// AgentPriority maps an agent name to its priority weight. Higher
// values win. The default map covers the four v4.7.0 agent types.
type AgentPriority struct {
	AgentName string
	Weight    float64
}

// DefaultAgentPriorities returns the v4.7.0 priority weights per
// the plan: pricing=0.8, fulfilment=0.6, content=0.4, CS=0.3.
func DefaultAgentPriorities() []AgentPriority {
	return []AgentPriority{
		{AgentName: "pricing", Weight: 0.8},
		{AgentName: "fulfilment", Weight: 0.6},
		{AgentName: "content", Weight: 0.4},
		{AgentName: "customer_service", Weight: 0.3},
	}
}

// WeightedPriority resolves conflicts by agent priority weight.
// Highest weight wins; ties break by recency then agent name.
type WeightedPriority struct {
	weights map[string]float64
}

// NewWeightedPriority constructs a WeightedPriority resolver from
// the supplied priority map. Unknown agents default to weight 0.
func NewWeightedPriority(priorities []AgentPriority) *WeightedPriority {
	w := make(map[string]float64, len(priorities))
	for _, p := range priorities {
		w[p.AgentName] = p.Weight
	}
	return &WeightedPriority{weights: w}
}

// Name satisfies ResolutionPolicy for backward compatibility.
func (wp *WeightedPriority) Name() string { return "weighted_priority" }

// Resolve satisfies ResolutionPolicy. Picks the highest-priority
// agent from the conflicting set. Ties break by recency then name.
func (wp *WeightedPriority) Resolve(decisions []AgentDecision) AgentDecision {
	if len(decisions) == 0 {
		return AgentDecision{}
	}
	ranked := wp.rankByPriority(decisions)
	return ranked[0]
}

// ResolveConflicts satisfies PolicyEngine. Processes each conflict
// through the weighted-priority ranking.
func (wp *WeightedPriority) ResolveConflicts(conflicts []Conflict) (Resolution, error) {
	outcomes := make([]ResolutionOutcome, 0, len(conflicts))
	for _, c := range conflicts {
		chosen := wp.Resolve(c.Decisions)
		outcomes = append(outcomes, ResolutionOutcome{
			SKU:        c.SKU,
			Chosen:     chosen,
			Reason:     wp.resolveReason(chosen),
			Candidates: c.Decisions,
		})
	}
	return Resolution{PolicyName: wp.Name(), Outcomes: outcomes}, nil
}

func (wp *WeightedPriority) resolveReason(chosen AgentDecision) string {
	return "highest_priority:" + chosen.AgentName
}

// rankByPriority sorts decisions by weight descending, breaking
// ties by recency (later ProposedAt first) then agent name.
func (wp *WeightedPriority) rankByPriority(decisions []AgentDecision) []AgentDecision {
	ranked := make([]AgentDecision, len(decisions))
	copy(ranked, decisions)
	sort.SliceStable(ranked, func(i, j int) bool {
		return wp.less(ranked[i], ranked[j])
	})
	return ranked
}

func (wp *WeightedPriority) less(a, b AgentDecision) bool {
	wa := wp.weights[a.AgentName]
	wb := wp.weights[b.AgentName]
	if wa != wb {
		return wa > wb
	}
	return breakTies(a, b)
}

// breakTies resolves equal-priority decisions: most recent first,
// then alphabetical agent name for determinism.
func breakTies(a, b AgentDecision) bool {
	if !a.ProposedAt.Equal(b.ProposedAt) {
		return a.ProposedAt.After(b.ProposedAt)
	}
	return a.AgentName < b.AgentName
}
