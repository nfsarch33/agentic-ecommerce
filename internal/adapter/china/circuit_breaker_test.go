package china

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_OpensAfter5Failures(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 5,
		CooldownDuration: 30 * time.Second,
		Now:              func() time.Time { return now },
	})

	errAPI := errors.New("api failure")
	for i := 0; i < 5; i++ {
		_ = cb.Do(context.Background(), func(_ context.Context) error { return errAPI })
	}

	if cb.State() != StateOpen {
		t.Fatalf("state=%q want=open after 5 failures", cb.State())
	}
	if err := cb.Do(context.Background(), func(_ context.Context) error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got: %v", err)
	}
}

func TestCircuitBreaker_RecoverAfterCooldown(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 5,
		CooldownDuration: 30 * time.Second,
		Now:              func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
	})

	errAPI := errors.New("api failure")
	for i := 0; i < 5; i++ {
		_ = cb.Do(context.Background(), func(_ context.Context) error { return errAPI })
	}
	if cb.State() != StateOpen {
		t.Fatalf("expected open, got %s", cb.State())
	}

	mu.Lock()
	now = now.Add(31 * time.Second)
	mu.Unlock()

	if cb.State() != StateHalfOpen {
		t.Fatalf("expected half_open after cooldown, got %s", cb.State())
	}
	err := cb.Do(context.Background(), func(_ context.Context) error { return nil })
	if err != nil {
		t.Fatalf("expected success in half-open, got: %v", err)
	}
	if cb.State() != StateClosed {
		t.Fatalf("expected closed after success, got %s", cb.State())
	}
}

func TestCircuitBreaker_ClosedOnSuccess(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 5})

	errAPI := errors.New("api failure")
	for i := 0; i < 3; i++ {
		_ = cb.Do(context.Background(), func(_ context.Context) error { return errAPI })
	}
	if cb.ConsecutiveFailures() != 3 {
		t.Fatalf("failures=%d want=3", cb.ConsecutiveFailures())
	}
	_ = cb.Do(context.Background(), func(_ context.Context) error { return nil })
	if cb.ConsecutiveFailures() != 0 {
		t.Fatalf("failures=%d want=0 after success", cb.ConsecutiveFailures())
	}
	if cb.State() != StateClosed {
		t.Fatalf("state=%q want=closed", cb.State())
	}
}

func TestCircuitBreaker_ContextCancelled(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cb.Do(ctx, func(_ context.Context) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}
