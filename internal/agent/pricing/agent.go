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

type Request struct {
	SKU                   string  `json:"sku"`
	CostCents             int     `json:"cost_cents"`
	ShippingCents         int     `json:"shipping_cents"`
	CurrentPriceCents     int     `json:"current_price_cents"`
	CompetitorPricesCents []int   `json:"competitor_prices_cents"`
	TargetMarginPct       float64 `json:"target_margin_pct"`
	MinimumMarginPct      float64 `json:"minimum_margin_pct"`
}

type Result struct {
	SKU                    string   `json:"sku"`
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
		Description:  "Computes deterministic recommended price and margin from cost and competition inputs.",
		Capabilities: []string{"price_recommendation", "margin_analysis"},
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

	targetPrice := priceForMargin(totalCost, targetMargin)
	minimumPrice := priceForMargin(totalCost, minimumMargin)
	competitorAverage := average(req.CompetitorPricesCents)
	recommended := targetPrice
	reasons := []string{"target_margin_floor_applied"}
	if competitorAverage > 0 {
		competitivePrice := charmPrice(competitorAverage - 100)
		if competitivePrice > recommended {
			recommended = competitivePrice
			reasons = append(reasons, "positioned_below_competitor_average")
		}
	}
	if recommended < minimumPrice {
		recommended = minimumPrice
		reasons = append(reasons, "minimum_margin_floor_applied")
	}
	if req.CurrentPriceCents > 0 && recommended < req.CurrentPriceCents {
		reasons = append(reasons, "recommended_price_below_current")
	}

	return Result{
		SKU:                    req.SKU,
		RecommendedPriceCents:  recommended,
		GrossMarginPct:         round4(float64(recommended-totalCost) / float64(recommended)),
		CompetitorAverageCents: competitorAverage,
		Reasons:                reasons,
	}, nil
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
