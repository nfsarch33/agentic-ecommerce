// Package pkgtrack provides package tracking: event ingestion, status
// normalisation, and webhook notification.
package pkgtrack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Normalised status constants.
const (
	StatusInTransit       = "IN_TRANSIT"
	StatusOutForDelivery  = "OUT_FOR_DELIVERY"
	StatusDelivered       = "DELIVERED"
	StatusException       = "EXCEPTION"
	StatusUnknown         = "UNKNOWN"
)

// Event holds a single tracking event for a shipment.
type Event struct {
	ID               string
	TrackingNo       string
	RawStatus        string
	NormalizedStatus string
	Location         string
	OccurredAt       time.Time
}

// ---------------------------------------------------------------------------
// StatusNormalizer
// ---------------------------------------------------------------------------

// StatusNormalizer maps raw carrier status strings to canonical normalised
// status values.
type StatusNormalizer struct {
	mu      sync.RWMutex
	mapping map[string]string
}

// NewStatusNormalizer returns an initialised StatusNormalizer.
func NewStatusNormalizer() *StatusNormalizer {
	return &StatusNormalizer{
		mapping: make(map[string]string),
	}
}

// Add registers a mapping from a raw status string to a normalised status.
func (s *StatusNormalizer) Add(raw, normalized string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mapping[raw] = normalized
}

// Normalize returns the normalised status for raw; falls back to StatusUnknown
// when no mapping is found.
func (s *StatusNormalizer) Normalize(raw string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.mapping[raw]; ok {
		return v
	}
	return StatusUnknown
}

// ---------------------------------------------------------------------------
// EventStore
// ---------------------------------------------------------------------------

// ErrNotFound is returned when no events exist for the requested tracking number.
var ErrNotFound = errors.New("pkgtrack: no events found for tracking number")

// EventStore is a thread-safe in-memory store for tracking events.
type EventStore struct {
	mu     sync.RWMutex
	events map[string][]Event // keyed by TrackingNo
}

// NewEventStore returns an initialised EventStore.
func NewEventStore() *EventStore {
	return &EventStore{
		events: make(map[string][]Event),
	}
}

// Ingest appends an event to the store, keyed by TrackingNo.
func (s *EventStore) Ingest(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[event.TrackingNo] = append(s.events[event.TrackingNo], event)
}

// Events returns all events for the given tracking number in ingestion order.
func (s *EventStore) Events(trackingNo string) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	evts := s.events[trackingNo]
	if len(evts) == 0 {
		return nil
	}
	// Return a copy so callers cannot mutate internal state.
	out := make([]Event, len(evts))
	copy(out, evts)
	return out
}

// Latest returns the most recently ingested event for trackingNo.
// It returns ErrNotFound when no events exist.
func (s *EventStore) Latest(trackingNo string) (*Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	evts := s.events[trackingNo]
	if len(evts) == 0 {
		return nil, ErrNotFound
	}
	cp := evts[len(evts)-1]
	return &cp, nil
}

// ---------------------------------------------------------------------------
// WebhookNotifier
// ---------------------------------------------------------------------------

// HTTPClient is a minimal interface over *http.Client so tests can inject a
// fake transport.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// WebhookNotifier dispatches tracking events to registered HTTP endpoints.
type WebhookNotifier struct {
	mu    sync.RWMutex
	hooks map[string][]string // trackingNo -> []url
}

// NewWebhookNotifier returns an initialised WebhookNotifier.
func NewWebhookNotifier() *WebhookNotifier {
	return &WebhookNotifier{
		hooks: make(map[string][]string),
	}
}

// RegisterHook registers url to receive notifications for trackingNo.
func (n *WebhookNotifier) RegisterHook(trackingNo, url string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.hooks[trackingNo] = append(n.hooks[trackingNo], url)
}

// webhookPayload is the JSON body sent to registered hooks.
type webhookPayload struct {
	TrackingNo       string    `json:"tracking_no"`
	EventID          string    `json:"event_id"`
	RawStatus        string    `json:"raw_status"`
	NormalizedStatus string    `json:"normalized_status"`
	Location         string    `json:"location"`
	OccurredAt       time.Time `json:"occurred_at"`
}

// Notify sends the event to all hooks registered for trackingNo.
// Errors from individual HTTP calls are silently ignored; each hook is
// attempted independently so a failure for one URL does not block others.
func (n *WebhookNotifier) Notify(ctx context.Context, trackingNo string, event Event, client HTTPClient) {
	n.mu.RLock()
	urls := make([]string, len(n.hooks[trackingNo]))
	copy(urls, n.hooks[trackingNo])
	n.mu.RUnlock()

	payload := webhookPayload{
		TrackingNo:       event.TrackingNo,
		EventID:          event.ID,
		RawStatus:        event.RawStatus,
		NormalizedStatus: event.NormalizedStatus,
		Location:         event.Location,
		OccurredAt:       event.OccurredAt,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		_ = fmt.Sprintf("webhook %s -> %d", u, resp.StatusCode) // consume resp
		resp.Body.Close()
	}
}
