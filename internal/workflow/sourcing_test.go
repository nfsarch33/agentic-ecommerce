package workflow

import (
	"context"
	"testing"

	sourcingagent "github.com/nfsarch33/helixon-ec/internal/agent/sourcing"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

func TestSourcingWorkflowSearchesScoresChecksMarginAndRecommends(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerSourcingTestActivities(env)

	input := SourcingWorkflowInput{
		Search: SourcingSearchInput{
			SKU:                     "RB-SET",
			EstimatedSellPriceCents: 4995,
			Candidates:              sourcingWorkflowCandidates(),
		},
		MinimumMarginPct: 0.32,
		RequestedBy:      "operator@example.com",
	}
	candidates := sourcingWorkflowCandidates()
	scored := sourcingagent.Result{
		TopCandidate: &sourcingagent.CandidateScore{SupplierID: "balanced", SKU: "RB-SET", Score: 86.83, GrossMarginPct: 0.6496, LeadTimeDays: 7},
		Scores: []sourcingagent.CandidateScore{
			{SupplierID: "balanced", SKU: "RB-SET", Score: 86.83, GrossMarginPct: 0.6496, LeadTimeDays: 7},
			{SupplierID: "slow-cheap", SKU: "RB-SET", Score: 62.12, GrossMarginPct: 0.6245, LeadTimeDays: 18},
		},
	}
	comparison := SourcingPriceComparison{BestLandedCostCents: 1500, AverageLandedCostCents: 1625, CandidateCount: 2}
	margin := SourcingMarginCheck{Pass: true, MinimumMarginPct: 0.32, ActualMarginPct: 0.6496}
	recommendation := SourcingRecommendation{SupplierID: "balanced", SKU: "RB-SET", Recommended: true, Reasons: []string{"highest_score", "margin_passed"}}

	env.OnActivity(SearchSuppliersActivity, mock.Anything, input.Search).Return(candidates, nil).Once()
	env.OnActivity(ScoreSourcingCandidatesActivity, mock.Anything, sourcingagent.Request{Candidates: candidates}).Return(scored, nil).Once()
	env.OnActivity(CompareSourcingPricesActivity, mock.Anything, SourcingPriceComparisonInput{Candidates: candidates, Scores: scored.Scores}).Return(comparison, nil).Once()
	env.OnActivity(CheckSourcingMarginActivity, mock.Anything, SourcingMarginCheckInput{TopCandidate: *scored.TopCandidate, MinimumMarginPct: input.MinimumMarginPct}).Return(margin, nil).Once()
	env.OnActivity(RecommendSourcingCandidateActivity, mock.Anything, SourcingRecommendationInput{Score: *scored.TopCandidate, Comparison: comparison, Margin: margin}).Return(recommendation, nil).Once()

	env.ExecuteWorkflow(SourcingWorkflow, input)
	env.AssertExpectations(t)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result SourcingWorkflowResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != SourcingWorkflowStatusRecommended || result.Recommendation.SupplierID != "balanced" || !result.Recommendation.Recommended {
		t.Fatalf("result = %+v, want balanced recommendation", result)
	}
}

func TestSourcingWorkflowRejectsCandidateBelowMargin(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerSourcingTestActivities(env)

	input := SourcingWorkflowInput{
		Search: SourcingSearchInput{
			SKU:                     "RB-SET",
			EstimatedSellPriceCents: 2200,
			Candidates:              sourcingWorkflowCandidates(),
		},
		MinimumMarginPct: 0.45,
	}
	candidates := sourcingWorkflowCandidates()
	scored := sourcingagent.Result{
		TopCandidate: &sourcingagent.CandidateScore{SupplierID: "balanced", SKU: "RB-SET", Score: 61, GrossMarginPct: 0.2045, LeadTimeDays: 7},
		Scores:       []sourcingagent.CandidateScore{{SupplierID: "balanced", SKU: "RB-SET", Score: 61, GrossMarginPct: 0.2045, LeadTimeDays: 7}},
	}
	comparison := SourcingPriceComparison{BestLandedCostCents: 1750, AverageLandedCostCents: 1750, CandidateCount: 1}
	margin := SourcingMarginCheck{Pass: false, MinimumMarginPct: 0.45, ActualMarginPct: 0.2045}

	env.OnActivity(SearchSuppliersActivity, mock.Anything, input.Search).Return(candidates, nil).Once()
	env.OnActivity(ScoreSourcingCandidatesActivity, mock.Anything, sourcingagent.Request{Candidates: candidates}).Return(scored, nil).Once()
	env.OnActivity(CompareSourcingPricesActivity, mock.Anything, SourcingPriceComparisonInput{Candidates: candidates, Scores: scored.Scores}).Return(comparison, nil).Once()
	env.OnActivity(CheckSourcingMarginActivity, mock.Anything, SourcingMarginCheckInput{TopCandidate: *scored.TopCandidate, MinimumMarginPct: input.MinimumMarginPct}).Return(margin, nil).Once()

	env.ExecuteWorkflow(SourcingWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result SourcingWorkflowResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != SourcingWorkflowStatusMarginFailed || result.Recommendation.Recommended {
		t.Fatalf("result = %+v, want margin failure without recommendation", result)
	}
}

func TestSourcingWorkflowE2EWithDeterministicActivities(t *testing.T) {
	t.Parallel()

	activities := NewSourcingActivities(SourcingActivityDeps{})
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(activities.SearchSuppliers, activity.RegisterOptions{Name: SearchSuppliersActivity})
	env.RegisterActivityWithOptions(activities.ScoreCandidates, activity.RegisterOptions{Name: ScoreSourcingCandidatesActivity})
	env.RegisterActivityWithOptions(activities.ComparePrices, activity.RegisterOptions{Name: CompareSourcingPricesActivity})
	env.RegisterActivityWithOptions(activities.CheckMargin, activity.RegisterOptions{Name: CheckSourcingMarginActivity})
	env.RegisterActivityWithOptions(activities.RecommendCandidate, activity.RegisterOptions{Name: RecommendSourcingCandidateActivity})

	env.ExecuteWorkflow(SourcingWorkflow, SourcingWorkflowInput{
		Search: SourcingSearchInput{
			SKU:                     "RB-SET",
			EstimatedSellPriceCents: 4995,
			Candidates:              sourcingWorkflowCandidates(),
		},
		MinimumMarginPct: 0.32,
		RequestedBy:      "qa@example.com",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result SourcingWorkflowResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != SourcingWorkflowStatusRecommended || result.Recommendation.SupplierID != "balanced" {
		t.Fatalf("result = %+v, want deterministic balanced recommendation", result)
	}
}

func TestSourcingWorkflowReplaysMarginFailureHistory(t *testing.T) {
	t.Parallel()

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(SourcingWorkflow)

	if err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, "testdata/sourcing_margin_failure_history.json"); err != nil {
		t.Fatalf("replay sourcing workflow history: %v", err)
	}
}

func registerSourcingTestActivities(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(context.Context, SourcingSearchInput) ([]sourcingagent.Candidate, error) {
		return nil, nil
	}, activity.RegisterOptions{Name: SearchSuppliersActivity})
	env.RegisterActivityWithOptions(func(context.Context, sourcingagent.Request) (sourcingagent.Result, error) {
		return sourcingagent.Result{}, nil
	}, activity.RegisterOptions{Name: ScoreSourcingCandidatesActivity})
	env.RegisterActivityWithOptions(func(context.Context, SourcingPriceComparisonInput) (SourcingPriceComparison, error) {
		return SourcingPriceComparison{}, nil
	}, activity.RegisterOptions{Name: CompareSourcingPricesActivity})
	env.RegisterActivityWithOptions(func(context.Context, SourcingMarginCheckInput) (SourcingMarginCheck, error) {
		return SourcingMarginCheck{}, nil
	}, activity.RegisterOptions{Name: CheckSourcingMarginActivity})
	env.RegisterActivityWithOptions(func(context.Context, SourcingRecommendationInput) (SourcingRecommendation, error) {
		return SourcingRecommendation{}, nil
	}, activity.RegisterOptions{Name: RecommendSourcingCandidateActivity})
}

func sourcingWorkflowCandidates() []sourcingagent.Candidate {
	return []sourcingagent.Candidate{
		{
			SupplierID:              "slow-cheap",
			SKU:                     "RB-SET",
			UnitCostCents:           1200,
			ShippingCents:           300,
			EstimatedSellPriceCents: 4995,
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
	}
}
