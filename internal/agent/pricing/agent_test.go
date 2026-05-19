package pricing

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	orchestrator "github.com/nfsarch33/helixon-ec/internal/agent"
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

func TestRecommendStrategyGoldenFiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		request Request
		golden  string
	}{
		{
			name: "margin based",
			request: Request{
				SKU:                   "RB-SET",
				Strategy:              StrategyMarginBased,
				CostCents:             1800,
				ShippingCents:         250,
				CurrentPriceCents:     4995,
				CompetitorPricesCents: []int{4595, 4895, 5195},
				TargetMarginPct:       0.45,
				MinimumMarginPct:      0.32,
				DemandScore:           0.82,
			},
			golden: "margin_based.golden.json",
		},
		{
			name: "competition based",
			request: Request{
				SKU:                   "RB-SET",
				Strategy:              StrategyCompetitionBased,
				CostCents:             1800,
				ShippingCents:         250,
				CurrentPriceCents:     4995,
				CompetitorPricesCents: []int{4595, 4895, 5195},
				TargetMarginPct:       0.45,
				MinimumMarginPct:      0.32,
				DemandScore:           0.82,
			},
			golden: "competition_based.golden.json",
		},
		{
			name: "demand based stub",
			request: Request{
				SKU:                   "RB-SET",
				Strategy:              StrategyDemandBased,
				CostCents:             1800,
				ShippingCents:         250,
				CurrentPriceCents:     4995,
				CompetitorPricesCents: []int{4595, 4895, 5195},
				TargetMarginPct:       0.45,
				MinimumMarginPct:      0.32,
				DemandScore:           0.82,
			},
			golden: "demand_based.golden.json",
		},
		{
			name: "low margin floor edge",
			request: Request{
				SKU:                   "RB-SET",
				Strategy:              StrategyCompetitionBased,
				CostCents:             4600,
				ShippingCents:         250,
				CurrentPriceCents:     4995,
				CompetitorPricesCents: []int{4895, 4995, 5095},
				TargetMarginPct:       0.4,
				MinimumMarginPct:      0.32,
			},
			golden: "low_margin_floor.golden.json",
		},
	}

	agent := NewAgent()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := agent.Recommend(context.Background(), tc.request)
			if err != nil {
				t.Fatalf("recommend: %v", err)
			}
			assertGoldenJSON(t, tc.golden, result)
		})
	}
}

func TestRecommendInvalidCostGoldenFile(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	_, err := agent.Recommend(context.Background(), Request{
		SKU:           "RB-SET",
		Strategy:      StrategyMarginBased,
		CostCents:     -1200,
		ShippingCents: 100,
	})
	if err == nil {
		t.Fatal("Recommend accepted negative total cost")
	}
	assertGoldenJSON(t, "negative_cost_error.golden.json", map[string]any{"error": err.Error()})
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

func assertGoldenJSON(t *testing.T, filename string, value any) {
	t.Helper()

	got, err := json.MarshalIndent(mustMap(value), "", "  ")
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	got = append(got, '\n')
	want, err := os.ReadFile(filepath.Join("testdata", filename))
	if err != nil {
		t.Fatalf("read golden %s: %v", filename, err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", filename, want, got)
	}
}
