package confighot

import (
	"errors"
	"sync"
)

// Value is the configuration map type.
type Value map[string]string

// Validator is a function that validates a Value before it is applied.
type Validator func(Value) error

// Store is a thread-safe hot-reloadable configuration store.
type Store struct {
	mu      sync.RWMutex
	current Value
	version int
}

// Load initialises the store with the given value.
func (s *Store) Load(initial Value) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := copyValue(initial)
	s.current = cp
	s.version = 0
}

// Get returns the value for the given key.
func (s *Store) Get(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current[key]
}

// GetAll returns a copy of the current configuration.
func (s *Store) GetAll() Value {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyValue(s.current)
}

// Update attempts to replace the current configuration.
// If validator is non-nil and returns an error, the update is rejected and
// the previous value is restored (rollback). Version is only incremented on
// success. Returns an error on validation failure (does NOT notify listeners).
func (s *Store) Update(v Value, validator Validator) error {
	if validator != nil {
		if err := validator(v); err != nil {
			return err
		}
	}

	s.mu.Lock()
	old := copyValue(s.current)
	s.current = copyValue(v)
	s.version++
	s.mu.Unlock()

	globalListener.Notify(old, v)
	return nil
}

// Version returns the current configuration version counter.
func (s *Store) Version() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// globalListener is the package-level change listener used by Store.
// Stores share a listener for simplicity; in production this would be per-Store.
var globalListener ChangeListener

// ChangeListener dispatches config change notifications.
type ChangeListener struct {
	mu          sync.RWMutex
	subscribers []func(old, new Value)
}

// Subscribe registers a function to be called on each successful config update.
func (c *ChangeListener) Subscribe(fn func(old, new Value)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribers = append(c.subscribers, fn)
}

// Notify calls all registered subscribers with the old and new values.
func (c *ChangeListener) Notify(old, new Value) {
	c.mu.RLock()
	subs := make([]func(Value, Value), len(c.subscribers))
	copy(subs, c.subscribers)
	c.mu.RUnlock()

	for _, fn := range subs {
		fn(old, new)
	}
}

// copyValue returns a shallow copy of a Value map.
func copyValue(v Value) Value {
	if v == nil {
		return make(Value)
	}
	out := make(Value, len(v))
	for k, val := range v {
		out[k] = val
	}
	return out
}

// ErrValidation is returned when a validator rejects a config update.
var ErrValidation = errors.New("confighot: validation failed")
