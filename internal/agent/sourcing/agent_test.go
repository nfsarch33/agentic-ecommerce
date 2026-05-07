package sourcing

import (
	"context"
	"testing"

	orchestrator "github.com/nfsarch33/agentic-ecommerce/internal/agent"
)

func TestScoreCandidatesRanksDeterministicOpportunity(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	result, err := agent.Score(context.Background(), Request{Candidates: []Candidate{
		{
			SupplierID:              "slow-cheap",
			SKU:                     "RB-SET",
			UnitCostCents:           1200,
			ShippingCents:           300,
			EstimatedSellPriceCents: 3995,
			LeadTimeDays:            18,
			ReliabilityScore:        0.65,
			DemandScore:             0.7,
			CompetitionScore:        0.8,
		},
		{
			SupplierID:              "balanced",
			SKU:                     "RB-SET",
			UnitCostCents:           1500,
			ShippingCents:           250,
			EstimatedSellPriceCents: 4995,
			LeadTimeDays:            7,
			ReliabilityScore:        0.92,
			DemandScore:             0.82,
			CompetitionScore:        0.35,
		},
	}})
	if err != nil {
		t.Fatalf("score: %v", err)
	}

	if result.TopCandidate == nil || result.TopCandidate.SupplierID != "balanced" {
		t.Fatalf("top candidate = %#v", result.TopCandidate)
	}
	if len(result.Scores) != 2 {
		t.Fatalf("scores len = %d, want 2", len(result.Scores))
	}
	if result.Scores[0].Score <= result.Scores[1].Score {
		t.Fatalf("scores not sorted descending: %#v", result.Scores)
	}
	if result.Scores[0].GrossMarginPct <= 0 {
		t.Fatalf("expected positive gross margin: %#v", result.Scores[0])
	}
}

func TestSourcingDescriptorAdvertisesOpportunityRanking(t *testing.T) {
	t.Parallel()

	descriptor := NewAgent().Descriptor()
	if descriptor.ID != "sourcing" || descriptor.Name == "" {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	if !hasSourcingCapability(descriptor.Capabilities, "opportunity_ranking") {
		t.Fatalf("capabilities = %v, want opportunity_ranking", descriptor.Capabilities)
	}
}

func TestSourcingRunReturnsStructuredPayloadForScheduler(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	result, err := agent.Run(context.Background(), orchestrator.Task{Payload: map[string]any{
		"candidates": []Candidate{
			{
				SupplierID:              "balanced",
				SKU:                     "RB-SET",
				UnitCostCents:           1500,
				ShippingCents:           250,
				EstimatedSellPriceCents: 4995,
				LeadTimeDays:            7,
				ReliabilityScore:        0.92,
				DemandScore:             0.82,
				CompetitionScore:        0.35,
			},
		},
	}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	top, ok := result.Payload["top_candidate"].(map[string]any)
	if !ok || top["supplier_id"] != "balanced" {
		t.Fatalf("payload = %#v, want scheduler-safe sourcing result", result.Payload)
	}
}

func TestRunRejectsMissingSourcingCandidates(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	_, err := agent.Run(context.Background(), orchestrator.Task{Payload: map[string]any{"candidates": []Candidate{}}})
	if err == nil {
		t.Fatal("expected error for missing candidates")
	}
}

func hasSourcingCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}
	return false
}
