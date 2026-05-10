// File scope: v3.9.1 EC-4-4 Instagram stub adapter RED tests.
//
// Acceptance:
//   - Name() returns "instagram".
//   - UpdateOrderStatus returns ErrChannelNotImplemented.
//   - CreateListing returns ErrChannelNotImplemented.
//   - Stub adapter calls return within 10ms (no external I/O).
package social

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/fulfilment"
	"github.com/nfsarch33/agentic-ecommerce/internal/channelport"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

func TestInstagramStub_NameReturnsInstagram(t *testing.T) {
	t.Parallel()
	a, err := NewInstagramStubAdapter(nil, "tenant-v391")
	if err != nil {
		t.Fatalf("NewInstagramStubAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	if got := a.Name(); got != InstagramChannelName {
		t.Fatalf("Name=%q want=%q", got, InstagramChannelName)
	}
	if got := a.ChannelName(); got != InstagramChannelName {
		t.Fatalf("ChannelName=%q want=%q", got, InstagramChannelName)
	}
}

func TestInstagramStub_UpdateOrderStatusReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	a, err := NewInstagramStubAdapter(nil, "tenant-v391")
	if err != nil {
		t.Fatalf("NewInstagramStubAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	start := time.Now()
	err = a.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID:        "tenant-v391",
		ExternalOrderID: "ig-order-1",
		Status:          "shipped",
		TrackingNumber:  "ETK-001",
		DeliveryDate:    time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
	})
	dur := time.Since(start)
	if !errors.Is(err, ErrChannelNotImplemented) {
		t.Fatalf("UpdateOrderStatus err=%v want ErrChannelNotImplemented", err)
	}
	if dur > 10*time.Millisecond {
		t.Fatalf("stub UpdateOrderStatus took %s; want <10ms", dur)
	}
}

func TestInstagramStub_CreateListingReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	a, err := NewInstagramStubAdapter(nil, "tenant-v391")
	if err != nil {
		t.Fatalf("NewInstagramStubAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	start := time.Now()
	err = a.CreateListing(context.Background(), channelport.ListingRequest{
		TenantID:      "tenant-v391",
		ProductID:     "sku-1",
		Channel:       InstagramChannelName,
		Title:         "Sample IG product",
		PriceAUDCents: 4990,
	})
	dur := time.Since(start)
	if !errors.Is(err, ErrChannelNotImplemented) {
		t.Fatalf("CreateListing err=%v want ErrChannelNotImplemented", err)
	}
	if dur > 10*time.Millisecond {
		t.Fatalf("stub CreateListing took %s; want <10ms", dur)
	}
}

func TestInstagramStub_PublishReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	a, err := NewInstagramStubAdapter(nil, "tenant-v391")
	if err != nil {
		t.Fatalf("NewInstagramStubAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	payload := eventbus.ProductEnrichedPayload{
		Version:      eventbus.ProductEnrichedPayloadVersion,
		TenantID:     "tenant-v391",
		ProductID:    "sku-1",
		EnglishTitle: "Sample IG product",
		PriceCents:   4990,
		Currency:     "AUD",
	}
	if err := a.Publish(context.Background(), payload); !errors.Is(err, ErrChannelNotImplemented) {
		t.Fatalf("Publish err=%v want ErrChannelNotImplemented", err)
	}
}

func TestInstagramStub_RequiresTenantID(t *testing.T) {
	t.Parallel()
	if _, err := NewInstagramStubAdapter(nil, ""); err == nil {
		t.Fatal("expected error for empty tenant_id")
	}
}

func TestInstagramStub_MetricsHookCalledOnEachOp(t *testing.T) {
	t.Parallel()
	stub := &recordingStubMetrics{}
	a, err := NewInstagramStubAdapter(stub, "tenant-v391")
	if err != nil {
		t.Fatalf("NewInstagramStubAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	_ = a.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{TenantID: "tenant-v391", ExternalOrderID: "x"})
	_ = a.CreateListing(context.Background(), channelport.ListingRequest{TenantID: "tenant-v391", ProductID: "sku-1", PriceAUDCents: 100})
	_ = a.Publish(context.Background(), eventbus.ProductEnrichedPayload{Version: 1, TenantID: "tenant-v391", ProductID: "sku-1", EnglishTitle: "x", PriceCents: 100, Currency: "AUD"})
	if got := stub.calls(); got != 3 {
		t.Fatalf("metrics calls=%d want=3", got)
	}
	if !stub.observedOp("update_order_status") || !stub.observedOp("create_listing") || !stub.observedOp("publish") {
		t.Fatalf("missing op observation: %+v", stub.list())
	}
}
