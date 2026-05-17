package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

const (
	MediaSourceActivity      = "media_processing.source"
	MediaApproveActivity     = "media_processing.approve"
	MediaProcessActivity     = "media_processing.process"
	MediaQualityActivity     = "media_processing.quality"
	MediaStoreActivity       = "media_processing.store"
	MediaLinkProductActivity = "media_processing.link_product"

	MediaProcessingStatusSourced       = "sourced"
	MediaProcessingStatusProcessed     = "processed"
	MediaProcessingStatusQualityFailed = "quality_failed"
	MediaProcessingStatusStored        = "stored"
	MediaProcessingStatusLinked        = "linked"
)

type MediaProcessingInput struct {
	ProductID        string                     `json:"product_id"`
	SourceURL        string                     `json:"source_url"`
	AltText          string                     `json:"alt_text,omitempty"`
	RequestedBy      string                     `json:"requested_by,omitempty"`
	Resize           intelligence.ResizeOptions `json:"resize,omitempty"`
	Format           string                     `json:"format,omitempty"`
	RemoveBackground bool                       `json:"remove_background,omitempty"`
}

type MediaProcessingResult struct {
	ProductID string                     `json:"product_id"`
	MediaID   string                     `json:"media_id"`
	Status    string                     `json:"status"`
	Source    intelligence.Asset         `json:"source,omitempty"`
	Processed intelligence.Asset         `json:"processed,omitempty"`
	Quality   intelligence.QualityReport `json:"quality,omitempty"`
	Stored    intelligence.Asset         `json:"stored,omitempty"`
	Link      MediaProductLinkResult     `json:"link,omitempty"`
}

type MediaProcessActivityInput struct {
	MediaID          string                     `json:"media_id"`
	Resize           intelligence.ResizeOptions `json:"resize,omitempty"`
	Format           string                     `json:"format,omitempty"`
	RemoveBackground bool                       `json:"remove_background,omitempty"`
}

type MediaReviewActivityInput struct {
	MediaID   string `json:"media_id"`
	Reviewer  string `json:"reviewer"`
	Note      string `json:"note,omitempty"`
}

type MediaQualityActivityInput struct {
	MediaID string `json:"media_id"`
}

type MediaStoreActivityInput struct {
	MediaID string `json:"media_id"`
}

type MediaProductLinkInput struct {
	ProductID string                   `json:"product_id"`
	MediaID   string                   `json:"media_id"`
	Storage   intelligence.StorageInfo `json:"storage"`
	AltText   string                   `json:"alt_text,omitempty"`
}

type MediaProductLinkResult struct {
	Linked     bool   `json:"linked"`
	ProductID  string `json:"product_id"`
	MediaID    string `json:"media_id"`
	StorageKey string `json:"storage_key,omitempty"`
}

type MediaProcessingActivityDeps struct {
	Media    *intelligence.Service
	Products port.ProductRepository
	Linker   MediaProductLinker
}

type MediaProductLinker interface {
	LinkMediaToProduct(context.Context, MediaProductLinkInput) (MediaProductLinkResult, error)
}

type MediaProcessingActivities struct {
	media    *intelligence.Service
	products port.ProductRepository
	linker   MediaProductLinker
}

func NewMediaProcessingActivities(deps MediaProcessingActivityDeps) *MediaProcessingActivities {
	return &MediaProcessingActivities{
		media:    deps.Media,
		products: deps.Products,
		linker:   deps.Linker,
	}
}

func MediaProcessingWorkflow(ctx temporalworkflow.Context, input MediaProcessingInput) (MediaProcessingResult, error) {
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

	result := MediaProcessingResult{ProductID: input.ProductID}
	if err := temporalworkflow.ExecuteActivity(ctx, MediaSourceActivity, input).Get(ctx, &result.Source); err != nil {
		return result, err
	}
	result.MediaID = result.Source.ID
	result.Status = MediaProcessingStatusSourced

	approvalInput := MediaReviewActivityInput{
		MediaID:  result.Source.ID,
		Reviewer: mediaWorkflowReviewer(input.RequestedBy),
		Note:     "Approved via media processing workflow request",
	}
	if err := temporalworkflow.ExecuteActivity(ctx, MediaApproveActivity, approvalInput).Get(ctx, &result.Source); err != nil {
		return result, err
	}

	processInput := MediaProcessActivityInput{
		MediaID:          result.Source.ID,
		Resize:           input.Resize,
		Format:           input.Format,
		RemoveBackground: input.RemoveBackground,
	}
	if err := temporalworkflow.ExecuteActivity(ctx, MediaProcessActivity, processInput).Get(ctx, &result.Processed); err != nil {
		return result, err
	}
	result.MediaID = result.Processed.ID
	result.Status = MediaProcessingStatusProcessed

	if err := temporalworkflow.ExecuteActivity(ctx, MediaQualityActivity, MediaQualityActivityInput{MediaID: result.Processed.ID}).Get(ctx, &result.Quality); err != nil {
		return result, err
	}
	if !result.Quality.Pass {
		result.Status = MediaProcessingStatusQualityFailed
		return result, nil
	}

	if err := temporalworkflow.ExecuteActivity(ctx, MediaStoreActivity, MediaStoreActivityInput{MediaID: result.Processed.ID}).Get(ctx, &result.Stored); err != nil {
		return result, err
	}
	result.Status = MediaProcessingStatusStored

	linkInput := MediaProductLinkInput{
		ProductID: input.ProductID,
		MediaID:   result.Processed.ID,
		Storage:   result.Stored.Storage,
		AltText:   input.AltText,
	}
	if err := temporalworkflow.ExecuteActivity(ctx, MediaLinkProductActivity, linkInput).Get(ctx, &result.Link); err != nil {
		return result, err
	}
	result.Status = MediaProcessingStatusLinked
	return result, nil
}

func (a *MediaProcessingActivities) SourceMedia(ctx context.Context, input MediaProcessingInput) (intelligence.Asset, error) {
	if a.media == nil {
		return intelligence.Asset{}, errors.New("media intelligence service is not configured")
	}
	return a.media.Source(ctx, intelligence.SourceRequest{
		URL:       input.SourceURL,
		ProductID: input.ProductID,
		AltText:   input.AltText,
	})
}

func (a *MediaProcessingActivities) ApproveMedia(ctx context.Context, input MediaReviewActivityInput) (intelligence.Asset, error) {
	if a.media == nil {
		return intelligence.Asset{}, errors.New("media intelligence service is not configured")
	}
	return a.media.Approve(ctx, input.MediaID, intelligence.ReviewRequest{
		Reviewer: input.Reviewer,
		Note:     input.Note,
	})
}

func (a *MediaProcessingActivities) ProcessMedia(ctx context.Context, input MediaProcessActivityInput) (intelligence.Asset, error) {
	if a.media == nil {
		return intelligence.Asset{}, errors.New("media intelligence service is not configured")
	}
	return a.media.Process(ctx, intelligence.ProcessRequest{
		MediaID:          input.MediaID,
		Resize:           input.Resize,
		Format:           input.Format,
		RemoveBackground: input.RemoveBackground,
	})
}

func (a *MediaProcessingActivities) AssessMediaQuality(ctx context.Context, input MediaQualityActivityInput) (intelligence.QualityReport, error) {
	if a.media == nil {
		return intelligence.QualityReport{}, errors.New("media intelligence service is not configured")
	}
	return a.media.Validate(ctx, input.MediaID)
}

func (a *MediaProcessingActivities) StoreMedia(ctx context.Context, input MediaStoreActivityInput) (intelligence.Asset, error) {
	if a.media == nil {
		return intelligence.Asset{}, errors.New("media intelligence service is not configured")
	}
	return a.media.Store(ctx, input.MediaID)
}

func (a *MediaProcessingActivities) LinkMediaToProduct(ctx context.Context, input MediaProductLinkInput) (MediaProductLinkResult, error) {
	if input.ProductID == "" {
		return MediaProductLinkResult{}, errors.New("product id is required")
	}
	if input.MediaID == "" {
		return MediaProductLinkResult{}, errors.New("media id is required")
	}
	if a.products != nil {
		if id, err := uuid.Parse(input.ProductID); err == nil {
			if _, err := a.products.GetByID(ctx, id); err != nil {
				return MediaProductLinkResult{}, fmt.Errorf("get product for media link: %w", err)
			}
		}
	}
	if a.linker != nil {
		return a.linker.LinkMediaToProduct(ctx, input)
	}
	return MediaProductLinkResult{Linked: true, ProductID: input.ProductID, MediaID: input.MediaID, StorageKey: input.Storage.Key}, nil
}

func mediaWorkflowReviewer(requestedBy string) string {
	if reviewer := strings.TrimSpace(requestedBy); reviewer != "" {
		return reviewer
	}
	return "media-workflow"
}
