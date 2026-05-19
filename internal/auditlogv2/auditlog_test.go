package auditlogv2

import (
	"encoding/json"
	"testing"
	"time"
)

func baseTime() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func TestStore_AppendAndCount(t *testing.T) {
	t.Parallel()

	s := &Store{}
	s.Append(Event{ID: "1", ActorID: "alice", Action: "login", OccurredAt: baseTime()})
	s.Append(Event{ID: "2", ActorID: "bob", Action: "logout", OccurredAt: baseTime().Add(time.Minute)})

	if s.Count() != 2 {
		t.Errorf("Count = %d, want 2", s.Count())
	}
}

func TestStore_QueryFilters(t *testing.T) {
	t.Parallel()

	s := &Store{}
	t0 := baseTime()
	s.Append(Event{ID: "1", ActorID: "alice", Action: "login", OccurredAt: t0})
	s.Append(Event{ID: "2", ActorID: "alice", Action: "delete", OccurredAt: t0.Add(time.Hour)})
	s.Append(Event{ID: "3", ActorID: "bob", Action: "login", OccurredAt: t0.Add(2 * time.Hour)})

	// Filter by actor.
	res := s.Query("alice", "", time.Time{})
	if len(res) != 2 {
		t.Errorf("query by alice = %d, want 2", len(res))
	}

	// Filter by action.
	res = s.Query("", "login", time.Time{})
	if len(res) != 2 {
		t.Errorf("query by login = %d, want 2", len(res))
	}

	// Filter by actor + action.
	res = s.Query("alice", "delete", time.Time{})
	if len(res) != 1 {
		t.Errorf("query alice+delete = %d, want 1", len(res))
	}

	// Filter by since.
	res = s.Query("", "", t0.Add(30*time.Minute))
	if len(res) != 2 {
		t.Errorf("query since +30m = %d, want 2", len(res))
	}
}

func TestEnforcer_RetentionByAge(t *testing.T) {
	t.Parallel()

	s := &Store{}
	now := baseTime().Add(24 * time.Hour)
	s.Append(Event{ID: "old", OccurredAt: baseTime()})             // 24h old
	s.Append(Event{ID: "recent", OccurredAt: now.Add(-time.Hour)}) // 1h old

	e := Enforcer{}
	removed := e.Enforce(s, RetentionPolicy{MaxAge: 12 * time.Hour}, now)

	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if s.Count() != 1 {
		t.Errorf("Count after enforce = %d, want 1", s.Count())
	}
}

func TestEnforcer_RetentionByMaxCount(t *testing.T) {
	t.Parallel()

	s := &Store{}
	t0 := baseTime()
	for i := 0; i < 5; i++ {
		s.Append(Event{ID: string(rune('1' + i)), OccurredAt: t0.Add(time.Duration(i) * time.Minute)})
	}

	e := Enforcer{}
	removed := e.Enforce(s, RetentionPolicy{MaxCount: 3}, time.Now())
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	if s.Count() != 3 {
		t.Errorf("Count after enforce = %d, want 3", s.Count())
	}
}

func TestExporter_ExportJSON(t *testing.T) {
	t.Parallel()

	s := &Store{}
	t0 := baseTime()
	s.Append(Event{ID: "1", ActorID: "alice", Action: "login", OccurredAt: t0})
	s.Append(Event{ID: "2", ActorID: "bob", Action: "logout", OccurredAt: t0.Add(time.Minute)})

	ex := Exporter{}
	data, err := ex.ExportJSON(s, time.Time{})
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	var events []Event
	if err := json.Unmarshal(data, &events); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("exported events = %d, want 2", len(events))
	}
}
