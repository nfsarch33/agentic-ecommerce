package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
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

func TestGetenvTrimsAndFallsBack(t *testing.T) {
	t.Setenv("ECOMMERCE_TEMPORAL_TEST_VALUE", "  configured  ")
	if got := getenv("ECOMMERCE_TEMPORAL_TEST_VALUE", "fallback"); got != "configured" {
		t.Fatalf("getenv configured = %q", got)
	}
	if got := getenv("ECOMMERCE_TEMPORAL_TEST_MISSING", "fallback"); got != "fallback" {
		t.Fatalf("getenv fallback = %q", got)
	}
}

type fakeSyncEngine struct {
	productID uuid.UUID
}

func (f *fakeSyncEngine) PublishToWooCommerce(_ context.Context, id uuid.UUID) error {
	f.productID = id
	return nil
}
