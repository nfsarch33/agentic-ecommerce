package abtest_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/abtest"
)

func makeStore(t *testing.T) *abtest.ExperimentStore {
	t.Helper()
	return abtest.NewExperimentStore()
}

func TestAssigner_Deterministic(t *testing.T) {
	t.Parallel()

	store := makeStore(t)
	exp := abtest.Experiment{
		ID:   "exp1",
		Name: "Test Exp",
		Variants: []abtest.Variant{
			{ID: "control", Name: "Control", Weight: 0.5},
			{ID: "treatment", Name: "Treatment", Weight: 0.5},
		},
		Active: true,
	}
	if err := store.Add(exp); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	assigner := abtest.NewAssigner(store)

	// Same user always gets the same variant.
	v1, err := assigner.Assign("exp1", "user123")
	if err != nil {
		t.Fatalf("Assign failed: %v", err)
	}
	for i := 0; i < 10; i++ {
		v, err := assigner.Assign("exp1", "user123")
		if err != nil {
			t.Fatalf("Assign iteration %d failed: %v", i, err)
		}
		if v != v1 {
			t.Errorf("non-deterministic assignment: got %s then %s", v1, v)
		}
	}
}

func TestAssigner_Distribution50_50(t *testing.T) {
	t.Parallel()

	store := makeStore(t)
	exp := abtest.Experiment{
		ID:   "dist_exp",
		Name: "Distribution Test",
		Variants: []abtest.Variant{
			{ID: "A", Name: "Variant A", Weight: 0.5},
			{ID: "B", Name: "Variant B", Weight: 0.5},
		},
		Active: true,
	}
	if err := store.Add(exp); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	assigner := abtest.NewAssigner(store)
	counts := make(map[string]int)
	total := 1000
	for i := 0; i < total; i++ {
		userID := fmt.Sprintf("user-%d", i)
		v, err := assigner.Assign("dist_exp", userID)
		if err != nil {
			t.Fatalf("Assign failed for user %d: %v", i, err)
		}
		counts[v]++
	}

	// Each variant should get ~50%. Allow 5% deviation = 50 users.
	for variant, count := range counts {
		pct := float64(count) / float64(total)
		if math.Abs(pct-0.5) > 0.05 {
			t.Errorf("variant %s got %.2f%% of traffic (expected ~50%%)", variant, pct*100)
		}
	}
}

func TestExperimentStore_GetNotFound(t *testing.T) {
	t.Parallel()

	store := makeStore(t)
	_, err := store.Get("nonexistent")
	if err != abtest.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestExperimentStore_Deactivate(t *testing.T) {
	t.Parallel()

	store := makeStore(t)
	exp := abtest.Experiment{
		ID:   "deact_exp",
		Name: "Deactivation Test",
		Variants: []abtest.Variant{
			{ID: "ctrl", Name: "Control", Weight: 1.0},
		},
		Active: true,
	}
	if err := store.Add(exp); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	store.Deactivate("deact_exp")
	retrieved, err := store.Get("deact_exp")
	if err != nil {
		t.Fatalf("Get after deactivate failed: %v", err)
	}
	if retrieved.Active {
		t.Error("experiment should be inactive after Deactivate")
	}
}

func TestExperimentStore_InvalidWeights(t *testing.T) {
	t.Parallel()

	store := makeStore(t)
	exp := abtest.Experiment{
		ID:   "bad_weights",
		Name: "Bad Weights",
		Variants: []abtest.Variant{
			{ID: "A", Weight: 0.3},
			{ID: "B", Weight: 0.3},
		},
		Active: true,
	}
	if err := store.Add(exp); err == nil {
		t.Error("expected error for weights not summing to 1.0")
	}
}

func TestZScore_Calculation(t *testing.T) {
	t.Parallel()

	// Baseline: 100 conversions / 1000 impressions = 10%
	// Treatment: 150 conversions / 1000 impressions = 15%
	results := []abtest.ConversionResult{
		{VariantID: "control", Conversions: 100, Impressions: 1000},
		{VariantID: "treatment", Conversions: 150, Impressions: 1000},
	}

	zScores := abtest.ZScore(results)

	if zScores["control"] != 0.0 {
		t.Errorf("baseline z-score should be 0.0, got %f", zScores["control"])
	}

	// Treatment has a higher conversion rate, z-score should be positive and > 1.96.
	tz := zScores["treatment"]
	if tz <= 0 {
		t.Errorf("expected positive z-score for higher-converting treatment, got %f", tz)
	}
	if tz < 1.96 {
		t.Errorf("expected z-score > 1.96 for 10%% vs 15%% with 1000 impressions each, got %f", tz)
	}
}

func TestZScore_EmptyResults(t *testing.T) {
	t.Parallel()

	zScores := abtest.ZScore(nil)
	if len(zScores) != 0 {
		t.Errorf("expected empty map for nil results, got %v", zScores)
	}
}

func TestIsSignificant(t *testing.T) {
	t.Parallel()

	// z=2.1 with p=0.05 (threshold ~1.96) -> significant.
	if !abtest.IsSignificant(2.1, 0.05) {
		t.Error("expected z=2.1 to be significant at p=0.05")
	}

	// z=1.5 with p=0.05 -> not significant.
	if abtest.IsSignificant(1.5, 0.05) {
		t.Error("expected z=1.5 to be not significant at p=0.05")
	}

	// Negative z (absolute value matters).
	if !abtest.IsSignificant(-2.1, 0.05) {
		t.Error("expected z=-2.1 to be significant at p=0.05")
	}
}
