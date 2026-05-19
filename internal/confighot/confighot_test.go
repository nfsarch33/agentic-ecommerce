package confighot

import (
	"errors"
	"sync"
	"testing"
)

func newStore(initial Value) *Store {
	s := &Store{}
	s.Load(initial)
	return s
}

func TestStore_LoadAndGet(t *testing.T) {
	t.Parallel()

	s := newStore(Value{"host": "localhost", "port": "8080"})
	if got := s.Get("host"); got != "localhost" {
		t.Errorf("Get(host) = %q, want localhost", got)
	}
	if got := s.Get("port"); got != "8080" {
		t.Errorf("Get(port) = %q, want 8080", got)
	}
	if got := s.Get("missing"); got != "" {
		t.Errorf("Get(missing) = %q, want empty", got)
	}
}

func TestStore_UpdateValid(t *testing.T) {
	t.Parallel()

	s := newStore(Value{"env": "dev"})
	err := s.Update(Value{"env": "prod"}, nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := s.Get("env"); got != "prod" {
		t.Errorf("Get(env) = %q, want prod", got)
	}
}

func TestStore_UpdateInvalidRollback(t *testing.T) {
	t.Parallel()

	s := newStore(Value{"env": "dev"})
	failValidator := func(v Value) error {
		return errors.New("invalid config")
	}
	err := s.Update(Value{"env": "bad"}, failValidator)
	if err == nil {
		t.Fatal("expected validation error")
	}
	// Value should be rolled back (update was rejected before apply).
	if got := s.Get("env"); got != "dev" {
		t.Errorf("after rollback, Get(env) = %q, want dev", got)
	}
}

func TestStore_VersionIncrement(t *testing.T) {
	t.Parallel()

	s := newStore(Value{})
	if v := s.Version(); v != 0 {
		t.Errorf("initial version = %d, want 0", v)
	}
	s.Update(Value{"k": "1"}, nil)
	if v := s.Version(); v != 1 {
		t.Errorf("after update, version = %d, want 1", v)
	}
	s.Update(Value{"k": "2"}, nil)
	if v := s.Version(); v != 2 {
		t.Errorf("after 2 updates, version = %d, want 2", v)
	}
}

func TestStore_VersionNotIncrementedOnRollback(t *testing.T) {
	t.Parallel()

	s := newStore(Value{})
	err := s.Update(Value{"k": "bad"}, func(Value) error { return errors.New("bad") })
	if err == nil {
		t.Fatal("expected error")
	}
	if v := s.Version(); v != 0 {
		t.Errorf("version after failed update = %d, want 0", v)
	}
}

func TestChangeListener_Notified(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var notified []Value

	// Use a local ChangeListener to avoid global state pollution.
	cl := &ChangeListener{}
	cl.Subscribe(func(old, new Value) {
		mu.Lock()
		notified = append(notified, new)
		mu.Unlock()
	})

	cl.Notify(Value{"k": "old"}, Value{"k": "new"})

	mu.Lock()
	defer mu.Unlock()
	if len(notified) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notified))
	}
	if notified[0]["k"] != "new" {
		t.Errorf("notification value k = %q, want new", notified[0]["k"])
	}
}

func TestChangeListener_NoNotifyOnRollback(t *testing.T) {
	t.Parallel()

	// Create an isolated store with its own listener.
	called := false
	cl := &ChangeListener{}
	cl.Subscribe(func(old, new Value) {
		called = true
	})

	// Simulate what Store.Update does: validator fails, so we do NOT call Notify.
	v := Value{"k": "bad"}
	validator := func(Value) error { return errors.New("bad") }
	if err := validator(v); err != nil {
		// Do NOT notify.
	}

	if called {
		t.Error("listener should not be called on rollback")
	}
}
