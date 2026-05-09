package sourcing

import (
	"context"
	"testing"
)

func BenchmarkScoreCandidates(b *testing.B) {
	agent := NewAgent()
	req := Request{Candidates: []Candidate{
		{SupplierID: "slow-cheap", SKU: "RB", UnitCostCents: 1200, ShippingCents: 300, EstimatedSellPriceCents: 4995, LeadTimeDays: 18, ReliabilityScore: 0.65, DemandScore: 0.7, CompetitionScore: 0.8},
		{SupplierID: "balanced", SKU: "RB", UnitCostCents: 1500, ShippingCents: 250, EstimatedSellPriceCents: 4995, LeadTimeDays: 7, ReliabilityScore: 0.92, DemandScore: 0.82, CompetitionScore: 0.35},
		{SupplierID: "premium", SKU: "RB", UnitCostCents: 1900, ShippingCents: 180, EstimatedSellPriceCents: 4995, LeadTimeDays: 4, ReliabilityScore: 0.98, DemandScore: 0.85, CompetitionScore: 0.5},
	}}
	for i := 0; i < b.N; i++ {
		if _, err := agent.Score(context.Background(), req); err != nil {
			b.Fatalf("Score: %v", err)
		}
	}
}
