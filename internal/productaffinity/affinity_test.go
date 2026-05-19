package productaffinity_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/productaffinity"
)

func TestAffinityMiner_CoOccurrenceCounting(t *testing.T) {
	t.Parallel()

	m := productaffinity.NewAffinityMiner()
	m.Ingest(productaffinity.Order{ID: "o1", ProductIDs: []string{"A", "B", "C"}})
	m.Ingest(productaffinity.Order{ID: "o2", ProductIDs: []string{"A", "B"}})
	m.Ingest(productaffinity.Order{ID: "o3", ProductIDs: []string{"B", "C"}})

	// A+B co-occur in o1, o2 = 2
	if n := m.CoOccurrences("A", "B"); n != 2 {
		t.Errorf("expected A+B co-occurrence 2, got %d", n)
	}
	// B+C co-occur in o1, o3 = 2
	if n := m.CoOccurrences("B", "C"); n != 2 {
		t.Errorf("expected B+C co-occurrence 2, got %d", n)
	}
	// A+C co-occur only in o1 = 1
	if n := m.CoOccurrences("A", "C"); n != 1 {
		t.Errorf("expected A+C co-occurrence 1, got %d", n)
	}
	// Non-existing pair
	if n := m.CoOccurrences("A", "Z"); n != 0 {
		t.Errorf("expected 0 for unknown pair, got %d", n)
	}
}

func TestAffinityMiner_SupportCalculation(t *testing.T) {
	t.Parallel()

	m := productaffinity.NewAffinityMiner()
	m.Ingest(productaffinity.Order{ID: "o1", ProductIDs: []string{"A", "B"}})
	m.Ingest(productaffinity.Order{ID: "o2", ProductIDs: []string{"A", "B"}})
	m.Ingest(productaffinity.Order{ID: "o3", ProductIDs: []string{"C"}})

	totalOrders := 3
	// A+B appear in 2 out of 3 orders = 0.666...
	sup := m.Support("A", "B", totalOrders)
	if sup < 0.665 || sup > 0.668 {
		t.Errorf("expected support ~0.666, got %f", sup)
	}

	// Zero totalOrders -> 0
	if s := m.Support("A", "B", 0); s != 0 {
		t.Errorf("expected support 0 for totalOrders=0, got %f", s)
	}
}

func TestMineRules_AboveThreshold(t *testing.T) {
	t.Parallel()

	m := productaffinity.NewAffinityMiner()
	// 4 orders: A+B always together, C rarely with A.
	m.Ingest(productaffinity.Order{ID: "o1", ProductIDs: []string{"A", "B"}})
	m.Ingest(productaffinity.Order{ID: "o2", ProductIDs: []string{"A", "B"}})
	m.Ingest(productaffinity.Order{ID: "o3", ProductIDs: []string{"A", "B"}})
	m.Ingest(productaffinity.Order{ID: "o4", ProductIDs: []string{"A", "C"}})

	totalOrders := 4
	// A+B support = 3/4 = 0.75; confidence(A->B) = 3/4 = 0.75, confidence(B->A) = 3/3 = 1.0
	rules := productaffinity.MineRules(m, totalOrders, 0.5, 0.7)

	// Expect at least the A->B and B->A rules.
	ruleMap := make(map[string]productaffinity.AssociationRule)
	for _, r := range rules {
		ruleMap[r.Antecedent+"->"+r.Consequent] = r
	}

	if r, ok := ruleMap["A->B"]; !ok {
		t.Error("expected rule A->B to be mined")
	} else if r.Confidence < 0.74 || r.Confidence > 0.76 {
		t.Errorf("A->B confidence expected ~0.75, got %f", r.Confidence)
	}

	if r, ok := ruleMap["B->A"]; !ok {
		t.Error("expected rule B->A to be mined")
	} else if r.Confidence < 0.99 || r.Confidence > 1.01 {
		t.Errorf("B->A confidence expected 1.0, got %f", r.Confidence)
	}
}

func TestRecommend_TopN(t *testing.T) {
	t.Parallel()

	m := productaffinity.NewAffinityMiner()
	m.Ingest(productaffinity.Order{ID: "o1", ProductIDs: []string{"A", "B", "C"}})
	m.Ingest(productaffinity.Order{ID: "o2", ProductIDs: []string{"A", "B"}})
	m.Ingest(productaffinity.Order{ID: "o3", ProductIDs: []string{"A", "C"}})
	m.Ingest(productaffinity.Order{ID: "o4", ProductIDs: []string{"A", "D"}})

	// A co-occurs with: B(2), C(2), D(1) -> top 2 should be B and C (or C then B, alphabetical tie-break).
	recs := productaffinity.Recommend(m, "A", 2, 4)
	if len(recs) != 2 {
		t.Fatalf("expected 2 recommendations, got %d: %v", len(recs), recs)
	}
	// Both B and C have 2 co-occurrences; D has 1. Top 2 must be {B, C}.
	recSet := make(map[string]bool)
	for _, r := range recs {
		recSet[r] = true
	}
	if !recSet["B"] || !recSet["C"] {
		t.Errorf("expected B and C in top 2 recommendations, got %v", recs)
	}
}

func TestRecommend_ProductNotInAnyOrder(t *testing.T) {
	t.Parallel()

	m := productaffinity.NewAffinityMiner()
	m.Ingest(productaffinity.Order{ID: "o1", ProductIDs: []string{"A", "B"}})

	recs := productaffinity.Recommend(m, "Z", 5, 1)
	if len(recs) != 0 {
		t.Errorf("expected empty recommendations for unknown product, got %v", recs)
	}
}

func TestMineRules_BelowThreshold(t *testing.T) {
	t.Parallel()

	m := productaffinity.NewAffinityMiner()
	m.Ingest(productaffinity.Order{ID: "o1", ProductIDs: []string{"A", "B"}})
	m.Ingest(productaffinity.Order{ID: "o2", ProductIDs: []string{"C", "D"}})
	m.Ingest(productaffinity.Order{ID: "o3", ProductIDs: []string{"E"}})

	// A+B support = 1/3 = 0.33; threshold 0.5 -> no rules.
	rules := productaffinity.MineRules(m, 3, 0.5, 0.9)
	if len(rules) != 0 {
		t.Errorf("expected no rules above threshold, got %d", len(rules))
	}
}
