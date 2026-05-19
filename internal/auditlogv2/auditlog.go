package auditlogv2

import (
	"encoding/json"
	"sync"
	"time"
)

// Event is a single audit log entry.
type Event struct {
	ID           string
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	Metadata     map[string]string
	OccurredAt   time.Time
	Retained     bool
}

// Store is a thread-safe in-memory audit log store.
type Store struct {
	mu     sync.RWMutex
	events []Event
}

// Append adds an event to the store.
func (s *Store) Append(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

// Query returns events matching actorID (empty = any), action (empty = any),
// and occurring at or after since (zero = any).
func (s *Store) Query(actorID, action string, since time.Time) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []Event
	for _, e := range s.events {
		if actorID != "" && e.ActorID != actorID {
			continue
		}
		if action != "" && e.Action != action {
			continue
		}
		if !since.IsZero() && e.OccurredAt.Before(since) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Count returns the total number of events.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

// RetentionPolicy defines rules for event retention.
type RetentionPolicy struct {
	MaxAge   time.Duration
	MaxCount int
}

// Enforcer applies retention policies to a store.
type Enforcer struct{}

// Enforce removes events outside the policy and returns the count removed.
// Events are removed if they are older than policy.MaxAge (when MaxAge > 0),
// or if the store exceeds policy.MaxCount (oldest removed first, when MaxCount > 0).
func (Enforcer) Enforce(store *Store, policy RetentionPolicy, now time.Time) int {
	store.mu.Lock()
	defer store.mu.Unlock()

	original := len(store.events)

	if policy.MaxAge > 0 {
		cutoff := now.Add(-policy.MaxAge)
		kept := store.events[:0]
		for _, e := range store.events {
			if !e.OccurredAt.Before(cutoff) {
				kept = append(kept, e)
			}
		}
		store.events = kept
	}

	if policy.MaxCount > 0 && len(store.events) > policy.MaxCount {
		// Remove oldest (front of slice).
		store.events = store.events[len(store.events)-policy.MaxCount:]
	}

	return original - len(store.events)
}

// Exporter exports audit log events.
type Exporter struct{}

// ExportJSON returns a JSON-encoded array of events occurring at or after since.
func (Exporter) ExportJSON(store *Store, since time.Time) ([]byte, error) {
	events := store.Query("", "", since)
	if events == nil {
		events = []Event{}
	}
	return json.Marshal(events)
}
