package workflow

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestMediaProcessingWorkflowSourcesProcessesQALinksAndStores(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMediaProcessingTestActivities(env)

	input := MediaProcessingInput{
		ProductID:   "product-123",
		SourceURL:   "https://supplier.example/images/lamp.png",
		AltText:     "Matte black desk lamp on white background",
		RequestedBy: "operator@example.com",
	}
	source := intelligence.Asset{ID: "source-media", ProductID: input.ProductID, SourceURL: input.SourceURL}
	processed := intelligence.Asset{ID: "processed-media", ProductID: input.ProductID, SourceURL: input.SourceURL}
	quality := intelligence.QualityReport{Pass: true, Score: 100}
	stored := processed
	stored.Storage = intelligence.StorageInfo{Key: "products/product-123/media/processed-media.webp", URL: "/media/products/product-123/media/processed-media.webp"}
	link := MediaProductLinkResult{Linked: true, ProductID: input.ProductID, MediaID: processed.ID}

	env.OnActivity(MediaSourceActivity, mock.Anything, input).Return(source, nil).Once()
	env.OnActivity(MediaProcessActivity, mock.Anything, MediaProcessActivityInput{MediaID: source.ID}).Return(processed, nil).Once()
	env.OnActivity(MediaQualityActivity, mock.Anything, MediaQualityActivityInput{MediaID: processed.ID}).Return(quality, nil).Once()
	env.OnActivity(MediaStoreActivity, mock.Anything, MediaStoreActivityInput{MediaID: processed.ID}).Return(stored, nil).Once()
	env.OnActivity(MediaLinkProductActivity, mock.Anything, MediaProductLinkInput{
		ProductID: input.ProductID,
		MediaID:   processed.ID,
		Storage:   stored.Storage,
		AltText:   input.AltText,
	}).Return(link, nil).Once()

	env.ExecuteWorkflow(MediaProcessingWorkflow, input)
	env.AssertExpectations(t)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MediaProcessingResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != MediaProcessingStatusLinked || result.MediaID != processed.ID || !result.Quality.Pass || !result.Link.Linked {
		t.Fatalf("result = %+v", result)
	}
}

func TestMediaProcessingWorkflowStopsWhenQualityFails(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMediaProcessingTestActivities(env)

	input := MediaProcessingInput{ProductID: "product-123", SourceURL: "https://supplier.example/images/lamp.png"}
	source := intelligence.Asset{ID: "source-media", ProductID: input.ProductID, SourceURL: input.SourceURL}
	processed := intelligence.Asset{ID: "processed-media", ProductID: input.ProductID, SourceURL: input.SourceURL}
	quality := intelligence.QualityReport{Pass: false, Score: 40, Issues: []intelligence.QualityIssue{{ID: "resolution_too_small"}}}

	env.OnActivity(MediaSourceActivity, mock.Anything, input).Return(source, nil).Once()
	env.OnActivity(MediaProcessActivity, mock.Anything, MediaProcessActivityInput{MediaID: source.ID}).Return(processed, nil).Once()
	env.OnActivity(MediaQualityActivity, mock.Anything, MediaQualityActivityInput{MediaID: processed.ID}).Return(quality, nil).Once()

	env.ExecuteWorkflow(MediaProcessingWorkflow, input)
	env.AssertExpectations(t)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MediaProcessingResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != MediaProcessingStatusQualityFailed || result.Quality.Pass {
		t.Fatalf("result = %+v, want quality failure", result)
	}
}

func TestMediaProcessingActivitiesUseMediaServiceAndNoopLinker(t *testing.T) {
	t.Parallel()

	store := &mediaActivityStore{}
	service := intelligence.NewService(intelligence.ServiceConfig{
		HTTPClient: mediaActivityClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(strings.NewReader(mediaActivityPNGString())),
			}, nil
		}),
		Store: store,
	})
	activities := NewMediaProcessingActivities(MediaProcessingActivityDeps{Media: service})

	source, err := activities.SourceMedia(context.Background(), MediaProcessingInput{
		ProductID: "product-123",
		SourceURL: "https://supplier.example/images/lamp.png",
		AltText:   "Matte black desk lamp on white background",
	})
	if err != nil {
		t.Fatalf("SourceMedia: %v", err)
	}
	processed, err := activities.ProcessMedia(context.Background(), MediaProcessActivityInput{
		MediaID: source.ID,
		Format:  "webp",
	})
	if err != nil {
		t.Fatalf("ProcessMedia: %v", err)
	}
	qa, err := activities.AssessMediaQuality(context.Background(), MediaQualityActivityInput{MediaID: processed.ID})
	if err != nil {
		t.Fatalf("AssessMediaQuality: %v", err)
	}
	if qa.Pass {
		t.Fatalf("qa = %+v, want tiny fixture to fail quality", qa)
	}
	stored, err := activities.StoreMedia(context.Background(), MediaStoreActivityInput{MediaID: processed.ID})
	if err != nil {
		t.Fatalf("StoreMedia: %v", err)
	}
	link, err := activities.LinkMediaToProduct(context.Background(), MediaProductLinkInput{
		ProductID: source.ProductID,
		MediaID:   processed.ID,
		Storage:   stored.Storage,
		AltText:   source.AltText,
	})
	if err != nil {
		t.Fatalf("LinkMediaToProduct: %v", err)
	}
	if !link.Linked || link.StorageKey == "" || store.body == "" {
		t.Fatalf("link=%+v store=%+v", link, store)
	}
}

func TestMediaProcessingActivitiesRequireConfiguredService(t *testing.T) {
	t.Parallel()

	activities := NewMediaProcessingActivities(MediaProcessingActivityDeps{})
	if _, err := activities.SourceMedia(context.Background(), MediaProcessingInput{}); err == nil {
		t.Fatal("SourceMedia err = nil, want configured service error")
	}
	if _, err := activities.ProcessMedia(context.Background(), MediaProcessActivityInput{}); err == nil {
		t.Fatal("ProcessMedia err = nil, want configured service error")
	}
	if _, err := activities.AssessMediaQuality(context.Background(), MediaQualityActivityInput{}); err == nil {
		t.Fatal("AssessMediaQuality err = nil, want configured service error")
	}
	if _, err := activities.StoreMedia(context.Background(), MediaStoreActivityInput{}); err == nil {
		t.Fatal("StoreMedia err = nil, want configured service error")
	}
	if _, err := activities.LinkMediaToProduct(context.Background(), MediaProductLinkInput{}); err == nil {
		t.Fatal("LinkMediaToProduct err = nil, want validation error")
	}
}

func registerMediaProcessingTestActivities(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(context.Context, MediaProcessingInput) (intelligence.Asset, error) {
		return intelligence.Asset{}, nil
	}, activity.RegisterOptions{Name: MediaSourceActivity})
	env.RegisterActivityWithOptions(func(context.Context, MediaProcessActivityInput) (intelligence.Asset, error) {
		return intelligence.Asset{}, nil
	}, activity.RegisterOptions{Name: MediaProcessActivity})
	env.RegisterActivityWithOptions(func(context.Context, MediaQualityActivityInput) (intelligence.QualityReport, error) {
		return intelligence.QualityReport{}, nil
	}, activity.RegisterOptions{Name: MediaQualityActivity})
	env.RegisterActivityWithOptions(func(context.Context, MediaStoreActivityInput) (intelligence.Asset, error) {
		return intelligence.Asset{}, nil
	}, activity.RegisterOptions{Name: MediaStoreActivity})
	env.RegisterActivityWithOptions(func(context.Context, MediaProductLinkInput) (MediaProductLinkResult, error) {
		return MediaProductLinkResult{}, nil
	}, activity.RegisterOptions{Name: MediaLinkProductActivity})
}

type mediaActivityClient func(*http.Request) (*http.Response, error)

func (c mediaActivityClient) Do(req *http.Request) (*http.Response, error) {
	return c(req)
}

type mediaActivityStore struct {
	body string
}

func (s *mediaActivityStore) Put(_ context.Context, object intelligence.MediaObject) (intelligence.StoredMediaObject, error) {
	body, err := io.ReadAll(object.Body)
	if err != nil {
		return intelligence.StoredMediaObject{}, err
	}
	s.body = string(body)
	return intelligence.StoredMediaObject{Key: object.Key, URL: "/media/" + object.Key, ContentType: object.ContentType, SizeBytes: int64(len(body))}, nil
}

func (s *mediaActivityStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}

func (s *mediaActivityStore) Delete(context.Context, string) error {
	return nil
}

func mediaActivityPNGString() string {
	raw, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	return string(raw)
}
