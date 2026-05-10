package resilience

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func cbLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestCircuitBreaker_ClosedToOpenOnFailures(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	cb := NewCircuitBreaker(cbLogger(), CBConfig{
		Name:             "stripe",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		CooldownDuration: 30 * time.Second,
		NowFunc:          func() time.Time { return now },
	})

	errRemote := errors.New("stripe timeout")
	for i := 0; i < 5; i++ {
		_ = cb.Do(context.Background(), func(_ context.Context) error { return errRemote })
	}
	if cb.State() != StateOpen {
		t.Fatalf("state=%s, want Open after 5 failures", cb.State())
	}
	if err := cb.Do(context.Background(), func(_ context.Context) error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got: %v", err)
	}
}

func TestCircuitBreaker_OpenToHalfOpenAfterCooldown(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	cb := NewCircuitBreaker(cbLogger(), CBConfig{
		Name:             "alipay",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		CooldownDuration: 30 * time.Second,
		NowFunc:          func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
	})

	errRemote := errors.New("timeout")
	for i := 0; i < 5; i++ {
		_ = cb.Do(context.Background(), func(_ context.Context) error { return errRemote })
	}
	if cb.State() != StateOpen {
		t.Fatalf("state=%s, want Open", cb.State())
	}

	mu.Lock()
	now = now.Add(31 * time.Second)
	mu.Unlock()

	if cb.State() != StateHalfOpen {
		t.Fatalf("state=%s, want HalfOpen after cooldown", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToClosedOnSuccess(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	cb := NewCircuitBreaker(cbLogger(), CBConfig{
		Name:             "wechat",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		CooldownDuration: 30 * time.Second,
		NowFunc:          func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
	})

	errRemote := errors.New("fail")
	for i := 0; i < 5; i++ {
		_ = cb.Do(context.Background(), func(_ context.Context) error { return errRemote })
	}

	mu.Lock()
	now = now.Add(31 * time.Second)
	mu.Unlock()

	for i := 0; i < 2; i++ {
		_ = cb.Do(context.Background(), func(_ context.Context) error { return nil })
	}
	if cb.State() != StateClosed {
		t.Fatalf("state=%s, want Closed after 2 successes in HalfOpen", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToOpenOnFailure(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	cb := NewCircuitBreaker(cbLogger(), CBConfig{
		Name:             "paypal",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		CooldownDuration: 30 * time.Second,
		NowFunc:          func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
	})

	errRemote := errors.New("fail")
	for i := 0; i < 5; i++ {
		_ = cb.Do(context.Background(), func(_ context.Context) error { return errRemote })
	}

	mu.Lock()
	now = now.Add(31 * time.Second)
	mu.Unlock()

	_ = cb.Do(context.Background(), func(_ context.Context) error { return errRemote })
	if cb.State() != StateOpen {
		t.Fatalf("state=%s, want Open after failure in HalfOpen", cb.State())
	}
}

func TestCircuitBreakerRegistry_AggregatedHealth(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(cbLogger())

	stripe := reg.Get("stripe", CBConfig{FailureThreshold: 3, SuccessThreshold: 2, CooldownDuration: time.Minute})
	alipay := reg.Get("alipay", CBConfig{FailureThreshold: 3, SuccessThreshold: 2, CooldownDuration: time.Minute})
	_ = reg.Get("wechat", CBConfig{FailureThreshold: 3, SuccessThreshold: 2, CooldownDuration: time.Minute})

	errFail := errors.New("fail")
	for i := 0; i < 3; i++ {
		_ = stripe.Do(context.Background(), func(_ context.Context) error { return errFail })
	}
	for i := 0; i < 3; i++ {
		_ = alipay.Do(context.Background(), func(_ context.Context) error { return errFail })
	}

	health := reg.Health()
	if health.Total != 3 {
		t.Fatalf("total=%d, want 3", health.Total)
	}
	if health.Open != 2 {
		t.Fatalf("open=%d, want 2", health.Open)
	}
	if health.Closed != 1 {
		t.Fatalf("closed=%d, want 1", health.Closed)
	}

	names := reg.Names()
	if len(names) != 3 {
		t.Fatalf("names=%v, want 3 entries", names)
	}
}
