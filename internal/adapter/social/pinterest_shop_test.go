// File scope: v3.9.1 EC-4-4 Pinterest stub adapter RED tests.
//
// Mirrors the Instagram stub tests; both adapters share the same
// behaviour contract today (Name + UpdateOrderStatus + CreateListing
// returning ErrChannelNotImplemented within 10ms).
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

func TestPinterestStub_NameReturnsPinterest(t *testing.T) {
	t.Parallel()
	a, err := NewPinterestStubAdapter(nil, "tenant-v391")
	if err != nil {
		t.Fatalf("NewPinterestStubAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	if got := a.Name(); got != PinterestChannelName {
		t.Fatalf("Name=%q want=%q", got, PinterestChannelName)
	}
	if got := a.ChannelName(); got != PinterestChannelName {
		t.Fatalf("ChannelName=%q want=%q", got, PinterestChannelName)
	}
}

func TestPinterestStub_UpdateOrderStatusReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	a, err := NewPinterestStubAdapter(nil, "tenant-v391")
	if err != nil {
		t.Fatalf("NewPinterestStubAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	start := time.Now()
	err = a.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID:        "tenant-v391",
		ExternalOrderID: "pin-order-1",
	})
	dur := time.Since(start)
	if !errors.Is(err, ErrChannelNotImplemented) {
		t.Fatalf("UpdateOrderStatus err=%v want ErrChannelNotImplemented", err)
	}
	if dur > 10*time.Millisecond {
		t.Fatalf("stub UpdateOrderStatus took %s; want <10ms", dur)
	}
}

func TestPinterestStub_CreateListingReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	a, err := NewPinterestStubAdapter(nil, "tenant-v391")
	if err != nil {
		t.Fatalf("NewPinterestStubAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	start := time.Now()
	err = a.CreateListing(context.Background(), channelport.ListingRequest{
		TenantID:      "tenant-v391",
		ProductID:     "sku-1",
		Channel:       PinterestChannelName,
		Title:         "Sample Pinterest pin",
		PriceAUDCents: 5500,
	})
	dur := time.Since(start)
	if !errors.Is(err, ErrChannelNotImplemented) {
		t.Fatalf("CreateListing err=%v want ErrChannelNotImplemented", err)
	}
	if dur > 10*time.Millisecond {
		t.Fatalf("stub CreateListing took %s; want <10ms", dur)
	}
}

func TestPinterestStub_PublishReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	a, err := NewPinterestStubAdapter(nil, "tenant-v391")
	if err != nil {
		t.Fatalf("NewPinterestStubAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	payload := eventbus.ProductEnrichedPayload{
		Version:      eventbus.ProductEnrichedPayloadVersion,
		TenantID:     "tenant-v391",
		ProductID:    "sku-1",
		EnglishTitle: "Sample pin",
		PriceCents:   5500,
		Currency:     "AUD",
	}
	if err := a.Publish(context.Background(), payload); !errors.Is(err, ErrChannelNotImplemented) {
		t.Fatalf("Publish err=%v want ErrChannelNotImplemented", err)
	}
}

func TestPinterestStub_RequiresTenantID(t *testing.T) {
	t.Parallel()
	if _, err := NewPinterestStubAdapter(nil, ""); err == nil {
		t.Fatal("expected error for empty tenant_id")
	}
}
