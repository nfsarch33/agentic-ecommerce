package eventbus

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestInMemoryBus_PublishSubscribeRoundTrip(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	var received []Event
	var mu sync.Mutex

	err := bus.Subscribe(context.Background(), []EventType{ProductCreated}, "test-group", func(_ context.Context, e Event) error {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	event := Event{
		ID:        "evt-1",
		Type:      ProductCreated,
		TenantID:  "tenant-a",
		Payload:   map[string]any{"sku": "TEST-001"},
		Timestamp: time.Now().UTC(),
		Source:    "test",
	}
	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].ID != "evt-1" {
		t.Errorf("event id = %q, want %q", received[0].ID, "evt-1")
	}
	if received[0].TenantID != "tenant-a" {
		t.Errorf("tenant_id = %q, want %q", received[0].TenantID, "tenant-a")
	}
}

func TestInMemoryBus_MultipleConsumerGroups(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	var groupA, groupB int
	var mu sync.Mutex

	_ = bus.Subscribe(context.Background(), []EventType{OrderPlaced}, "group-a", func(_ context.Context, _ Event) error {
		mu.Lock()
		groupA++
		mu.Unlock()
		return nil
	})
	_ = bus.Subscribe(context.Background(), []EventType{OrderPlaced}, "group-b", func(_ context.Context, _ Event) error {
		mu.Lock()
		groupB++
		mu.Unlock()
		return nil
	})

	event := Event{ID: "evt-2", Type: OrderPlaced, TenantID: "default", Timestamp: time.Now().UTC()}
	_ = bus.Publish(context.Background(), event)

	mu.Lock()
	defer mu.Unlock()
	if groupA != 1 {
		t.Errorf("group-a received %d events, want 1", groupA)
	}
	if groupB != 1 {
		t.Errorf("group-b received %d events, want 1", groupB)
	}
}

func TestInMemoryBus_SameGroupDeduplication(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	var count int
	var mu sync.Mutex

	handler := func(_ context.Context, _ Event) error {
		mu.Lock()
		count++
		mu.Unlock()
		return nil
	}
	_ = bus.Subscribe(context.Background(), []EventType{ProductUpdated}, "same-group", handler)
	_ = bus.Subscribe(context.Background(), []EventType{ProductUpdated}, "same-group", handler)

	event := Event{ID: "evt-3", Type: ProductUpdated, TenantID: "default", Timestamp: time.Now().UTC()}
	_ = bus.Publish(context.Background(), event)

	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Errorf("same group received %d events, want 1 (dedup)", count)
	}
}

func TestInMemoryBus_EventTypeFiltering(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	var received int
	var mu sync.Mutex

	_ = bus.Subscribe(context.Background(), []EventType{ProductCreated}, "filter-group", func(_ context.Context, _ Event) error {
		mu.Lock()
		received++
		mu.Unlock()
		return nil
	})

	_ = bus.Publish(context.Background(), Event{ID: "e1", Type: OrderPlaced, TenantID: "default", Timestamp: time.Now().UTC()})
	_ = bus.Publish(context.Background(), Event{ID: "e2", Type: ProductCreated, TenantID: "default", Timestamp: time.Now().UTC()})

	mu.Lock()
	defer mu.Unlock()
	if received != 1 {
		t.Errorf("received %d events, want 1 (only ProductCreated)", received)
	}
}

func TestInMemoryBus_CloseRejectsPublish(t *testing.T) {
	bus := NewInMemoryBus()
	_ = bus.Close()

	err := bus.Publish(context.Background(), Event{ID: "e1", Type: ProductCreated, TenantID: "default", Timestamp: time.Now().UTC()})
	if err != ErrBusClosed {
		t.Errorf("publish after close = %v, want ErrBusClosed", err)
	}
}

func TestInMemoryBus_CloseRejectsSubscribe(t *testing.T) {
	bus := NewInMemoryBus()
	_ = bus.Close()

	err := bus.Subscribe(context.Background(), []EventType{ProductCreated}, "g", func(_ context.Context, _ Event) error { return nil })
	if err != ErrBusClosed {
		t.Errorf("subscribe after close = %v, want ErrBusClosed", err)
	}
}

func TestInMemoryBus_PingHealthy(t *testing.T) {
	bus := NewInMemoryBus()
	if err := bus.Ping(context.Background()); err != nil {
		t.Errorf("ping healthy bus: %v", err)
	}
	_ = bus.Close()
	if err := bus.Ping(context.Background()); err != ErrBusClosed {
		t.Errorf("ping closed bus = %v, want ErrBusClosed", err)
	}
}

func TestInMemoryBus_Delivered(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	e1 := Event{ID: "d1", Type: ProductCreated, TenantID: "t1", Timestamp: time.Now().UTC()}
	e2 := Event{ID: "d2", Type: OrderPlaced, TenantID: "t2", Timestamp: time.Now().UTC()}
	_ = bus.Publish(context.Background(), e1)
	_ = bus.Publish(context.Background(), e2)

	delivered := bus.Delivered()
	if len(delivered) != 2 {
		t.Fatalf("delivered = %d, want 2", len(delivered))
	}
	if delivered[0].ID != "d1" || delivered[1].ID != "d2" {
		t.Errorf("delivered IDs = [%s, %s], want [d1, d2]", delivered[0].ID, delivered[1].ID)
	}
}
