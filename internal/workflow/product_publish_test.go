package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

func TestProductPublishWorkflowWaitsForReviewSignalBeforePublishing(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		return ComplianceResult{}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (MediaValidationResult, error) {
		return MediaValidationResult{}, nil
	}, activity.RegisterOptions{Name: ValidateMediaActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (PublishResult, error) {
		return PublishResult{}, nil
	}, activity.RegisterOptions{Name: PublishToWooCommerceActivity})
	env.RegisterActivityWithOptions(func(context.Context, WorkflowEvent) error {
		return nil
	}, activity.RegisterOptions{Name: RecordWorkflowEventActivity})

	input := ProductPublishInput{ProductID: "product-123", RequestedBy: "operator@example.com"}
	env.OnActivity(CheckComplianceActivity, mock.Anything, ProductPublishActivityInput{ProductID: input.ProductID}).Return(
		ComplianceResult{Pass: true, Score: 94}, nil,
	).Once()
	env.OnActivity(ValidateMediaActivity, mock.Anything, ProductPublishActivityInput{ProductID: input.ProductID}).Return(
		MediaValidationResult{Pass: true, Score: 100}, nil,
	).Once()
	env.OnActivity(RecordWorkflowEventActivity, mock.Anything, mock.Anything).Return(nil).Times(5)
	env.OnActivity(PublishToWooCommerceActivity, mock.Anything, ProductPublishActivityInput{ProductID: input.ProductID}).Return(
		PublishResult{Published: true, RemoteID: "wc-123"}, nil,
	).Once()
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProductPublishReviewSignal, ReviewSignal{Approved: true, Reviewer: "lead@example.com", Note: "ready"})
	}, time.Minute)

	env.ExecuteWorkflow(ProductPublishWorkflow, input)

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result ProductPublishResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != ProductPublishStatusPublished || !result.Published {
		t.Fatalf("result = %+v, want published", result)
	}
	if result.Review.Reviewer != "lead@example.com" {
		t.Fatalf("review = %+v, want reviewer from signal", result.Review)
	}
}

func TestProductPublishWorkflowStopsWhenHumanReviewRejects(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		return ComplianceResult{}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (MediaValidationResult, error) {
		return MediaValidationResult{}, nil
	}, activity.RegisterOptions{Name: ValidateMediaActivity})
	env.RegisterActivityWithOptions(func(context.Context, WorkflowEvent) error {
		return nil
	}, activity.RegisterOptions{Name: RecordWorkflowEventActivity})

	input := ProductPublishInput{ProductID: "product-456", RequestedBy: "operator@example.com"}
	env.OnActivity(CheckComplianceActivity, mock.Anything, ProductPublishActivityInput{ProductID: input.ProductID}).Return(
		ComplianceResult{Pass: true, Score: 90}, nil,
	).Once()
	env.OnActivity(ValidateMediaActivity, mock.Anything, ProductPublishActivityInput{ProductID: input.ProductID}).Return(
		MediaValidationResult{Pass: true, Score: 100}, nil,
	).Once()
	env.OnActivity(RecordWorkflowEventActivity, mock.Anything, mock.Anything).Return(nil).Times(4)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProductPublishReviewSignal, ReviewSignal{Approved: false, Reviewer: "lead@example.com", Note: "copy needs work"})
	}, time.Minute)

	env.ExecuteWorkflow(ProductPublishWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result ProductPublishResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != ProductPublishStatusRejected || result.Published {
		t.Fatalf("result = %+v, want rejected without publish", result)
	}
}

func TestProductPublishWorkflowStopsWhenComplianceFails(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		return ComplianceResult{}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	env.RegisterActivityWithOptions(func(context.Context, WorkflowEvent) error {
		return nil
	}, activity.RegisterOptions{Name: RecordWorkflowEventActivity})

	input := ProductPublishInput{ProductID: "product-789"}
	env.OnActivity(CheckComplianceActivity, mock.Anything, ProductPublishActivityInput{ProductID: input.ProductID}).Return(
		ComplianceResult{Pass: false, Score: 30, Reasons: []string{"product description is too short"}}, nil,
	).Once()
	env.OnActivity(RecordWorkflowEventActivity, mock.Anything, mock.Anything).Return(nil).Twice()

	env.ExecuteWorkflow(ProductPublishWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result ProductPublishResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != ProductPublishStatusComplianceFailed {
		t.Fatalf("status = %q, want compliance failed", result.Status)
	}
}

func TestProductPublishWorkflowStopsWhenMediaValidationFails(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		return ComplianceResult{}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (MediaValidationResult, error) {
		return MediaValidationResult{}, nil
	}, activity.RegisterOptions{Name: ValidateMediaActivity})
	env.RegisterActivityWithOptions(func(context.Context, WorkflowEvent) error {
		return nil
	}, activity.RegisterOptions{Name: RecordWorkflowEventActivity})

	input := ProductPublishInput{ProductID: "product-media-fail"}
	env.OnActivity(CheckComplianceActivity, mock.Anything, ProductPublishActivityInput{ProductID: input.ProductID}).Return(
		ComplianceResult{Pass: true, Score: 90}, nil,
	).Once()
	env.OnActivity(ValidateMediaActivity, mock.Anything, ProductPublishActivityInput{ProductID: input.ProductID}).Return(
		MediaValidationResult{Pass: false, Score: 20, Reasons: []string{"alt text is required"}}, nil,
	).Once()
	env.OnActivity(RecordWorkflowEventActivity, mock.Anything, mock.Anything).Return(nil).Times(3)

	env.ExecuteWorkflow(ProductPublishWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result ProductPublishResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != ProductPublishStatusMediaFailed {
		t.Fatalf("status = %q, want media failed", result.Status)
	}
}

func TestProductPublishWorkflowReportsAwaitingReviewViaQuery(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		return ComplianceResult{Pass: true, Score: 94}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (MediaValidationResult, error) {
		return MediaValidationResult{Pass: true, Score: 100}, nil
	}, activity.RegisterOptions{Name: ValidateMediaActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (PublishResult, error) {
		return PublishResult{Published: true, RemoteID: "wc-query"}, nil
	}, activity.RegisterOptions{Name: PublishToWooCommerceActivity})
	registerNoopRecordActivity(env)

	var queried ProductPublishResult
	env.RegisterDelayedCallback(func() {
		value, err := env.QueryWorkflow(ProductPublishStatusQuery)
		if err != nil {
			t.Fatalf("query workflow: %v", err)
		}
		if err := value.Get(&queried); err != nil {
			t.Fatalf("decode query result: %v", err)
		}
	}, time.Minute)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProductPublishReviewSignal, ReviewSignal{Approved: true, Reviewer: "lead@example.com"})
	}, 2*time.Minute)

	env.ExecuteWorkflow(ProductPublishWorkflow, ProductPublishInput{ProductID: "product-query"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if queried.Status != ProductPublishStatusAwaitingReview {
		t.Fatalf("queried status = %q, want awaiting review", queried.Status)
	}
}

func TestProductPublishWorkflowCanBeCanceledWhileAwaitingReview(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		return ComplianceResult{Pass: true, Score: 94}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (MediaValidationResult, error) {
		return MediaValidationResult{Pass: true, Score: 100}, nil
	}, activity.RegisterOptions{Name: ValidateMediaActivity})
	registerNoopRecordActivity(env)

	env.RegisterDelayedCallback(func() {
		env.CancelWorkflow()
	}, time.Minute)

	env.ExecuteWorkflow(ProductPublishWorkflow, ProductPublishInput{ProductID: "product-cancel"})

	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("expected workflow cancellation error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		t.Fatalf("workflow error = %v, want canceled", err)
	}
}

func TestProductPublishWorkflowRetriesComplianceActivityBeforeReturningComplianceFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	attempts := 0
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		attempts++
		if attempts < 3 {
			return ComplianceResult{}, errors.New("transient compliance outage")
		}
		return ComplianceResult{Pass: false, Score: 30, Reasons: []string{"product description is too short"}}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	registerNoopRecordActivity(env)

	env.ExecuteWorkflow(ProductPublishWorkflow, ProductPublishInput{ProductID: "product-compliance-retry"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("compliance attempts = %d, want 3", attempts)
	}
	var result ProductPublishResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != ProductPublishStatusComplianceFailed {
		t.Fatalf("status = %q, want compliance failed", result.Status)
	}
}

func TestProductPublishWorkflowRetriesMediaValidationActivityBeforeReturningMediaFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		return ComplianceResult{Pass: true, Score: 90}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	attempts := 0
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (MediaValidationResult, error) {
		attempts++
		if attempts < 3 {
			return MediaValidationResult{}, errors.New("transient media outage")
		}
		return MediaValidationResult{Pass: false, Score: 20, Reasons: []string{"alt text is required"}}, nil
	}, activity.RegisterOptions{Name: ValidateMediaActivity})
	registerNoopRecordActivity(env)

	env.ExecuteWorkflow(ProductPublishWorkflow, ProductPublishInput{ProductID: "product-media-retry"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("media validation attempts = %d, want 3", attempts)
	}
	var result ProductPublishResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != ProductPublishStatusMediaFailed {
		t.Fatalf("status = %q, want media failed", result.Status)
	}
}

func TestProductPublishWorkflowRetriesPublishActivityAndSurfacesFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		return ComplianceResult{Pass: true, Score: 94}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (MediaValidationResult, error) {
		return MediaValidationResult{Pass: true, Score: 100}, nil
	}, activity.RegisterOptions{Name: ValidateMediaActivity})
	attempts := 0
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (PublishResult, error) {
		attempts++
		return PublishResult{}, errors.New("woocommerce publish unavailable")
	}, activity.RegisterOptions{Name: PublishToWooCommerceActivity})
	registerNoopRecordActivity(env)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProductPublishReviewSignal, ReviewSignal{Approved: true, Reviewer: "lead@example.com"})
	}, time.Minute)

	env.ExecuteWorkflow(ProductPublishWorkflow, ProductPublishInput{ProductID: "product-publish-retry"})

	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("expected publish failure after retries")
	}
	if attempts != 3 {
		t.Fatalf("publish attempts = %d, want 3", attempts)
	}
}

func TestProductPublishWorkflowE2EWithRealActivities(t *testing.T) {
	t.Parallel()

	repo := newActivityProductRepo(t)
	publisher := &fakeProductPublisher{}
	recorder := &fakeWorkflowEventRecorder{}
	activities := NewProductPublishActivities(ProductPublishActivityDeps{
		Products:  repo,
		Publisher: publisher,
		Recorder:  recorder,
	})

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(activities.CheckCompliance, activity.RegisterOptions{Name: CheckComplianceActivity})
	env.RegisterActivityWithOptions(activities.ValidateMedia, activity.RegisterOptions{Name: ValidateMediaActivity})
	env.RegisterActivityWithOptions(activities.PublishToWooCommerce, activity.RegisterOptions{Name: PublishToWooCommerceActivity})
	env.RegisterActivityWithOptions(activities.RecordWorkflowEvent, activity.RegisterOptions{Name: RecordWorkflowEventActivity})
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProductPublishReviewSignal, ReviewSignal{Approved: true, Reviewer: "qa@example.com", Note: "qa approved"})
	}, time.Minute)

	env.ExecuteWorkflow(ProductPublishWorkflow, ProductPublishInput{ProductID: repo.productID, RequestedBy: "qa@example.com"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result ProductPublishResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != ProductPublishStatusPublished || !result.Published {
		t.Fatalf("result = %+v, want published", result)
	}
	if publisher.publishedID != repo.productID {
		t.Fatalf("published id = %q, want %q", publisher.publishedID, repo.productID)
	}
	if len(recorder.events) != 5 {
		t.Fatalf("recorded events = %d, want 5", len(recorder.events))
	}
}

func TestProductPublishWorkflowReplaysSmokeFailureHistory(t *testing.T) {
	t.Parallel()

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(ProductPublishWorkflow)

	if err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, "testdata/product_publish_failure_history.json"); err != nil {
		t.Fatalf("replay workflow history: %v", err)
	}
}

func TestProductPublishWorkflowReplaysApprovedHistory(t *testing.T) {
	t.Parallel()

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(ProductPublishWorkflow)

	if err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, "testdata/product_publish_approved_history.json"); err != nil {
		t.Fatalf("replay approved workflow history: %v", err)
	}
}

func TestProductPublishWorkflowReplaysRejectedHistory(t *testing.T) {
	t.Parallel()

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(ProductPublishWorkflow)

	if err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, "testdata/product_publish_rejected_history.json"); err != nil {
		t.Fatalf("replay rejected workflow history: %v", err)
	}
}

func TestProductPublishActivitiesUseBackendEngines(t *testing.T) {
	t.Parallel()

	repo := newActivityProductRepo(t)
	publisher := &fakeProductPublisher{}
	recorder := &fakeWorkflowEventRecorder{}
	activities := NewProductPublishActivities(ProductPublishActivityDeps{
		Products:  repo,
		Publisher: publisher,
		Recorder:  recorder,
	})

	compliance, err := activities.CheckCompliance(context.Background(), ProductPublishActivityInput{ProductID: repo.productID})
	if err != nil {
		t.Fatalf("CheckCompliance: %v", err)
	}
	if !compliance.Pass || compliance.Score == 0 {
		t.Fatalf("compliance = %+v, want pass", compliance)
	}
	media, err := activities.ValidateMedia(context.Background(), ProductPublishActivityInput{ProductID: repo.productID})
	if err != nil {
		t.Fatalf("ValidateMedia: %v", err)
	}
	if !media.Pass || media.ImagesChecked != 1 {
		t.Fatalf("media = %+v, want one passing image", media)
	}
	published, err := activities.PublishToWooCommerce(context.Background(), ProductPublishActivityInput{ProductID: repo.productID})
	if err != nil {
		t.Fatalf("PublishToWooCommerce: %v", err)
	}
	if !published.Published || publisher.publishedID != repo.productID {
		t.Fatalf("published = %+v, publisher id = %q", published, publisher.publishedID)
	}
	if err := activities.RecordWorkflowEvent(context.Background(), WorkflowEvent{ProductID: repo.productID, Type: "published"}); err != nil {
		t.Fatalf("RecordWorkflowEvent: %v", err)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("recorded events = %d, want 1", len(recorder.events))
	}
}

func TestProductPublishActivitiesReturnValidationFailures(t *testing.T) {
	t.Parallel()

	repo := newActivityProductRepo(t)
	repo.withoutImages = true
	activities := NewProductPublishActivities(ProductPublishActivityDeps{Products: repo, Publisher: &fakeProductPublisher{}})

	media, err := activities.ValidateMedia(context.Background(), ProductPublishActivityInput{ProductID: repo.productID})
	if err != nil {
		t.Fatalf("ValidateMedia: %v", err)
	}
	if media.Pass || len(media.Reasons) == 0 {
		t.Fatalf("media = %+v, want failure reasons", media)
	}
}

func TestProductPublishActivityRejectsInvalidProductID(t *testing.T) {
	t.Parallel()

	activities := NewProductPublishActivities(ProductPublishActivityDeps{Products: newActivityProductRepo(t)})
	_, err := activities.CheckCompliance(context.Background(), ProductPublishActivityInput{ProductID: "not-a-uuid"})
	if err == nil {
		t.Fatal("expected invalid product id error")
	}
}

func TestProductPublishActivityRequiresConfiguredDependencies(t *testing.T) {
	t.Parallel()

	activities := NewProductPublishActivities(ProductPublishActivityDeps{})
	if _, err := activities.CheckCompliance(context.Background(), ProductPublishActivityInput{ProductID: uuid.NewString()}); err == nil {
		t.Fatal("expected missing product repository error")
	}

	repo := newActivityProductRepo(t)
	activities = NewProductPublishActivities(ProductPublishActivityDeps{Products: repo})
	if _, err := activities.PublishToWooCommerce(context.Background(), ProductPublishActivityInput{ProductID: repo.productID}); err == nil {
		t.Fatal("expected missing publisher error")
	}
	if err := activities.RecordWorkflowEvent(context.Background(), WorkflowEvent{ProductID: repo.productID, Type: "noop"}); err != nil {
		t.Fatalf("nil recorder should be a no-op: %v", err)
	}
}

type fakeProductPublisher struct {
	publishedID string
	err         error
}

func (f *fakeProductPublisher) PublishToWooCommerce(_ context.Context, productID string) error {
	if f.err != nil {
		return f.err
	}
	if productID == "" {
		return errors.New("missing product id")
	}
	f.publishedID = productID
	return nil
}

type fakeWorkflowEventRecorder struct {
	events []WorkflowEvent
}

func (f *fakeWorkflowEventRecorder) RecordWorkflowEvent(_ context.Context, event WorkflowEvent) error {
	f.events = append(f.events, event)
	return nil
}

func registerNoopRecordActivity(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(context.Context, WorkflowEvent) error {
		return nil
	}, activity.RegisterOptions{Name: RecordWorkflowEventActivity})
}

type activityProductRepo struct {
	*inmemory.ProductRepository
	productID     string
	withoutImages bool
}

func newActivityProductRepo(t *testing.T) *activityProductRepo {
	t.Helper()

	price, err := catalog.NewMoney(2995, "AUD")
	if err != nil {
		t.Fatalf("price: %v", err)
	}
	product, err := catalog.NewProduct(catalog.ProductInput{
		SKU:         "WF-READY",
		Title:       "Workflow Ready Product",
		Description: "A durable workflow ready product description with enough detail for compliance and search quality.",
		Price:       price,
		Stock:       5,
		Status:      catalog.StatusDraft,
		Images: []catalog.Image{{
			URL: "https://example.test/workflow-ready-product.jpg",
			Alt: "Workflow ready product on a clean studio background",
		}},
	})
	if err != nil {
		t.Fatalf("product: %v", err)
	}
	repo := inmemory.NewProductRepository()
	if err := repo.Create(context.Background(), product); err != nil {
		t.Fatalf("create product: %v", err)
	}
	return &activityProductRepo{ProductRepository: repo, productID: product.ID().String()}
}

func (r *activityProductRepo) GetByID(ctx context.Context, id uuid.UUID) (catalog.Product, error) {
	product, err := r.ProductRepository.GetByID(ctx, id)
	if err != nil || !r.withoutImages {
		return product, err
	}
	return catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          product.ID(),
		SKU:         product.SKU(),
		Title:       product.Title(),
		Slug:        product.Slug(),
		Description: product.Description(),
		Price:       product.Price(),
		Stock:       product.Stock(),
		Status:      product.Status(),
		CreatedAt:   product.CreatedAt(),
		UpdatedAt:   product.UpdatedAt(),
	}), nil
}

var _ port.ProductRepository = (*activityProductRepo)(nil)
