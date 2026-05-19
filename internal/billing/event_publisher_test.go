package billing

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

func TestBusEventPublisherBridgesToBus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := eventbus.NewInMemoryBus()
	pub := NewBusEventPublisher(bus)
	err := pub.PublishBilling(ctx, "subscription.created", "test", time.Now().UTC(), BillingPayload{
		Version: BillingPayloadVersion, TenantID: "tenant-a",
		SubscriptionID: "sub_1", PlanID: "free", State: "trialing",
	})
	if err != nil {
		t.Fatalf("PublishBilling: %v", err)
	}
	delivered := bus.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("delivered len = %d, want 1", len(delivered))
	}
	if delivered[0].Type != eventbus.SubscriptionCreated {
		t.Fatalf("event type = %s, want subscription.created", delivered[0].Type)
	}
	if delivered[0].TenantID != "tenant-a" {
		t.Fatalf("tenant_id = %s", delivered[0].TenantID)
	}
}

func TestBusEventPublisherNilBusNoop(t *testing.T) {
	t.Parallel()
	var pub *BusEventPublisher
	if err := pub.PublishBilling(context.Background(), "subscription.created", "", time.Time{}, BillingPayload{Version: 1, TenantID: "t", SubscriptionID: "s"}); err != nil {
		t.Fatalf("nil receiver should be no-op, got %v", err)
	}
}

func TestNoopPublisher(t *testing.T) {
	t.Parallel()
	if err := (NoopPublisher{}).PublishBilling(context.Background(), "x", "y", time.Time{}, BillingPayload{}); err != nil {
		t.Fatalf("NoopPublisher: %v", err)
	}
}

func TestBusEventPublisherInvalidPayload(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	pub := NewBusEventPublisher(bus)
	err := pub.PublishBilling(context.Background(), "subscription.created", "test", time.Now().UTC(), BillingPayload{Version: 1})
	if err == nil {
		t.Fatalf("expected error for missing tenant_id")
	}
}
