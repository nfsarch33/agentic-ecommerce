package shipping_test

import (
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/domain/shipping"
)

type mockNotifier struct {
	mu     sync.Mutex
	calls  []string
}

func (m *mockNotifier) Notify(orderID string, _ shipping.TrackingEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, orderID)
	return nil
}

func TestTracking_AddEvent(t *testing.T) {
	t.Parallel()
	ts := shipping.NewTrackingStore(nil)
	err := ts.UpdateTracking("ORD-1", shipping.TrackingEvent{
		Status: shipping.StatusInTransit, Location: "Sydney", Timestamp: time.Now(), CarrierCode: "AP",
	})
	if err != nil {
		t.Fatalf("UpdateTracking: %v", err)
	}
}

func TestTracking_GetHistoryOrdering(t *testing.T) {
	t.Parallel()
	ts := shipping.NewTrackingStore(nil)
	t1 := time.Now()
	t2 := t1.Add(time.Hour)
	ts.UpdateTracking("ORD-2", shipping.TrackingEvent{Status: shipping.StatusPickedUp, Timestamp: t1, CarrierCode: "AP"})
	ts.UpdateTracking("ORD-2", shipping.TrackingEvent{Status: shipping.StatusInTransit, Timestamp: t2, CarrierCode: "AP"})
	hist, err := ts.GetHistory("ORD-2")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 events, got %d", len(hist))
	}
	if hist[0].Status != shipping.StatusPickedUp {
		t.Fatalf("expected first event to be picked_up")
	}
}

func TestTracking_DuplicateEventIdempotent(t *testing.T) {
	t.Parallel()
	ts := shipping.NewTrackingStore(nil)
	ev := shipping.TrackingEvent{Status: shipping.StatusInTransit, Timestamp: time.Now(), CarrierCode: "AP"}
	ts.UpdateTracking("ORD-3", ev)
	ts.UpdateTracking("ORD-3", ev)
	hist, _ := ts.GetHistory("ORD-3")
	if len(hist) != 1 {
		t.Fatalf("expected 1 event after dedup, got %d", len(hist))
	}
}

func TestTracking_WebhookOnDelivery(t *testing.T) {
	t.Parallel()
	n := &mockNotifier{}
	ts := shipping.NewTrackingStore(n)
	ts.UpdateTracking("ORD-4", shipping.TrackingEvent{Status: shipping.StatusDelivered, Timestamp: time.Now(), CarrierCode: "AP"})
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 1 || n.calls[0] != "ORD-4" {
		t.Fatalf("expected 1 delivery notification, got %v", n.calls)
	}
}

func TestTracking_WebhookSkipInTransit(t *testing.T) {
	t.Parallel()
	n := &mockNotifier{}
	ts := shipping.NewTrackingStore(n)
	ts.UpdateTracking("ORD-5", shipping.TrackingEvent{Status: shipping.StatusInTransit, Timestamp: time.Now(), CarrierCode: "AP"})
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.calls) != 0 {
		t.Fatalf("expected 0 notifications for in-transit, got %d", len(n.calls))
	}
}

func TestTracking_InvalidOrder(t *testing.T) {
	t.Parallel()
	ts := shipping.NewTrackingStore(nil)
	_, err := ts.GetHistory("NOEXIST")
	if err == nil {
		t.Fatal("expected error for unknown order")
	}
}
