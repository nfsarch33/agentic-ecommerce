package events

import (
	"errors"
	"sync"
)

var (
	ErrVersionConflict = errors.New("version conflict")
	ErrStreamNotFound  = errors.New("stream not found")
)

type Event struct {
	StreamID string
	Type     string
	Data     any
	Version  int
}

type Snapshot struct {
	State   any
	Version int
}

type EventStore struct {
	mu        sync.RWMutex
	streams   map[string][]Event
	snapshots map[string]Snapshot
}

func NewEventStore() *EventStore {
	return &EventStore{
		streams:   make(map[string][]Event),
		snapshots: make(map[string]Snapshot),
	}
}

func (es *EventStore) Append(streamID string, newEvents []Event) error {
	es.mu.Lock()
	defer es.mu.Unlock()
	existing := es.streams[streamID]
	currentVersion := len(existing)
	for i, e := range newEvents {
		expected := currentVersion + i
		if e.Version != expected {
			return ErrVersionConflict
		}
		e.StreamID = streamID
		existing = append(existing, e)
	}
	es.streams[streamID] = existing
	return nil
}

func (es *EventStore) Replay(streamID string, fromVersion int) ([]Event, error) {
	es.mu.RLock()
	defer es.mu.RUnlock()
	events, ok := es.streams[streamID]
	if !ok {
		return []Event{}, nil
	}
	if fromVersion >= len(events) {
		return []Event{}, nil
	}
	out := make([]Event, len(events)-fromVersion)
	copy(out, events[fromVersion:])
	return out, nil
}

func (es *EventStore) Snapshot(streamID string, state any, version int) error {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.snapshots[streamID] = Snapshot{State: state, Version: version}
	return nil
}

func (es *EventStore) ProjectState(streamID string) (any, error) {
	es.mu.RLock()
	snap, hasSnap := es.snapshots[streamID]
	events := es.streams[streamID]
	es.mu.RUnlock()

	if !hasSnap {
		return events, nil
	}
	// Return snapshot state + tail events
	tail := events[snap.Version:]
	return map[string]any{"snapshot": snap.State, "tail": tail}, nil
}
