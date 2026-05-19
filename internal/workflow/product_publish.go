package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/compliance"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	"github.com/nfsarch33/helixon-ec/internal/media"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

const (
	TaskQueue = "ec-workflows"

	ProductPublishReviewSignal = "product-publish-review"
	ProductPublishStatusQuery  = "product-publish-status"

	CheckComplianceActivity      = "product_publish.check_compliance"
	ValidateMediaActivity        = "product_publish.validate_media"
	PublishToWooCommerceActivity = "product_publish.publish_to_woocommerce"
	RecordWorkflowEventActivity  = "product_publish.record_workflow_event"

	ProductPublishStatusDraft            = "draft"
	ProductPublishStatusComplianceFailed = "compliance_failed"
	ProductPublishStatusMediaFailed      = "media_failed"
	ProductPublishStatusAwaitingReview   = "awaiting_review"
	ProductPublishStatusRejected         = "rejected"
	ProductPublishStatusPublishing       = "publishing"
	ProductPublishStatusFailed           = "failed"
	ProductPublishStatusPublished        = "published"

	workflowActivityStatusPending       = "pending"
	workflowActivityStatusRunning       = "running"
	workflowActivityStatusWaitingReview = "waiting_review"
	workflowActivityStatusCompleted     = "completed"
	workflowActivityStatusFailed        = "failed"
	workflowActivityStatusSkipped       = "skipped"
)

type ProductPublishInput struct {
	ProductID   string `json:"product_id"`
	RequestedBy string `json:"requested_by,omitempty"`
}

type ProductPublishActivityInput struct {
	ProductID string `json:"product_id"`
}

type ProductPublishResult struct {
	ProductID       string                         `json:"product_id"`
	Status          string                         `json:"status"`
	Published       bool                           `json:"published"`
	Compliance      ComplianceResult               `json:"compliance"`
	Media           MediaValidationResult          `json:"media"`
	Review          ReviewSignal                   `json:"review"`
	Publish         PublishResult                  `json:"publish"`
	CurrentActivity string                         `json:"current_activity,omitempty"`
	StartedAt       string                         `json:"started_at,omitempty"`
	UpdatedAt       string                         `json:"updated_at,omitempty"`
	CompletedAt     string                         `json:"completed_at,omitempty"`
	Activities      []ProductPublishLifecycleStage `json:"activities,omitempty"`
}

type ProductPublishLifecycleStage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Message     string `json:"message,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`
	Error       string `json:"error,omitempty"`
}

type ComplianceResult struct {
	Pass     bool     `json:"pass"`
	Score    int      `json:"score"`
	Reasons  []string `json:"reasons"`
	RuleIDs  []string `json:"rule_ids"`
	Severity string   `json:"severity,omitempty"`
}

type MediaValidationResult struct {
	Pass          bool     `json:"pass"`
	Score         int      `json:"score"`
	Reasons       []string `json:"reasons"`
	ImagesChecked int      `json:"images_checked"`
}

type ReviewSignal struct {
	Approved bool   `json:"approved"`
	Reviewer string `json:"reviewer,omitempty"`
	Note     string `json:"note,omitempty"`
}

type PublishResult struct {
	Published bool   `json:"published"`
	RemoteID  string `json:"remote_id,omitempty"`
}

type WorkflowEvent struct {
	ProductID   string    `json:"product_id"`
	Type        string    `json:"type"`
	Status      string    `json:"status,omitempty"`
	Message     string    `json:"message,omitempty"`
	RequestedBy string    `json:"requested_by,omitempty"`
	Reviewer    string    `json:"reviewer,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type ProductPublishActivityDeps struct {
	Products  port.ProductRepository
	Publisher ProductPublisher
	Recorder  WorkflowEventRecorder
}

type ProductPublisher interface {
	PublishToWooCommerce(context.Context, string) error
}

type WorkflowEventRecorder interface {
	RecordWorkflowEvent(context.Context, WorkflowEvent) error
}

type ProductPublishActivities struct {
	products   port.ProductRepository
	publisher  ProductPublisher
	recorder   WorkflowEventRecorder
	compliance compliance.Engine
	media      media.Processor
}

func NewProductPublishActivities(deps ProductPublishActivityDeps) *ProductPublishActivities {
	return &ProductPublishActivities{
		products:   deps.Products,
		publisher:  deps.Publisher,
		recorder:   deps.Recorder,
		compliance: compliance.NewEngine(compliance.DefaultRules()),
		media:      media.NewProcessor(media.DefaultConstraints()),
	}
}

func ProductPublishWorkflow(ctx temporalworkflow.Context, input ProductPublishInput) (ProductPublishResult, error) {
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

	state := newProductPublishState(input, temporalworkflow.Now(ctx))
	if err := temporalworkflow.SetQueryHandler(ctx, ProductPublishStatusQuery, func() (ProductPublishResult, error) {
		return state, nil
	}); err != nil {
		return state, err
	}

	activityInput := ProductPublishActivityInput{ProductID: input.ProductID}
	if err := record(ctx, input, "product_publish.started", ProductPublishStatusDraft, "product publish workflow started", ""); err != nil {
		return state, err
	}
	if err := runProductPublishLifecycle(ctx, input, activityInput, &state); err != nil {
		return state, err
	}
	return state, nil
}

func runProductPublishLifecycle(ctx temporalworkflow.Context, input ProductPublishInput, activityInput ProductPublishActivityInput, state *ProductPublishResult) error {
	ok, err := runInitialProductPublishStages(ctx, input, activityInput, state)
	if err != nil || !ok {
		return err
	}
	return runReviewedProductPublishStages(ctx, input, activityInput, state)
}

func runInitialProductPublishStages(ctx temporalworkflow.Context, input ProductPublishInput, activityInput ProductPublishActivityInput, state *ProductPublishResult) (bool, error) {
	for _, gate := range []func(temporalworkflow.Context, ProductPublishInput, ProductPublishActivityInput, *ProductPublishResult) (bool, error){
		runComplianceGate,
		runMediaGate,
	} {
		shouldContinue, err := gate(ctx, input, activityInput, state)
		if err != nil || !shouldContinue {
			return false, err
		}
	}
	return true, nil
}

func runReviewedProductPublishStages(ctx temporalworkflow.Context, input ProductPublishInput, activityInput ProductPublishActivityInput, state *ProductPublishResult) error {
	approved, err := runReviewGate(ctx, input, state)
	if err != nil || !approved {
		return err
	}
	if err := runPublish(ctx, input, activityInput, state); err != nil {
		return err
	}
	return nil
}

func runComplianceGate(ctx temporalworkflow.Context, input ProductPublishInput, activityInput ProductPublishActivityInput, state *ProductPublishResult) (bool, error) {
	if err := temporalworkflow.ExecuteActivity(ctx, CheckComplianceActivity, activityInput).Get(ctx, &state.Compliance); err != nil {
		state.Status = ProductPublishStatusComplianceFailed
		state.failActivity("check_compliance", temporalworkflow.Now(ctx), "Compliance check failed", err)
		state.skipActivities(temporalworkflow.Now(ctx), "validate_media", "human_review", "publish")
		state.CompletedAt = workflowTimestamp(temporalworkflow.Now(ctx))
		return false, err
	}
	if state.Compliance.Pass {
		state.completeActivity("check_compliance", temporalworkflow.Now(ctx), "Compliance check passed")
		state.startActivity("validate_media", temporalworkflow.Now(ctx), "Validating media")
		return true, record(ctx, input, "product_publish.compliance_passed", state.Status, "compliance check passed", "")
	}
	state.Status = ProductPublishStatusComplianceFailed
	state.failActivity("check_compliance", temporalworkflow.Now(ctx), "Compliance check failed", errors.New(strings.Join(state.Compliance.Reasons, "; ")))
	state.skipActivities(temporalworkflow.Now(ctx), "validate_media", "human_review", "publish")
	state.CompletedAt = workflowTimestamp(temporalworkflow.Now(ctx))
	return false, record(ctx, input, "product_publish.compliance_failed", state.Status, "compliance check failed", "")
}

func runMediaGate(ctx temporalworkflow.Context, input ProductPublishInput, activityInput ProductPublishActivityInput, state *ProductPublishResult) (bool, error) {
	if err := temporalworkflow.ExecuteActivity(ctx, ValidateMediaActivity, activityInput).Get(ctx, &state.Media); err != nil {
		state.Status = ProductPublishStatusMediaFailed
		state.failActivity("validate_media", temporalworkflow.Now(ctx), "Media validation failed", err)
		state.skipActivities(temporalworkflow.Now(ctx), "human_review", "publish")
		state.CompletedAt = workflowTimestamp(temporalworkflow.Now(ctx))
		return false, err
	}
	if state.Media.Pass {
		state.completeActivity("validate_media", temporalworkflow.Now(ctx), "Media validation passed")
		state.waitForReview(temporalworkflow.Now(ctx), "Awaiting human review")
		return true, record(ctx, input, "product_publish.media_validated", state.Status, "media validation passed", "")
	}
	state.Status = ProductPublishStatusMediaFailed
	state.failActivity("validate_media", temporalworkflow.Now(ctx), "Media validation failed", errors.New(strings.Join(state.Media.Reasons, "; ")))
	state.skipActivities(temporalworkflow.Now(ctx), "human_review", "publish")
	state.CompletedAt = workflowTimestamp(temporalworkflow.Now(ctx))
	return false, record(ctx, input, "product_publish.media_failed", state.Status, "media validation failed", "")
}

func runReviewGate(ctx temporalworkflow.Context, input ProductPublishInput, state *ProductPublishResult) (bool, error) {
	state.Status = ProductPublishStatusAwaitingReview
	review, err := receiveReviewSignal(ctx)
	if err != nil {
		state.failActivity("human_review", temporalworkflow.Now(ctx), "Human review did not complete", err)
		state.skipActivities(temporalworkflow.Now(ctx), "publish")
		return false, err
	}
	state.Review = review
	if review.Approved {
		state.completeActivity("human_review", temporalworkflow.Now(ctx), "Human review approved publish")
		state.startActivity("publish", temporalworkflow.Now(ctx), "Publish to WooCommerce")
		return true, record(ctx, input, "product_publish.review_approved", state.Status, "human review approved publish", review.Reviewer)
	}
	state.Status = ProductPublishStatusRejected
	state.completeActivity("human_review", temporalworkflow.Now(ctx), "Human review rejected publish")
	state.skipActivities(temporalworkflow.Now(ctx), "publish")
	state.CompletedAt = workflowTimestamp(temporalworkflow.Now(ctx))
	return false, record(ctx, input, "product_publish.review_rejected", state.Status, "human review rejected publish", review.Reviewer)
}

func runPublish(ctx temporalworkflow.Context, input ProductPublishInput, activityInput ProductPublishActivityInput, state *ProductPublishResult) error {
	state.Status = ProductPublishStatusPublishing
	if err := temporalworkflow.ExecuteActivity(ctx, PublishToWooCommerceActivity, activityInput).Get(ctx, &state.Publish); err != nil {
		state.Status = ProductPublishStatusFailed
		state.failActivity("publish", temporalworkflow.Now(ctx), "Publish to WooCommerce failed", err)
		state.CompletedAt = workflowTimestamp(temporalworkflow.Now(ctx))
		return err
	}
	state.Published = state.Publish.Published
	state.Status = ProductPublishStatusPublished
	state.completeActivity("publish", temporalworkflow.Now(ctx), "Published to WooCommerce")
	state.CompletedAt = workflowTimestamp(temporalworkflow.Now(ctx))
	return record(ctx, input, "product_publish.published", state.Status, "product published to WooCommerce", state.Review.Reviewer)
}

func receiveReviewSignal(ctx temporalworkflow.Context) (ReviewSignal, error) {
	var review ReviewSignal
	canceled := false
	selector := temporalworkflow.NewSelector(ctx)
	signalCh := temporalworkflow.GetSignalChannel(ctx, ProductPublishReviewSignal)
	selector.AddReceive(signalCh, func(ch temporalworkflow.ReceiveChannel, _ bool) {
		ch.Receive(ctx, &review)
	})
	selector.AddReceive(ctx.Done(), func(temporalworkflow.ReceiveChannel, bool) {
		canceled = true
	})
	selector.Select(ctx)
	if canceled {
		return ReviewSignal{}, temporal.NewCanceledError("product publish canceled while awaiting review")
	}
	return review, nil
}

func (a *ProductPublishActivities) CheckCompliance(ctx context.Context, input ProductPublishActivityInput) (ComplianceResult, error) {
	product, err := a.product(ctx, input.ProductID)
	if err != nil {
		return ComplianceResult{}, err
	}
	result := a.compliance.Evaluate(ctx, compliance.ProductContent{Product: product})
	return ComplianceResult{
		Pass:     result.Pass,
		Score:    result.Score,
		Reasons:  append([]string(nil), result.Reasons...),
		RuleIDs:  append([]string(nil), result.RuleIDs...),
		Severity: string(result.Severity),
	}, nil
}

func (a *ProductPublishActivities) ValidateMedia(ctx context.Context, input ProductPublishActivityInput) (MediaValidationResult, error) {
	product, err := a.product(ctx, input.ProductID)
	if err != nil {
		return MediaValidationResult{}, err
	}
	images := product.Images()
	if len(images) == 0 {
		return MediaValidationResult{Pass: false, Score: 0, Reasons: []string{"at least one product image is required"}}, nil
	}
	reasons := make([]string, 0)
	scoreTotal := 0
	for _, image := range images {
		result := a.media.Validate(media.ImageMetadata{URL: image.URL, AltText: image.Alt, ProductName: product.Title()})
		scoreTotal += result.Score
		if result.Pass {
			continue
		}
		for _, reason := range result.Reasons {
			reasons = append(reasons, reason.Message)
		}
	}
	return MediaValidationResult{
		Pass:          len(reasons) == 0,
		Score:         scoreTotal / len(images),
		Reasons:       reasons,
		ImagesChecked: len(images),
	}, nil
}

func (a *ProductPublishActivities) PublishToWooCommerce(ctx context.Context, input ProductPublishActivityInput) (PublishResult, error) {
	if _, err := parseProductID(input.ProductID); err != nil {
		return PublishResult{}, err
	}
	if a.publisher == nil {
		return PublishResult{}, errors.New("woocommerce publisher is not configured")
	}
	if err := a.publisher.PublishToWooCommerce(ctx, input.ProductID); err != nil {
		return PublishResult{}, err
	}
	return PublishResult{Published: true}, nil
}

func (a *ProductPublishActivities) RecordWorkflowEvent(ctx context.Context, event WorkflowEvent) error {
	if a.recorder == nil {
		return nil
	}
	return a.recorder.RecordWorkflowEvent(ctx, event)
}

func (a *ProductPublishActivities) product(ctx context.Context, productID string) (catalog.Product, error) {
	if a.products == nil {
		return catalog.Product{}, errors.New("product repository is not configured")
	}
	id, err := parseProductID(productID)
	if err != nil {
		return catalog.Product{}, err
	}
	product, err := a.products.GetByID(ctx, id)
	if err != nil {
		return catalog.Product{}, fmt.Errorf("get product %s: %w", productID, err)
	}
	return product, nil
}

func record(ctx temporalworkflow.Context, input ProductPublishInput, eventType, status, message, reviewer string) error {
	event := WorkflowEvent{
		ProductID:   input.ProductID,
		Type:        eventType,
		Status:      status,
		Message:     message,
		RequestedBy: input.RequestedBy,
		Reviewer:    reviewer,
		OccurredAt:  temporalworkflow.Now(ctx),
	}
	return temporalworkflow.ExecuteActivity(ctx, RecordWorkflowEventActivity, event).Get(ctx, nil)
}

func newProductPublishState(input ProductPublishInput, now time.Time) ProductPublishResult {
	timestamp := workflowTimestamp(now)
	return ProductPublishResult{
		ProductID:       input.ProductID,
		Status:          ProductPublishStatusDraft,
		CurrentActivity: "Check compliance",
		StartedAt:       timestamp,
		UpdatedAt:       timestamp,
		Activities: []ProductPublishLifecycleStage{
			{
				ID:        "check_compliance",
				Name:      "Check compliance",
				Status:    workflowActivityStatusRunning,
				StartedAt: timestamp,
				Message:   "Running compliance check",
				Attempt:   1,
			},
			{
				ID:     "validate_media",
				Name:   "Validate media",
				Status: workflowActivityStatusPending,
			},
			{
				ID:     "human_review",
				Name:   "Human review",
				Status: workflowActivityStatusPending,
			},
			{
				ID:     "publish",
				Name:   "Publish to WooCommerce",
				Status: workflowActivityStatusPending,
			},
		},
	}
}

func workflowTimestamp(now time.Time) string {
	return now.UTC().Format(time.RFC3339)
}

func (r *ProductPublishResult) startActivity(activityID string, now time.Time, message string) {
	stage := r.stage(activityID)
	if stage == nil {
		return
	}
	stage.Status = workflowActivityStatusRunning
	if stage.StartedAt == "" {
		stage.StartedAt = workflowTimestamp(now)
	}
	stage.Message = message
	stage.Attempt++
	if stage.Attempt == 0 {
		stage.Attempt = 1
	}
	r.CurrentActivity = stage.Name
	r.UpdatedAt = workflowTimestamp(now)
}

func (r *ProductPublishResult) waitForReview(now time.Time, message string) {
	stage := r.stage("human_review")
	if stage == nil {
		return
	}
	r.Status = ProductPublishStatusAwaitingReview
	stage.Status = workflowActivityStatusWaitingReview
	if stage.StartedAt == "" {
		stage.StartedAt = workflowTimestamp(now)
	}
	stage.Message = message
	stage.Attempt = 1
	r.CurrentActivity = message
	r.UpdatedAt = workflowTimestamp(now)
}

func (r *ProductPublishResult) completeActivity(activityID string, now time.Time, message string) {
	stage := r.stage(activityID)
	if stage == nil {
		return
	}
	stage.Status = workflowActivityStatusCompleted
	if stage.StartedAt == "" {
		stage.StartedAt = workflowTimestamp(now)
	}
	stage.CompletedAt = workflowTimestamp(now)
	stage.Message = message
	if stage.Attempt == 0 {
		stage.Attempt = 1
	}
	r.UpdatedAt = workflowTimestamp(now)
}

func (r *ProductPublishResult) failActivity(activityID string, now time.Time, message string, err error) {
	stage := r.stage(activityID)
	if stage == nil {
		return
	}
	stage.Status = workflowActivityStatusFailed
	if stage.StartedAt == "" {
		stage.StartedAt = workflowTimestamp(now)
	}
	stage.CompletedAt = workflowTimestamp(now)
	stage.Message = message
	if err != nil {
		stage.Error = err.Error()
	}
	if stage.Attempt == 0 {
		stage.Attempt = 1
	}
	r.CurrentActivity = stage.Name
	r.UpdatedAt = workflowTimestamp(now)
}

func (r *ProductPublishResult) skipActivities(now time.Time, activityIDs ...string) {
	for _, activityID := range activityIDs {
		stage := r.stage(activityID)
		if stage == nil || stage.Status != workflowActivityStatusPending {
			continue
		}
		stage.Status = workflowActivityStatusSkipped
		stage.Message = "Skipped"
	}
	r.UpdatedAt = workflowTimestamp(now)
}

func (r *ProductPublishResult) stage(activityID string) *ProductPublishLifecycleStage {
	for i := range r.Activities {
		if r.Activities[i].ID == activityID {
			return &r.Activities[i]
		}
	}
	return nil
}

func parseProductID(productID string) (uuid.UUID, error) {
	id, err := uuid.Parse(productID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid product id: %w", err)
	}
	return id, nil
}
