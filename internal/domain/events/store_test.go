package events_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/events"
)

func makeEvent(version int) events.Event {
	return events.Event{Type: "order.created", Data: "test", Version: version}
}

func TestEvents_AppendAndReplay(t *testing.T) {
	t.Parallel()
	es := events.NewEventStore()
	if err := es.Append("S1", []events.Event{makeEvent(0), makeEvent(1)}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	evs, err := es.Replay("S1", 0)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evs))
	}
}

func TestEvents_VersionConflictOnConcurrentAppend(t *testing.T) {
	t.Parallel()
	es := events.NewEventStore()
	es.Append("S2", []events.Event{makeEvent(0)})
	// Try to append with wrong version (should be 1, not 0)
	err := es.Append("S2", []events.Event{makeEvent(0)})
	if err == nil {
		t.Fatal("expected version conflict error")
	}
}

func TestEvents_SnapshotCreation(t *testing.T) {
	t.Parallel()
	es := events.NewEventStore()
	es.Append("S3", []events.Event{makeEvent(0), makeEvent(1)})
	if err := es.Snapshot("S3", map[string]any{"state": "v2"}, 2); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
}

func TestEvents_ProjectFromSnapshotPlusTail(t *testing.T) {
	t.Parallel()
	es := events.NewEventStore()
	es.Append("S4", []events.Event{makeEvent(0), makeEvent(1), makeEvent(2)})
	es.Snapshot("S4", "state-at-2", 2)
	result, err := es.ProjectState("S4")
	if err != nil {
		t.Fatalf("ProjectState: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if m["snapshot"] != "state-at-2" {
		t.Fatalf("unexpected snapshot state: %v", m["snapshot"])
	}
}

func TestEvents_ReplayEmptyStream(t *testing.T) {
	t.Parallel()
	es := events.NewEventStore()
	evs, err := es.Replay("NOEXIST", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evs) != 0 {
		t.Fatalf("expected empty, got %d", len(evs))
	}
}

func TestEvents_AppendToNonExistentCreatesStream(t *testing.T) {
	t.Parallel()
	es := events.NewEventStore()
	if err := es.Append("NEW", []events.Event{makeEvent(0)}); err != nil {
		t.Fatalf("Append to new stream: %v", err)
	}
}

func TestEvents_VersionOrdering(t *testing.T) {
	t.Parallel()
	es := events.NewEventStore()
	es.Append("S5", []events.Event{makeEvent(0), makeEvent(1), makeEvent(2)})
	evs, _ := es.Replay("S5", 1)
	if len(evs) != 2 {
		t.Fatalf("expected 2 events from version 1, got %d", len(evs))
	}
	if evs[0].Version != 1 {
		t.Fatalf("expected first version 1, got %d", evs[0].Version)
	}
}
