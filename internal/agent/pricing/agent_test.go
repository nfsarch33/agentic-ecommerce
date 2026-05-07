package pricing

import (
	"context"
	"testing"

	orchestrator "github.com/nfsarch33/agentic-ecommerce/internal/agent"
)

func TestRecommendComputesPriceAndMarginFromCostsAndCompetition(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	result, err := agent.Recommend(context.Background(), Request{
		SKU:                   "RB-SET",
		CostCents:             1800,
		ShippingCents:         250,
		CurrentPriceCents:     4995,
		CompetitorPricesCents: []int{4595, 4895, 5195},
		TargetMarginPct:       0.45,
		MinimumMarginPct:      0.32,
	})
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}

	if result.RecommendedPriceCents != 4795 {
		t.Fatalf("recommended price = %d, want 4795", result.RecommendedPriceCents)
	}
	if result.GrossMarginPct < 0.57 || result.GrossMarginPct > 0.58 {
		t.Fatalf("gross margin = %.4f, want about 0.5725", result.GrossMarginPct)
	}
	if result.CompetitorAverageCents != 4895 {
		t.Fatalf("competitor avg = %d, want 4895", result.CompetitorAverageCents)
	}
	if len(result.Reasons) == 0 {
		t.Fatal("expected structured pricing reasons")
	}
}

func TestPricingDescriptorAdvertisesMarginCapability(t *testing.T) {
	t.Parallel()

	descriptor := NewAgent().Descriptor()
	if descriptor.ID != "pricing" || descriptor.Name == "" {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	if !hasPricingCapability(descriptor.Capabilities, "margin_analysis") {
		t.Fatalf("capabilities = %v, want margin_analysis", descriptor.Capabilities)
	}
}

func TestPricingRunReturnsStructuredPayloadForScheduler(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	result, err := agent.Run(context.Background(), orchestrator.Task{Payload: map[string]any{
		"sku":                     "RB-SET",
		"cost_cents":              1800,
		"shipping_cents":          250,
		"current_price_cents":     4995,
		"competitor_prices_cents": []int{4595, 4895, 5195},
		"target_margin_pct":       0.45,
		"minimum_margin_pct":      0.32,
	}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Payload["sku"] != "RB-SET" || result.Payload["recommended_price_cents"] == nil {
		t.Fatalf("payload = %#v, want scheduler-safe pricing result", result.Payload)
	}
}

func TestPricingRunRejectsInvalidCost(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	_, err := agent.Run(context.Background(), orchestrator.Task{Payload: map[string]any{"sku": "RB-SET", "cost_cents": 0}})
	if err == nil {
		t.Fatal("expected invalid cost error")
	}
}

func hasPricingCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}
	return false
}
