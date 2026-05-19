package sourcing

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"

	orchestrator "github.com/nfsarch33/helixon-ec/internal/agent"
)

var ErrNoCandidates = errors.New("at least one sourcing candidate is required")

type Agent struct{}

type Candidate struct {
	SupplierID              string  `json:"supplier_id"`
	SKU                     string  `json:"sku"`
	UnitCostCents           int     `json:"unit_cost_cents"`
	ShippingCents           int     `json:"shipping_cents"`
	EstimatedSellPriceCents int     `json:"estimated_sell_price_cents"`
	LeadTimeDays            int     `json:"lead_time_days"`
	ReliabilityScore        float64 `json:"reliability_score"`
	DemandScore             float64 `json:"demand_score"`
	CompetitionScore        float64 `json:"competition_score"`
}

type Request struct {
	Candidates []Candidate `json:"candidates"`
}

type CandidateScore struct {
	SupplierID     string  `json:"supplier_id"`
	SKU            string  `json:"sku"`
	Score          float64 `json:"score"`
	GrossMarginPct float64 `json:"gross_margin_pct"`
	LeadTimeDays   int     `json:"lead_time_days"`
}

type Result struct {
	TopCandidate *CandidateScore  `json:"top_candidate,omitempty"`
	Scores       []CandidateScore `json:"scores"`
}

func NewAgent() *Agent {
	return &Agent{}
}

func (a *Agent) Descriptor() orchestrator.Descriptor {
	return orchestrator.Descriptor{
		ID:           "sourcing",
		Name:         "Sourcing Agent",
		Description:  "Scores supplier candidates using deterministic cost, demand, competition, and reliability inputs.",
		Capabilities: []string{"candidate_scoring", "opportunity_ranking"},
	}
}

func (a *Agent) Run(ctx context.Context, task orchestrator.Task) (orchestrator.RunResult, error) {
	var req Request
	if err := decodePayload(task.Payload, &req); err != nil {
		return orchestrator.RunResult{}, err
	}
	result, err := a.Score(ctx, req)
	if err != nil {
		return orchestrator.RunResult{}, err
	}
	return orchestrator.RunResult{Payload: mustMap(result)}, nil
}

func (a *Agent) Score(ctx context.Context, req Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(req.Candidates) == 0 {
		return Result{}, ErrNoCandidates
	}
	scores := make([]CandidateScore, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		totalCost := candidate.UnitCostCents + candidate.ShippingCents
		margin := grossMargin(totalCost, candidate.EstimatedSellPriceCents)
		leadTimeScore := clamp01(1 - float64(candidate.LeadTimeDays)/30)
		competitionOpportunity := clamp01(1 - candidate.CompetitionScore)
		score := 100 * (0.35*clamp01(candidate.ReliabilityScore) +
			0.25*clamp01(candidate.DemandScore) +
			0.2*clamp01(margin) +
			0.1*competitionOpportunity +
			0.1*leadTimeScore)
		scores = append(scores, CandidateScore{
			SupplierID:     candidate.SupplierID,
			SKU:            candidate.SKU,
			Score:          round2(score),
			GrossMarginPct: round4(margin),
			LeadTimeDays:   candidate.LeadTimeDays,
		})
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Score == scores[j].Score {
			return scores[i].SupplierID < scores[j].SupplierID
		}
		return scores[i].Score > scores[j].Score
	})
	top := scores[0]
	return Result{TopCandidate: &top, Scores: scores}, nil
}

func decodePayload(payload map[string]any, out any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func mustMap(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func grossMargin(costCents, sellPriceCents int) float64 {
	if sellPriceCents <= 0 {
		return 0
	}
	return float64(sellPriceCents-costCents) / float64(sellPriceCents)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
