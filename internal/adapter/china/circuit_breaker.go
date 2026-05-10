// File scope: v4.6.0 -- Circuit breaker for China API calls.
//
// Simple 3-state circuit breaker (closed -> open -> half-open).
// Open after consecutiveFailureThreshold failures (default 5),
// half-open after cooldownDuration (default 30s), back to closed
// on first success in half-open state.
package china

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Circuit breaker states.
const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half_open"
)

var (
	ErrCircuitOpen = errors.New("china: circuit breaker open")
)

// CircuitBreakerConfig configures the breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int
	CooldownDuration time.Duration
	Now              func() time.Time
}

// CircuitBreaker wraps China API calls with open/half-open/closed
// state transitions.
type CircuitBreaker struct {
	mu               sync.Mutex
	state            string
	consecutiveFails int
	failureThreshold int
	cooldownDuration time.Duration
	lastFailureAt    time.Time
	now              func() time.Time
}

// NewCircuitBreaker constructs a breaker with sensible defaults.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.CooldownDuration <= 0 {
		cfg.CooldownDuration = 30 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: cfg.FailureThreshold,
		cooldownDuration: cfg.CooldownDuration,
		now:              cfg.Now,
	}
}

// State returns the current breaker state. Thread-safe.
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.checkState()
}

// Do executes fn through the breaker. Returns ErrCircuitOpen when
// the breaker is open and the cooldown has not elapsed.
func (cb *CircuitBreaker) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cb.mu.Lock()
	state := cb.checkState()
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

// ConsecutiveFailures returns the current failure count.
func (cb *CircuitBreaker) ConsecutiveFailures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.consecutiveFails
}

func (cb *CircuitBreaker) checkState() string {
	if cb.state == StateOpen {
		if cb.now().Sub(cb.lastFailureAt) >= cb.cooldownDuration {
			cb.state = StateHalfOpen
		}
	}
	return cb.state
}

func (cb *CircuitBreaker) recordResult(err error) {
	if err == nil {
		cb.consecutiveFails = 0
		cb.state = StateClosed
		return
	}
	cb.consecutiveFails++
	cb.lastFailureAt = cb.now()
	cb.transitionState()
}

func (cb *CircuitBreaker) transitionState() {
	if cb.consecutiveFails >= cb.failureThreshold {
		cb.state = StateOpen
	}
}
