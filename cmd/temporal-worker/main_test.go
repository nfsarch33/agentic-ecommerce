package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
)

func TestSyncPublisherParsesProductID(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	engine := &fakeSyncEngine{}
	publisher := syncPublisher{engine: engine}

	if err := publisher.PublishToWooCommerce(context.Background(), id.String()); err != nil {
		t.Fatalf("PublishToWooCommerce: %v", err)
	}
	if engine.productID != id {
		t.Fatalf("product id = %s, want %s", engine.productID, id)
	}
}

func TestSyncPublisherRejectsInvalidProductID(t *testing.T) {
	t.Parallel()

	publisher := syncPublisher{engine: &fakeSyncEngine{}}
	if err := publisher.PublishToWooCommerce(context.Background(), "not-a-uuid"); err == nil {
		t.Fatal("expected invalid product id error")
	}
}

func TestLogRecorderAcceptsWorkflowEvents(t *testing.T) {
	t.Parallel()

	recorder := logRecorder{logger: slog.Default()}
	err := recorder.RecordWorkflowEvent(context.Background(), ecworkflow.WorkflowEvent{ProductID: uuid.NewString(), Type: "product_publish.started"})
	if err != nil {
		t.Fatalf("RecordWorkflowEvent: %v", err)
	}
}

func TestNewContentGenerationActivitiesFromEnvCreatesFactChecker(t *testing.T) {
	t.Setenv("ECOMMERCE_AI_BRIDGE_URL", "")
	t.Setenv("MINIMAX_BRIDGE_URL", "")

	activities := newContentGenerationActivitiesFromEnv(slog.Default())
	result, err := activities.FactCheckContent(context.Background(), ecworkflow.ContentFactCheckActivityInput{
		ProductID: uuid.NewString(),
		Content:   contentagent.GeneratedContent{Description: "Plain marketing copy without factual claims."},
	})
	if err != nil {
		t.Fatalf("FactCheckContent: %v", err)
	}
	if !result.Pass || result.Confidence != 1 {
		t.Fatalf("fact check = %+v, want no-claim pass", result)
	}
}

func TestNewMediaProcessingActivitiesFromEnvCreatesLocalStoreBackedActivities(t *testing.T) {
	t.Setenv("ECOMMERCE_MEDIA_STORE_PROVIDER", "local")
	t.Setenv("ECOMMERCE_MEDIA_STORE_ROOT", t.TempDir())
	t.Setenv("ECOMMERCE_MEDIA_PUBLIC_BASE_URL", "/media")

	activities := newMediaProcessingActivitiesFromEnv(slog.Default(), nil)
	link, err := activities.LinkMediaToProduct(context.Background(), ecworkflow.MediaProductLinkInput{
		ProductID: "product-123",
		MediaID:   "media-123",
	})
	if err != nil {
		t.Fatalf("LinkMediaToProduct: %v", err)
	}
	if !link.Linked || link.ProductID != "product-123" || link.MediaID != "media-123" {
		t.Fatalf("link = %+v", link)
	}
}

func TestContentFactCheckLogRecorderAcceptsResults(t *testing.T) {
	t.Parallel()

	recorder := contentFactCheckLogRecorder{logger: slog.Default()}
	err := recorder.RecordContentFactCheck(context.Background(), ecworkflow.ContentGenerationResult{
		ProductID: uuid.NewString(),
		Status:    ecworkflow.ContentGenerationStatusApproved,
		FactCheck: contentagent.FactCheckResult{
			Confidence: 0.91,
		},
	})
	if err != nil {
		t.Fatalf("RecordContentFactCheck: %v", err)
	}
}

func TestGetenvTrimsAndFallsBack(t *testing.T) {
	t.Setenv("ECOMMERCE_TEMPORAL_TEST_VALUE", "  configured  ")
	if got := getenv("ECOMMERCE_TEMPORAL_TEST_VALUE", "fallback"); got != "configured" {
		t.Fatalf("getenv configured = %q", got)
	}
	if got := getenv("ECOMMERCE_TEMPORAL_TEST_MISSING", "fallback"); got != "fallback" {
		t.Fatalf("getenv fallback = %q", got)
	}
}

func TestTemporalAddressFromEnvAcceptsComposeAlias(t *testing.T) {
	t.Setenv("ECOMMERCE_TEMPORAL_ADDR", "")
	t.Setenv("ECOMMERCE_TEMPORAL_ADDRESS", " temporal:7233 ")

	if got := temporalAddressFromEnv(); got != "temporal:7233" {
		t.Fatalf("temporal address = %q, want compose alias", got)
	}
}

func TestTemporalAddressFromEnvPrefersLegacyAddr(t *testing.T) {
	t.Setenv("ECOMMERCE_TEMPORAL_ADDR", " 127.0.0.1:7233 ")
	t.Setenv("ECOMMERCE_TEMPORAL_ADDRESS", "temporal:7233")

	if got := temporalAddressFromEnv(); got != "127.0.0.1:7233" {
		t.Fatalf("temporal address = %q, want legacy addr", got)
	}
}

func TestNewProductRepositoryFromEnvFallsBackToInMemoryWithoutDSN(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_URL", "")

	repo, cleanup, err := newProductRepositoryFromEnv(context.Background(), nil)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	if repo == nil {
		t.Fatal("repo is nil")
	}
	cleanup()
}

func TestNewProductRepositoryFromEnvRejectsInvalidDSN(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_URL", "://not-a-valid-dsn")

	repo, cleanup, err := newProductRepositoryFromEnv(context.Background(), nil)
	if err == nil {
		t.Fatal("expected invalid DSN error")
	}
	if repo != nil || cleanup != nil {
		t.Fatal("repo and cleanup should be nil on error")
	}
}

type fakeSyncEngine struct {
	productID uuid.UUID
}

func (f *fakeSyncEngine) PublishToWooCommerce(_ context.Context, id uuid.UUID) error {
	f.productID = id
	return nil
}
