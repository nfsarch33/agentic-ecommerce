package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/compliance"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/media"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
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
	ProductPublishStatusPublished        = "published"
)

type ProductPublishInput struct {
	ProductID   string `json:"product_id"`
	RequestedBy string `json:"requested_by,omitempty"`
}

type ProductPublishActivityInput struct {
	ProductID string `json:"product_id"`
}

type ProductPublishResult struct {
	ProductID  string                `json:"product_id"`
	Status     string                `json:"status"`
	Published  bool                  `json:"published"`
	Compliance ComplianceResult      `json:"compliance"`
	Media      MediaValidationResult `json:"media"`
	Review     ReviewSignal          `json:"review"`
	Publish    PublishResult         `json:"publish"`
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

	state := ProductPublishResult{ProductID: input.ProductID, Status: ProductPublishStatusDraft}
	if err := temporalworkflow.SetQueryHandler(ctx, ProductPublishStatusQuery, func() (ProductPublishResult, error) {
		return state, nil
	}); err != nil {
		return state, err
	}

	activityInput := ProductPublishActivityInput{ProductID: input.ProductID}
	if err := record(ctx, input, "product_publish.started", ProductPublishStatusDraft, "product publish workflow started", ""); err != nil {
		return state, err
	}

	if err := temporalworkflow.ExecuteActivity(ctx, CheckComplianceActivity, activityInput).Get(ctx, &state.Compliance); err != nil {
		state.Status = ProductPublishStatusComplianceFailed
		return state, err
	}
	if !state.Compliance.Pass {
		state.Status = ProductPublishStatusComplianceFailed
		if err := record(ctx, input, "product_publish.compliance_failed", state.Status, "compliance check failed", ""); err != nil {
			return state, err
		}
		return state, nil
	}
	if err := record(ctx, input, "product_publish.compliance_passed", state.Status, "compliance check passed", ""); err != nil {
		return state, err
	}

	if err := temporalworkflow.ExecuteActivity(ctx, ValidateMediaActivity, activityInput).Get(ctx, &state.Media); err != nil {
		state.Status = ProductPublishStatusMediaFailed
		return state, err
	}
	if !state.Media.Pass {
		state.Status = ProductPublishStatusMediaFailed
		if err := record(ctx, input, "product_publish.media_failed", state.Status, "media validation failed", ""); err != nil {
			return state, err
		}
		return state, nil
	}
	if err := record(ctx, input, "product_publish.media_validated", state.Status, "media validation passed", ""); err != nil {
		return state, err
	}

	state.Status = ProductPublishStatusAwaitingReview
	var review ReviewSignal
	temporalworkflow.GetSignalChannel(ctx, ProductPublishReviewSignal).Receive(ctx, &review)
	state.Review = review
	if !review.Approved {
		state.Status = ProductPublishStatusRejected
		if err := record(ctx, input, "product_publish.review_rejected", state.Status, "human review rejected publish", review.Reviewer); err != nil {
			return state, err
		}
		return state, nil
	}
	if err := record(ctx, input, "product_publish.review_approved", state.Status, "human review approved publish", review.Reviewer); err != nil {
		return state, err
	}

	state.Status = ProductPublishStatusPublishing
	if err := temporalworkflow.ExecuteActivity(ctx, PublishToWooCommerceActivity, activityInput).Get(ctx, &state.Publish); err != nil {
		return state, err
	}
	state.Published = state.Publish.Published
	state.Status = ProductPublishStatusPublished
	if err := record(ctx, input, "product_publish.published", state.Status, "product published to WooCommerce", review.Reviewer); err != nil {
		return state, err
	}
	return state, nil
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

func parseProductID(productID string) (uuid.UUID, error) {
	id, err := uuid.Parse(productID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid product id: %w", err)
	}
	return id, nil
}
