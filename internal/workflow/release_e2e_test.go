package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
	"github.com/nfsarch33/agentic-ecommerce/internal/webhook/outbound"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestV200ReleaseBackendFlowCompletesWithLocalMocks(t *testing.T) {
	t.Parallel()

	productID := assertReleaseContentWorkflow(t)
	assertReleaseMediaWorkflow(t, productID)
	assertReleasePublishWorkflow(t)
	assertReleaseN8NWebhookDelivery(t, productID)
}

func assertReleaseContentWorkflow(t *testing.T) string {
	t.Helper()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerContentGenerationTestActivities(env)

	input := ContentGenerationInput{
		Product: contentagent.ProductInfo{ID: "release-product", Title: "Resistance Band Set"},
		Request: contentagent.GenerateRequest{
			Product:  contentagent.ProductInfo{ID: "release-product", Title: "Resistance Band Set"},
			Style:    "professional",
			MaxWords: 120,
			Keywords: []string{"home workouts", "resistance band set"},
		},
		RequestedBy: "release-demo",
	}
	generated := contentagent.GenerateResult{
		GeneratedContent: contentagent.GeneratedContent{
			Description:     "Resistance Band Set includes five resistance levels for home workouts.",
			SEOTitle:        "Resistance Band Set for Home Workouts",
			MetaDescription: "Resistance Band Set with five progressive resistance levels.",
		},
		Evaluation: contentagent.Evaluation{Score: 94, Pass: true},
		TokensUsed: 42,
	}
	factCheck := contentagent.FactCheckResult{ProductID: input.Product.ID, Pass: true, Confidence: 0.93}
	evaluation := contentagent.Evaluation{Score: 94, Pass: true}

	env.OnActivity(ContentGenerateActivity, mock.Anything, input).Return(generated, nil).Once()
	env.OnActivity(ContentFactCheckActivity, mock.Anything, ContentFactCheckActivityInput{
		ProductID: input.Product.ID,
		Content:   generated.GeneratedContent,
	}).Return(factCheck, nil).Once()
	env.OnActivity(ContentEvaluateActivity, mock.Anything, ContentEvaluateActivityInput{
		Request: input.Request,
		Result:  generated,
	}).Return(evaluation, nil).Once()
	env.OnActivity(RecordContentFactCheckActivity, mock.Anything, mock.MatchedBy(func(result ContentGenerationResult) bool {
		return result.Approved && result.Status == ContentGenerationStatusApproved
	})).Return(nil).Once()

	env.ExecuteWorkflow(ContentGenerationWorkflow, input)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("content workflow error: %v", err)
	}
	var result ContentGenerationResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("content workflow result: %v", err)
	}
	if !result.Approved || result.Status != ContentGenerationStatusApproved || !result.FactCheck.Pass {
		t.Fatalf("content result = %+v, want approved fact-checked content", result)
	}
	return input.Product.ID
}

func assertReleaseMediaWorkflow(t *testing.T, productID string) {
	t.Helper()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMediaProcessingTestActivities(env)

	input := MediaProcessingInput{
		ProductID:   productID,
		SourceURL:   "http://127.0.0.1:18081/fixtures/resistance-band.png",
		AltText:     "Resistance band set with five tension levels",
		RequestedBy: "release-demo",
		Format:      "webp",
	}
	source := intelligence.Asset{ID: "release-media-source", ProductID: productID, SourceURL: input.SourceURL, AltText: input.AltText}
	processed := intelligence.Asset{ID: "release-media-processed", ProductID: productID, SourceURL: input.SourceURL, AltText: input.AltText}
	stored := processed
	stored.Storage = intelligence.StorageInfo{Key: "products/release-product/hero.webp", URL: "/media/products/release-product/hero.webp"}
	quality := intelligence.QualityReport{Pass: true, Score: 96}
	link := MediaProductLinkResult{Linked: true, ProductID: productID, MediaID: processed.ID, StorageKey: stored.Storage.Key}

	env.OnActivity(MediaSourceActivity, mock.Anything, input).Return(source, nil).Once()
	env.OnActivity(MediaProcessActivity, mock.Anything, MediaProcessActivityInput{MediaID: source.ID, Format: input.Format}).Return(processed, nil).Once()
	env.OnActivity(MediaQualityActivity, mock.Anything, MediaQualityActivityInput{MediaID: processed.ID}).Return(quality, nil).Once()
	env.OnActivity(MediaStoreActivity, mock.Anything, MediaStoreActivityInput{MediaID: processed.ID}).Return(stored, nil).Once()
	env.OnActivity(MediaLinkProductActivity, mock.Anything, MediaProductLinkInput{
		ProductID: productID,
		MediaID:   processed.ID,
		Storage:   stored.Storage,
		AltText:   input.AltText,
	}).Return(link, nil).Once()

	env.ExecuteWorkflow(MediaProcessingWorkflow, input)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("media workflow error: %v", err)
	}
	var result MediaProcessingResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("media workflow result: %v", err)
	}
	if result.Status != MediaProcessingStatusLinked || !result.Quality.Pass || !result.Link.Linked {
		t.Fatalf("media result = %+v, want linked MIS asset", result)
	}
}

func assertReleasePublishWorkflow(t *testing.T) {
	t.Helper()

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
		env.SignalWorkflow(ProductPublishReviewSignal, ReviewSignal{Approved: true, Reviewer: "release-demo"})
	}, time.Minute)

	env.ExecuteWorkflow(ProductPublishWorkflow, ProductPublishInput{ProductID: repo.productID, RequestedBy: "release-demo"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("publish workflow error: %v", err)
	}
	var result ProductPublishResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("publish workflow result: %v", err)
	}
	if result.Status != ProductPublishStatusPublished || !result.Published || !result.Compliance.Pass || !result.Media.Pass {
		t.Fatalf("publish result = %+v, want compliant mocked WooCommerce publish", result)
	}
	if publisher.publishedID != repo.productID || len(recorder.events) != 5 {
		t.Fatalf("publisher=%q events=%d, want publish and five events", publisher.publishedID, len(recorder.events))
	}
}

func assertReleaseN8NWebhookDelivery(t *testing.T, productID string) {
	t.Helper()

	requests := 0
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-EC-Webhook-Signature") == "" {
			t.Error("missing webhook signature")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	store := outbound.NewInMemoryStore()
	reg, err := store.CreateRegistration(context.Background(), outbound.CreateRegistrationInput{
		URL:        receiver.URL,
		EventTypes: []eventbus.EventType{eventbus.OrderPlaced},
		Secret:     "release-local-secret",
	})
	if err != nil {
		t.Fatalf("register local n8n webhook: %v", err)
	}
	service := outbound.NewService(outbound.ServiceConfig{
		Store: store,
		Client: outbound.NewClient(outbound.ClientConfig{
			HTTPClient:  receiver.Client(),
			MaxAttempts: 1,
			Backoff:     func(int) time.Duration { return 0 },
		}),
	})

	results, err := service.DeliverEvent(context.Background(), eventbus.Event{
		ID:        "evt-release-order",
		Type:      eventbus.OrderPlaced,
		TenantID:  "tenant-release-demo",
		Timestamp: time.Now().UTC(),
		Source:    "release-e2e",
		Payload:   map[string]any{"product_id": productID},
	})
	if err != nil {
		t.Fatalf("deliver local n8n webhook: %v", err)
	}
	if requests != 1 || len(results) != 1 || results[0].WebhookID != reg.ID || !results[0].Success {
		t.Fatalf("requests=%d results=%+v, want one successful local n8n delivery", requests, results)
	}
}
