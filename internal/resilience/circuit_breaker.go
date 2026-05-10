// Package resilience provides reusable resilience primitives (circuit
// breaker, registry) for all external calls. Generalized from the
// v4.6.0 china-specific breaker at internal/adapter/china/.
package resilience

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"
)

const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half_open"
)

var ErrCircuitOpen = errors.New("resilience: circuit breaker open")

// CBConfig configures a single CircuitBreaker.
type CBConfig struct {
	Name             string
	FailureThreshold int
	SuccessThreshold int
	CooldownDuration time.Duration
	NowFunc          func() time.Time
}

// CircuitBreaker wraps external calls with open/half-open/closed
// state transitions. Thread-safe.
type CircuitBreaker struct {
	logger *slog.Logger
	name   string

	mu               sync.Mutex
	state            string
	consecutiveFails int
	halfOpenSuccs    int
	failureThreshold int
	successThreshold int
	cooldownDuration time.Duration
	lastFailureAt    time.Time
	nowFunc          func() time.Time
}

// NewCircuitBreaker creates a breaker with sensible defaults.
func NewCircuitBreaker(logger *slog.Logger, cfg CBConfig) *CircuitBreaker {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.CooldownDuration <= 0 {
		cfg.CooldownDuration = 30 * time.Second
	}
	if cfg.NowFunc == nil {
		cfg.NowFunc = func() time.Time { return time.Now().UTC() }
	}
	return &CircuitBreaker{
		logger:           logger,
		name:             cfg.Name,
		state:            StateClosed,
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		cooldownDuration: cfg.CooldownDuration,
		nowFunc:          cfg.NowFunc,
	}
}

// State returns the current breaker state. Thread-safe.
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.resolveState()
}

// Name returns the breaker's identifying name.
func (cb *CircuitBreaker) Name() string { return cb.name }

// Do executes fn through the breaker.
func (cb *CircuitBreaker) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cb.mu.Lock()
	state := cb.resolveState()
	if state == StateOpen {
		cb.mu.Unlock()
		return ErrCircuitOpen
	}
	cb.mu.Unlock()

	err := fn(ctx)

	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.recordResult(err)
	return err
}

func (cb *CircuitBreaker) resolveState() string {
	if cb.state == StateOpen {
		if cb.nowFunc().Sub(cb.lastFailureAt) >= cb.cooldownDuration {
			cb.state = StateHalfOpen
			cb.halfOpenSuccs = 0
		}
	}
	return cb.state
}

func (cb *CircuitBreaker) recordResult(err error) {
	if err == nil {
		cb.consecutiveFails = 0
		if cb.state == StateHalfOpen {
			cb.halfOpenSuccs++
			if cb.halfOpenSuccs >= cb.successThreshold {
				cb.transition(StateHalfOpen, StateClosed)
			}
			return
		}
		return
	}
	cb.consecutiveFails++
	cb.lastFailureAt = cb.nowFunc()
	if cb.state == StateHalfOpen {
		cb.transition(StateHalfOpen, StateOpen)
		return
	}
	if cb.consecutiveFails >= cb.failureThreshold {
		cb.transition(StateClosed, StateOpen)
	}
}

func (cb *CircuitBreaker) transition(from, to string) {
	cb.state = to
	cb.logger.Info("resilience.circuit_breaker_transition",
		"name", cb.name, "from", from, "to", to,
	)
}

// RegistryHealth is an aggregated view of all breakers.
type RegistryHealth struct {
	Total    int
	Closed   int
	Open     int
	HalfOpen int
}

// Registry is a central registry of named circuit breakers.
type Registry struct {
	logger   *slog.Logger
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
}

// NewRegistry creates an empty Registry.
func NewRegistry(logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{logger: logger, breakers: make(map[string]*CircuitBreaker)}
}

// Get returns the named breaker, creating it if it doesn't exist.
func (r *Registry) Get(name string, cfg CBConfig) *CircuitBreaker {
	r.mu.RLock()
	if cb, ok := r.breakers[name]; ok {
		r.mu.RUnlock()
		return cb
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if cb, ok := r.breakers[name]; ok {
		return cb
	}
	cfg.Name = name
	cb := NewCircuitBreaker(r.logger, cfg)
	r.breakers[name] = cb
	return cb
}

// Health returns an aggregated health view of all breakers.
func (r *Registry) Health() RegistryHealth {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var h RegistryHealth
	h.Total = len(r.breakers)
	for _, cb := range r.breakers {
		switch cb.State() {
		case StateClosed:
			h.Closed++
		case StateOpen:
			h.Open++
		case StateHalfOpen:
			h.HalfOpen++
		}
	}
	return h
}

// Names returns a sorted list of all registered breaker names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.breakers))
	for n := range r.breakers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
