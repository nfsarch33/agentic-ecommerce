package shipping

import (
	"errors"
	"sync"
	"time"
)

var ErrOrderNotFound = errors.New("order not found")

type TrackingStatus string

const (
	StatusInTransit  TrackingStatus = "in_transit"
	StatusDelivered  TrackingStatus = "delivered"
	StatusException  TrackingStatus = "exception"
	StatusPickedUp   TrackingStatus = "picked_up"
)

type TrackingEvent struct {
	Status      TrackingStatus
	Location    string
	Timestamp   time.Time
	CarrierCode string
}

type DeliveryNotifier interface {
	Notify(orderID string, event TrackingEvent) error
}

type TrackingStore struct {
	mu       sync.RWMutex
	history  map[string][]TrackingEvent
	notifier DeliveryNotifier
}

func NewTrackingStore(n DeliveryNotifier) *TrackingStore {
	return &TrackingStore{
		history:  make(map[string][]TrackingEvent),
		notifier: n,
	}
}

func (ts *TrackingStore) UpdateTracking(orderID string, event TrackingEvent) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	events := ts.history[orderID]
	// Idempotency: skip exact duplicate (same status + timestamp)
	for _, e := range events {
		if e.Status == event.Status && e.Timestamp.Equal(event.Timestamp) {
			return nil
		}
	}
	ts.history[orderID] = append(events, event)

	if (event.Status == StatusDelivered || event.Status == StatusException) && ts.notifier != nil {
		return ts.notifier.Notify(orderID, event)
	}
	return nil
}

func (ts *TrackingStore) GetHistory(orderID string) ([]TrackingEvent, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	events, ok := ts.history[orderID]
	if !ok {
		return nil, ErrOrderNotFound
	}
	out := make([]TrackingEvent, len(events))
	copy(out, events)
	return out, nil
}
