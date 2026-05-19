package workflow

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	sourcingagent "github.com/nfsarch33/helixon-ec/internal/agent/sourcing"
	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

const (
	SearchSuppliersActivity            = "sourcing.search_suppliers"
	ScoreSourcingCandidatesActivity    = "sourcing.score_candidates"
	CompareSourcingPricesActivity      = "sourcing.compare_prices"
	CheckSourcingMarginActivity        = "sourcing.check_margin"
	RecommendSourcingCandidateActivity = "sourcing.recommend_candidate"

	SourcingWorkflowStatusSearched     = "searched"
	SourcingWorkflowStatusScored       = "scored"
	SourcingWorkflowStatusCompared     = "compared"
	SourcingWorkflowStatusMarginFailed = "margin_failed"
	SourcingWorkflowStatusRecommended  = "recommended"
)

type SourcingSearchInput struct {
	SKU                     string                    `json:"sku"`
	Query                   string                    `json:"query,omitempty"`
	EstimatedSellPriceCents int                       `json:"estimated_sell_price_cents"`
	CandidateLimit          int                       `json:"candidate_limit,omitempty"`
	Candidates              []sourcingagent.Candidate `json:"candidates,omitempty"`
}

type SourcingWorkflowInput struct {
	Search           SourcingSearchInput `json:"search"`
	MinimumMarginPct float64             `json:"minimum_margin_pct"`
	RequestedBy      string              `json:"requested_by,omitempty"`
}

type SourcingWorkflowResult struct {
	Status         string                    `json:"status"`
	RequestedBy    string                    `json:"requested_by,omitempty"`
	Candidates     []sourcingagent.Candidate `json:"candidates,omitempty"`
	Scoring        sourcingagent.Result      `json:"scoring"`
	Comparison     SourcingPriceComparison   `json:"comparison"`
	Margin         SourcingMarginCheck       `json:"margin"`
	Recommendation SourcingRecommendation    `json:"recommendation"`
}

type SourcingPriceComparisonInput struct {
	Candidates []sourcingagent.Candidate      `json:"candidates"`
	Scores     []sourcingagent.CandidateScore `json:"scores"`
}

type SourcingPriceComparison struct {
	BestLandedCostCents    int `json:"best_landed_cost_cents"`
	AverageLandedCostCents int `json:"average_landed_cost_cents"`
	CandidateCount         int `json:"candidate_count"`
}

type SourcingMarginCheckInput struct {
	TopCandidate     sourcingagent.CandidateScore `json:"top_candidate"`
	MinimumMarginPct float64                      `json:"minimum_margin_pct"`
}

type SourcingMarginCheck struct {
	Pass             bool    `json:"pass"`
	MinimumMarginPct float64 `json:"minimum_margin_pct"`
	ActualMarginPct  float64 `json:"actual_margin_pct"`
}

type SourcingRecommendationInput struct {
	Score      sourcingagent.CandidateScore `json:"score"`
	Comparison SourcingPriceComparison      `json:"comparison"`
	Margin     SourcingMarginCheck          `json:"margin"`
}

type SourcingRecommendation struct {
	SupplierID  string   `json:"supplier_id,omitempty"`
	SKU         string   `json:"sku,omitempty"`
	Recommended bool     `json:"recommended"`
	Reasons     []string `json:"reasons,omitempty"`
}

type SourcingActivityDeps struct {
	Searcher SupplierSearcher
}

type SupplierSearcher interface {
	SearchSuppliers(context.Context, SourcingSearchInput) ([]sourcingagent.Candidate, error)
}

type SourcingActivities struct {
	searcher SupplierSearcher
	scorer   *sourcingagent.Agent
}

func NewSourcingActivities(deps SourcingActivityDeps) *SourcingActivities {
	return &SourcingActivities{searcher: deps.Searcher, scorer: sourcingagent.NewAgent()}
}

func SourcingWorkflow(ctx temporalworkflow.Context, input SourcingWorkflowInput) (SourcingWorkflowResult, error) {
	activityOptions := temporalworkflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
	ctx = temporalworkflow.WithActivityOptions(ctx, activityOptions)

	result := SourcingWorkflowResult{RequestedBy: input.RequestedBy}
	if err := temporalworkflow.ExecuteActivity(ctx, SearchSuppliersActivity, input.Search).Get(ctx, &result.Candidates); err != nil {
		return result, err
	}
	result.Status = SourcingWorkflowStatusSearched

	scoreInput := sourcingagent.Request{Candidates: result.Candidates}
	if err := temporalworkflow.ExecuteActivity(ctx, ScoreSourcingCandidatesActivity, scoreInput).Get(ctx, &result.Scoring); err != nil {
		return result, err
	}
	result.Status = SourcingWorkflowStatusScored
	if result.Scoring.TopCandidate == nil {
		return result, errors.New("sourcing workflow requires a top candidate")
	}

	compareInput := SourcingPriceComparisonInput{Candidates: result.Candidates, Scores: result.Scoring.Scores}
	if err := temporalworkflow.ExecuteActivity(ctx, CompareSourcingPricesActivity, compareInput).Get(ctx, &result.Comparison); err != nil {
		return result, err
	}
	result.Status = SourcingWorkflowStatusCompared

	marginInput := SourcingMarginCheckInput{TopCandidate: *result.Scoring.TopCandidate, MinimumMarginPct: input.MinimumMarginPct}
	if err := temporalworkflow.ExecuteActivity(ctx, CheckSourcingMarginActivity, marginInput).Get(ctx, &result.Margin); err != nil {
		return result, err
	}
	if !result.Margin.Pass {
		result.Status = SourcingWorkflowStatusMarginFailed
		return result, nil
	}

	recommendInput := SourcingRecommendationInput{Score: *result.Scoring.TopCandidate, Comparison: result.Comparison, Margin: result.Margin}
	if err := temporalworkflow.ExecuteActivity(ctx, RecommendSourcingCandidateActivity, recommendInput).Get(ctx, &result.Recommendation); err != nil {
		return result, err
	}
	result.Status = SourcingWorkflowStatusRecommended
	return result, nil
}

func (a *SourcingActivities) SearchSuppliers(ctx context.Context, input SourcingSearchInput) ([]sourcingagent.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.searcher != nil {
		return a.searcher.SearchSuppliers(ctx, input)
	}
	candidates := append([]sourcingagent.Candidate(nil), input.Candidates...)
	if input.SKU != "" {
		for i := range candidates {
			if candidates[i].SKU == "" {
				candidates[i].SKU = input.SKU
			}
			if candidates[i].EstimatedSellPriceCents == 0 {
				candidates[i].EstimatedSellPriceCents = input.EstimatedSellPriceCents
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].SupplierID < candidates[j].SupplierID
	})
	if input.CandidateLimit > 0 && len(candidates) > input.CandidateLimit {
		candidates = candidates[:input.CandidateLimit]
	}
	return candidates, nil
}

func (a *SourcingActivities) ScoreCandidates(ctx context.Context, input sourcingagent.Request) (sourcingagent.Result, error) {
	if a.scorer == nil {
		a.scorer = sourcingagent.NewAgent()
	}
	return a.scorer.Score(ctx, input)
}

func (a *SourcingActivities) ComparePrices(ctx context.Context, input SourcingPriceComparisonInput) (SourcingPriceComparison, error) {
	if err := ctx.Err(); err != nil {
		return SourcingPriceComparison{}, err
	}
	if len(input.Candidates) == 0 {
		return SourcingPriceComparison{}, sourcingagent.ErrNoCandidates
	}
	best := math.MaxInt
	sum := 0
	count := 0
	for _, candidate := range input.Candidates {
		landed := candidate.UnitCostCents + candidate.ShippingCents
		if landed <= 0 {
			continue
		}
		if landed < best {
			best = landed
		}
		sum += landed
		count++
	}
	if count == 0 {
		return SourcingPriceComparison{}, sourcingagent.ErrNoCandidates
	}
	return SourcingPriceComparison{
		BestLandedCostCents:    best,
		AverageLandedCostCents: int(math.Round(float64(sum) / float64(count))),
		CandidateCount:         count,
	}, nil
}

func (a *SourcingActivities) CheckMargin(ctx context.Context, input SourcingMarginCheckInput) (SourcingMarginCheck, error) {
	if err := ctx.Err(); err != nil {
		return SourcingMarginCheck{}, err
	}
	minimum := input.MinimumMarginPct
	if minimum <= 0 {
		minimum = 0.3
	}
	return SourcingMarginCheck{
		Pass:             input.TopCandidate.GrossMarginPct >= minimum,
		MinimumMarginPct: minimum,
		ActualMarginPct:  input.TopCandidate.GrossMarginPct,
	}, nil
}

func (a *SourcingActivities) RecommendCandidate(ctx context.Context, input SourcingRecommendationInput) (SourcingRecommendation, error) {
	if err := ctx.Err(); err != nil {
		return SourcingRecommendation{}, err
	}
	return SourcingRecommendation{
		SupplierID:  input.Score.SupplierID,
		SKU:         input.Score.SKU,
		Recommended: input.Margin.Pass,
		Reasons:     []string{"highest_score", "margin_passed"},
	}, nil
}
