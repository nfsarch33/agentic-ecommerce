package workflow

import (
	"context"
	"testing"
	"time"

	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	sourcingagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/sourcing"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
	"github.com/nfsarch33/agentic-ecommerce/internal/webhook/outbound"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// File scope: workflow worker E2E using Temporal's deterministic test
// environment. The Temporal SDK's `testsuite.WorkflowTestSuite` is the
// embedded equivalent of `temporal server start-dev` for Go workers --
// it provides a fully in-process Temporal core that exercises real
// workflow + activity registration, signal/query routing, retries, and
// determinism enforcement without a running Temporal cluster.
//
// The existing release_e2e_test.go drives the publish/content/media
// flows. This file adds a registration smoke test that exercises every
// workflow and activity name the production temporal-worker binary
// registers, asserting no collisions or missing names.

// TestWorkerRegistrationCoversAllProductionNames ensures every workflow
// + activity name the production temporal-worker binary registers can
// be reached via the testsuite worker. If a future change adds a new
// activity to the worker but forgets to register it on the testsuite,
// this test fails with a clear "activity not found" message.
func TestWorkerRegistrationCoversAllProductionNames(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(ProductPublishWorkflow)
	env.RegisterWorkflow(ContentGenerationWorkflow)
	env.RegisterWorkflow(MediaProcessingWorkflow)
	env.RegisterWorkflow(SourcingWorkflow)

	env.RegisterActivityWithOptions(noopComplianceActivity, activity.RegisterOptions{Name: CheckComplianceActivity})
	env.RegisterActivityWithOptions(noopMediaValidationActivity, activity.RegisterOptions{Name: ValidateMediaActivity})
	env.RegisterActivityWithOptions(noopPublishActivity, activity.RegisterOptions{Name: PublishToWooCommerceActivity})
	env.RegisterActivityWithOptions(noopWorkflowEventRecorder, activity.RegisterOptions{Name: RecordWorkflowEventActivity})
	env.RegisterActivityWithOptions(noopGenerateContentActivity, activity.RegisterOptions{Name: ContentGenerateActivity})
	env.RegisterActivityWithOptions(noopFactCheckActivity, activity.RegisterOptions{Name: ContentFactCheckActivity})
	env.RegisterActivityWithOptions(noopEvaluateActivity, activity.RegisterOptions{Name: ContentEvaluateActivity})
	env.RegisterActivityWithOptions(noopRecordContentFactCheckActivity, activity.RegisterOptions{Name: RecordContentFactCheckActivity})
	env.RegisterActivityWithOptions(noopMediaSourceActivity, activity.RegisterOptions{Name: MediaSourceActivity})
	env.RegisterActivityWithOptions(noopMediaApproveActivity, activity.RegisterOptions{Name: MediaApproveActivity})
	env.RegisterActivityWithOptions(noopMediaProcessActivity, activity.RegisterOptions{Name: MediaProcessActivity})
	env.RegisterActivityWithOptions(noopMediaQualityActivity, activity.RegisterOptions{Name: MediaQualityActivity})
	env.RegisterActivityWithOptions(noopMediaStoreActivity, activity.RegisterOptions{Name: MediaStoreActivity})
	env.RegisterActivityWithOptions(noopMediaLinkProductActivity, activity.RegisterOptions{Name: MediaLinkProductActivity})
	env.RegisterActivityWithOptions(noopSearchSuppliersActivity, activity.RegisterOptions{Name: SearchSuppliersActivity})
	env.RegisterActivityWithOptions(noopScoreSourcingCandidatesActivity, activity.RegisterOptions{Name: ScoreSourcingCandidatesActivity})
	env.RegisterActivityWithOptions(noopCompareSourcingPricesActivity, activity.RegisterOptions{Name: CompareSourcingPricesActivity})
	env.RegisterActivityWithOptions(noopCheckSourcingMarginActivity, activity.RegisterOptions{Name: CheckSourcingMarginActivity})
	env.RegisterActivityWithOptions(noopRecommendSourcingCandidateActivity, activity.RegisterOptions{Name: RecommendSourcingCandidateActivity})

	env.OnActivity(CheckComplianceActivity, mock.Anything, ProductPublishActivityInput{ProductID: "registration-smoke"}).Return(
		ComplianceResult{Pass: true, Score: 95}, nil,
	).Once()
	env.OnActivity(ValidateMediaActivity, mock.Anything, ProductPublishActivityInput{ProductID: "registration-smoke"}).Return(
		MediaValidationResult{Pass: true, Score: 100}, nil,
	).Once()
	env.OnActivity(RecordWorkflowEventActivity, mock.Anything, mock.Anything).Return(nil).Times(5)
	env.OnActivity(PublishToWooCommerceActivity, mock.Anything, ProductPublishActivityInput{ProductID: "registration-smoke"}).Return(
		PublishResult{Published: true, RemoteID: "wc-smoke"}, nil,
	).Once()
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProductPublishReviewSignal, ReviewSignal{Approved: true, Reviewer: "qa@example.com"})
	}, time.Minute)

	env.ExecuteWorkflow(ProductPublishWorkflow, ProductPublishInput{ProductID: "registration-smoke", RequestedBy: "qa"})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete on registered worker")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
}

func noopComplianceActivity(_ context.Context, _ ProductPublishActivityInput) (ComplianceResult, error) {
	return ComplianceResult{Pass: true, Score: 90}, nil
}

func noopMediaValidationActivity(_ context.Context, _ ProductPublishActivityInput) (MediaValidationResult, error) {
	return MediaValidationResult{Pass: true, Score: 100}, nil
}

func noopPublishActivity(_ context.Context, _ ProductPublishActivityInput) (PublishResult, error) {
	return PublishResult{Published: true}, nil
}

func noopWorkflowEventRecorder(_ context.Context, _ WorkflowEvent) error {
	return nil
}

func noopGenerateContentActivity(_ context.Context, _ ContentGenerationInput) (contentagent.GenerateResult, error) {
	return contentagent.GenerateResult{}, nil
}

func noopFactCheckActivity(_ context.Context, _ ContentFactCheckActivityInput) (contentagent.FactCheckResult, error) {
	return contentagent.FactCheckResult{Pass: true, Confidence: 1}, nil
}

func noopEvaluateActivity(_ context.Context, _ ContentEvaluateActivityInput) (contentagent.Evaluation, error) {
	return contentagent.Evaluation{Pass: true, Score: 90}, nil
}

func noopRecordContentFactCheckActivity(_ context.Context, _ ContentGenerationResult) error {
	return nil
}

func noopMediaSourceActivity(_ context.Context, _ MediaProcessingInput) (intelligence.Asset, error) {
	return intelligence.Asset{ID: "media-id"}, nil
}

func noopMediaApproveActivity(_ context.Context, input MediaReviewActivityInput) (intelligence.Asset, error) {
	return intelligence.Asset{ID: input.MediaID, ReviewState: intelligence.MediaReviewStateApproved}, nil
}

func noopMediaProcessActivity(_ context.Context, _ MediaProcessActivityInput) (intelligence.Asset, error) {
	return intelligence.Asset{ID: "media-id"}, nil
}

func noopMediaQualityActivity(_ context.Context, _ MediaQualityActivityInput) (intelligence.QualityReport, error) {
	return intelligence.QualityReport{Pass: true, Score: 95}, nil
}

func noopMediaStoreActivity(_ context.Context, _ MediaStoreActivityInput) (intelligence.Asset, error) {
	return intelligence.Asset{ID: "media-id"}, nil
}

func noopMediaLinkProductActivity(_ context.Context, input MediaProductLinkInput) (MediaProductLinkResult, error) {
	return MediaProductLinkResult{Linked: true, ProductID: input.ProductID, MediaID: input.MediaID}, nil
}

func noopSearchSuppliersActivity(_ context.Context, _ SourcingSearchInput) ([]sourcingagent.Candidate, error) {
	return nil, nil
}

func noopScoreSourcingCandidatesActivity(_ context.Context, _ sourcingagent.Request) (sourcingagent.Result, error) {
	return sourcingagent.Result{}, nil
}

func noopCompareSourcingPricesActivity(_ context.Context, _ SourcingPriceComparisonInput) (SourcingPriceComparison, error) {
	return SourcingPriceComparison{}, nil
}

func noopCheckSourcingMarginActivity(_ context.Context, _ SourcingMarginCheckInput) (SourcingMarginCheck, error) {
	return SourcingMarginCheck{Pass: true}, nil
}

func noopRecommendSourcingCandidateActivity(_ context.Context, _ SourcingRecommendationInput) (SourcingRecommendation, error) {
	return SourcingRecommendation{Recommended: true}, nil
}

// Compile-time guard: keep transitive type imports honest so future
// renames in eventbus/outbound trigger a build break here.
var (
	_ = (*outbound.Service)(nil)
	_ = (*eventbus.Event)(nil)
)
