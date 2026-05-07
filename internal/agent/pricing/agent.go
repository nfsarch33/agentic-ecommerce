package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	orchestrator "github.com/nfsarch33/agentic-ecommerce/internal/agent"
)

var ErrInvalidCost = errors.New("cost_cents must be greater than zero")

type Agent struct{}

type Strategy string

const (
	StrategyMarginBased      Strategy = "margin_based"
	StrategyCompetitionBased Strategy = "competition_based"
	StrategyDemandBased      Strategy = "demand_based"
)

type Request struct {
	SKU                   string   `json:"sku"`
	Strategy              Strategy `json:"strategy,omitempty"`
	CostCents             int      `json:"cost_cents"`
	ShippingCents         int      `json:"shipping_cents"`
	CurrentPriceCents     int      `json:"current_price_cents"`
	CompetitorPricesCents []int    `json:"competitor_prices_cents"`
	TargetMarginPct       float64  `json:"target_margin_pct"`
	MinimumMarginPct      float64  `json:"minimum_margin_pct"`
	DemandScore           float64  `json:"demand_score,omitempty"`
	DemandLiftPct         float64  `json:"demand_lift_pct,omitempty"`
}

type Result struct {
	SKU                    string   `json:"sku"`
	Strategy               Strategy `json:"strategy"`
	RecommendedPriceCents  int      `json:"recommended_price_cents"`
	GrossMarginPct         float64  `json:"gross_margin_pct"`
	CompetitorAverageCents int      `json:"competitor_average_cents"`
	Reasons                []string `json:"reasons"`
}

func NewAgent() *Agent {
	return &Agent{}
}

func (a *Agent) Descriptor() orchestrator.Descriptor {
	return orchestrator.Descriptor{
		ID:           "pricing",
		Name:         "Pricing Agent",
		Description:  "Computes deterministic recommended price and margin using configurable strategy inputs.",
		Capabilities: []string{"price_recommendation", "margin_analysis", "competition_pricing", "demand_pricing_stub"},
	}
}

func (a *Agent) Run(ctx context.Context, task orchestrator.Task) (orchestrator.RunResult, error) {
	var req Request
	if err := decodePayload(task.Payload, &req); err != nil {
		return orchestrator.RunResult{}, err
	}
	result, err := a.Recommend(ctx, req)
	if err != nil {
		return orchestrator.RunResult{}, err
	}
	return orchestrator.RunResult{Payload: mustMap(result)}, nil
}

func (a *Agent) Recommend(ctx context.Context, req Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	totalCost := req.CostCents + req.ShippingCents
	if totalCost <= 0 {
		return Result{}, ErrInvalidCost
	}
	targetMargin := req.TargetMarginPct
	if targetMargin <= 0 || targetMargin >= 0.95 {
		targetMargin = 0.4
	}
	minimumMargin := req.MinimumMarginPct
	if minimumMargin <= 0 || minimumMargin >= targetMargin {
		minimumMargin = targetMargin * 0.75
	}

	minimumPrice := priceForMargin(totalCost, minimumMargin)
	competitorAverage := average(req.CompetitorPricesCents)
	strategy := normalizeStrategy(req.Strategy)
	recommended, reasons := recommendByStrategy(strategy, totalCost, targetMargin, competitorAverage, req)
	if recommended < minimumPrice {
		recommended = minimumPrice
		reasons = append(reasons, "minimum_margin_floor_applied")
	}
	return Result{
		SKU:                    req.SKU,
		Strategy:               strategy,
		RecommendedPriceCents:  recommended,
		GrossMarginPct:         round4(float64(recommended-totalCost) / float64(recommended)),
		CompetitorAverageCents: competitorAverage,
		Reasons:                reasons,
	}, nil
}

func normalizeStrategy(strategy Strategy) Strategy {
	switch strategy {
	case StrategyMarginBased, StrategyCompetitionBased, StrategyDemandBased:
		return strategy
	default:
		return StrategyCompetitionBased
	}
}

func recommendByStrategy(strategy Strategy, totalCost int, targetMargin float64, competitorAverage int, req Request) (int, []string) {
	switch strategy {
	case StrategyMarginBased:
		return priceForMargin(totalCost, targetMargin), []string{"strategy_margin_based", "target_margin_floor_applied"}
	case StrategyDemandBased:
		lift := req.DemandLiftPct
		if lift <= 0 {
			lift = 0.05
		}
		demandAdjustedMargin := targetMargin
		if req.DemandScore >= 0.75 {
			demandAdjustedMargin += lift
		}
		if demandAdjustedMargin >= 0.95 {
			demandAdjustedMargin = 0.94
		}
		return priceForMargin(totalCost, demandAdjustedMargin), []string{"strategy_demand_based", "demand_signal_stub_applied", "target_margin_floor_applied"}
	default:
		targetPrice := priceForMargin(totalCost, targetMargin)
		if competitorAverage <= 0 {
			return targetPrice, []string{"strategy_competition_based", "target_margin_floor_applied"}
		}
		competitivePrice := charmPrice(competitorAverage - 100)
		if competitivePrice > targetPrice {
			return competitivePrice, []string{"strategy_competition_based", "positioned_below_competitor_average"}
		}
		return targetPrice, []string{"strategy_competition_based", "target_margin_floor_applied"}
	}
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

func priceForMargin(costCents int, margin float64) int {
	return charmPrice(int(math.Ceil(float64(costCents) / (1 - margin))))
}

func charmPrice(price int) int {
	if price <= 0 {
		return 0
	}
	return (price/100)*100 + 95
}

func average(values []int) int {
	if len(values) == 0 {
		return 0
	}
	sum := 0
	count := 0
	for _, value := range values {
		if value > 0 {
			sum += value
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return int(math.Round(float64(sum) / float64(count)))
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
