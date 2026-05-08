package workflow

import (
	"context"
	"errors"
	"testing"

	sourcingagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/sourcing"
)

// File scope: pure-activity unit coverage for SourcingActivities. The
// existing sourcing_test.go drives the workflow through the testsuite.
// This file pins the activity implementations directly so we cover the
// otherwise-66% branches in CheckMargin, RecommendCandidate,
// ComparePrices, ScoreCandidates, and SearchSuppliers without spinning a
// full workflow environment.

type stubSupplierSearcher struct {
	candidates []sourcingagent.Candidate
	err        error
}

func (s *stubSupplierSearcher) SearchSuppliers(_ context.Context, _ SourcingSearchInput) ([]sourcingagent.Candidate, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.candidates, nil
}

func TestSourcingActivitiesSearchSuppliersDelegatesToSearcher(t *testing.T) {
	t.Parallel()

	want := []sourcingagent.Candidate{{SupplierID: "supplier-a"}}
	activities := NewSourcingActivities(SourcingActivityDeps{Searcher: &stubSupplierSearcher{candidates: want}})
	got, err := activities.SearchSuppliers(context.Background(), SourcingSearchInput{SKU: "RB"})
	if err != nil {
		t.Fatalf("SearchSuppliers: %v", err)
	}
	if len(got) != 1 || got[0].SupplierID != "supplier-a" {
		t.Fatalf("got = %+v", got)
	}
}

func TestSourcingActivitiesSearchSuppliersReturnsCancellationError(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := activities.SearchSuppliers(ctx, SourcingSearchInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSourcingActivitiesSearchSuppliersBackfillsSKUAndPriceWhenMissing(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	got, err := activities.SearchSuppliers(context.Background(), SourcingSearchInput{
		SKU:                     "BACKFILL",
		EstimatedSellPriceCents: 1234,
		Candidates: []sourcingagent.Candidate{
			{SupplierID: "missing"},
			{SupplierID: "preset", SKU: "PRESET", EstimatedSellPriceCents: 9999},
		},
	})
	if err != nil {
		t.Fatalf("SearchSuppliers: %v", err)
	}

	// Activities sorts candidates by SupplierID alphabetically; locate them
	// by SupplierID rather than relying on input order.
	bySupplier := map[string]sourcingagent.Candidate{}
	for _, c := range got {
		bySupplier[c.SupplierID] = c
	}
	if missing := bySupplier["missing"]; missing.SKU != "BACKFILL" || missing.EstimatedSellPriceCents != 1234 {
		t.Fatalf("backfill candidate = %+v, want SKU=BACKFILL price=1234", missing)
	}
	if preset := bySupplier["preset"]; preset.SKU != "PRESET" || preset.EstimatedSellPriceCents != 9999 {
		t.Fatalf("preset candidate = %+v, want preserved", preset)
	}
}

func TestSourcingActivitiesSearchSuppliersAppliesCandidateLimit(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	candidates := []sourcingagent.Candidate{
		{SupplierID: "alpha"},
		{SupplierID: "bravo"},
		{SupplierID: "charlie"},
	}
	got, err := activities.SearchSuppliers(context.Background(), SourcingSearchInput{
		Candidates:     candidates,
		CandidateLimit: 2,
	})
	if err != nil {
		t.Fatalf("SearchSuppliers: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got = %d, want 2 (limit applied)", len(got))
	}
	if got[0].SupplierID != "alpha" || got[1].SupplierID != "bravo" {
		t.Fatalf("limit applied incorrectly: %+v", got)
	}
}

func TestSourcingActivitiesScoreCandidatesUsesInjectedScorer(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	got, err := activities.ScoreCandidates(context.Background(), sourcingagent.Request{
		Candidates: []sourcingagent.Candidate{
			{SupplierID: "balanced", SKU: "RB", UnitCostCents: 1500, ShippingCents: 250, EstimatedSellPriceCents: 4995, ReliabilityScore: 0.9, DemandScore: 0.8, CompetitionScore: 0.4},
		},
	})
	if err != nil {
		t.Fatalf("ScoreCandidates: %v", err)
	}
	if got.TopCandidate == nil {
		t.Fatal("TopCandidate is nil")
	}
}

func TestSourcingActivitiesComparePricesReturnsErrNoCandidatesForEmpty(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	if _, err := activities.ComparePrices(context.Background(), SourcingPriceComparisonInput{}); !errors.Is(err, sourcingagent.ErrNoCandidates) {
		t.Fatalf("err = %v, want ErrNoCandidates", err)
	}
}

func TestSourcingActivitiesComparePricesIgnoresZeroLandedCosts(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	got, err := activities.ComparePrices(context.Background(), SourcingPriceComparisonInput{
		Candidates: []sourcingagent.Candidate{
			{SupplierID: "free", UnitCostCents: 0, ShippingCents: 0},
			{SupplierID: "real", UnitCostCents: 1000, ShippingCents: 200},
		},
	})
	if err != nil {
		t.Fatalf("ComparePrices: %v", err)
	}
	if got.CandidateCount != 1 || got.BestLandedCostCents != 1200 {
		t.Fatalf("result = %+v, want CandidateCount=1 BestLanded=1200", got)
	}
}

func TestSourcingActivitiesComparePricesPropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := activities.ComparePrices(ctx, SourcingPriceComparisonInput{
		Candidates: []sourcingagent.Candidate{{UnitCostCents: 100}},
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSourcingActivitiesCheckMarginAppliesDefaultMinimumWhenZero(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	got, err := activities.CheckMargin(context.Background(), SourcingMarginCheckInput{
		TopCandidate: sourcingagent.CandidateScore{GrossMarginPct: 0.4},
	})
	if err != nil {
		t.Fatalf("CheckMargin: %v", err)
	}
	if got.MinimumMarginPct != 0.3 {
		t.Fatalf("MinimumMarginPct = %v, want 0.3 default", got.MinimumMarginPct)
	}
	if !got.Pass {
		t.Fatalf("expected pass; result = %+v", got)
	}
}

func TestSourcingActivitiesCheckMarginRespectsConfiguredMinimum(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	got, err := activities.CheckMargin(context.Background(), SourcingMarginCheckInput{
		TopCandidate:     sourcingagent.CandidateScore{GrossMarginPct: 0.3},
		MinimumMarginPct: 0.45,
	})
	if err != nil {
		t.Fatalf("CheckMargin: %v", err)
	}
	if got.Pass {
		t.Fatalf("expected fail; result = %+v", got)
	}
}

func TestSourcingActivitiesCheckMarginPropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := activities.CheckMargin(ctx, SourcingMarginCheckInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSourcingActivitiesRecommendCandidateMirrorsTopScoreAndMargin(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	got, err := activities.RecommendCandidate(context.Background(), SourcingRecommendationInput{
		Score:  sourcingagent.CandidateScore{SupplierID: "balanced", SKU: "RB"},
		Margin: SourcingMarginCheck{Pass: true, MinimumMarginPct: 0.3},
	})
	if err != nil {
		t.Fatalf("RecommendCandidate: %v", err)
	}
	if got.SupplierID != "balanced" || got.SKU != "RB" || !got.Recommended {
		t.Fatalf("result = %+v", got)
	}
	if len(got.Reasons) != 2 {
		t.Fatalf("Reasons = %v, want exactly two", got.Reasons)
	}
}

func TestSourcingActivitiesRecommendCandidatePropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := activities.RecommendCandidate(ctx, SourcingRecommendationInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSourcingActivitiesScoreCandidatesAfterScorerCleared(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	activities.scorer = nil
	if _, err := activities.ScoreCandidates(context.Background(), sourcingagent.Request{
		Candidates: []sourcingagent.Candidate{
			{SupplierID: "balanced", SKU: "RB", UnitCostCents: 1500, ShippingCents: 250, EstimatedSellPriceCents: 4995, ReliabilityScore: 0.9, DemandScore: 0.8, CompetitionScore: 0.4},
		},
	}); err != nil {
		t.Fatalf("ScoreCandidates after nil scorer: %v", err)
	}
}
