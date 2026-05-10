//go:build v461_smoke

// File scope: v4.6.1 QA-2 -- Carrier production smoke tests.
//
// Verifies:
//   - Dual-key webhook verification with both AusPost + DHL
//   - Circuit breaker trips after 5 failures + recovers after 30s
//   - Connection pool stats via Prometheus-compatible metrics
package v461

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/carrier"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/china"
)

func TestCarrier_DualKeyVerification_AusPost(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	kr, err := carrier.NewKeyRotator(carrier.KeyRotationConfig{
		CarrierName: "auspost",
		CurrentKey:  "auspost-key-v2",
		PreviousKey: "auspost-key-v1",
		PreviousSet: now.Add(-1 * time.Hour),
		TTL:         48 * time.Hour,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewKeyRotator: %v", err)
	}

	res, err := kr.Verify(func(s string) bool { return s == "auspost-key-v2" })
	if err != nil || res.MatchedKey != "current" {
		t.Fatalf("current key: %+v %v", res, err)
	}
	res, err = kr.Verify(func(s string) bool { return s == "auspost-key-v1" })
	if err != nil || res.MatchedKey != "previous" {
		t.Fatalf("previous key: %+v %v", res, err)
	}
}

func TestCarrier_DualKeyVerification_DHL(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	kr, err := carrier.NewKeyRotator(carrier.KeyRotationConfig{
		CarrierName: "dhl",
		CurrentKey:  "dhl-key-v2",
		PreviousKey: "dhl-key-v1",
		PreviousSet: now.Add(-1 * time.Hour),
		TTL:         48 * time.Hour,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewKeyRotator: %v", err)
	}

	res, err := kr.Verify(func(s string) bool { return s == "dhl-key-v2" })
	if err != nil || res.MatchedKey != "current" {
		t.Fatalf("current key: %+v %v", res, err)
	}
	res, err = kr.Verify(func(s string) bool { return s == "dhl-key-v1" })
	if err != nil || res.MatchedKey != "previous" {
		t.Fatalf("previous key: %+v %v", res, err)
	}
}

func TestCarrier_CircuitBreakerTripsAndRecovers(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	cb := china.NewCircuitBreaker(china.CircuitBreakerConfig{
		FailureThreshold: 5,
		CooldownDuration: 30 * time.Second,
		Now:              func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
	})

	errAPI := errors.New("api down")
	for i := 0; i < 5; i++ {
		_ = cb.Do(context.Background(), func(_ context.Context) error { return errAPI })
	}
	if cb.State() != china.StateOpen {
		t.Fatalf("expected open, got %s", cb.State())
	}

	if err := cb.Do(context.Background(), func(_ context.Context) error { return nil }); !errors.Is(err, china.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got: %v", err)
	}

	mu.Lock()
	now = now.Add(31 * time.Second)
	mu.Unlock()

	if cb.State() != china.StateHalfOpen {
		t.Fatalf("expected half_open, got %s", cb.State())
	}
	if err := cb.Do(context.Background(), func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("half-open success should work: %v", err)
	}
	if cb.State() != china.StateClosed {
		t.Fatalf("expected closed after success, got %s", cb.State())
	}
}

func TestCarrier_ConnectionPoolStats(t *testing.T) {
	t.Parallel()
	cb := china.NewCircuitBreaker(china.CircuitBreakerConfig{FailureThreshold: 5})
	cfg := china.ProductionScalingConfig{PoolSize: 15, Timeout: 30 * time.Second}
	stats := china.GetConnectionPoolStats(cfg, cb)

	if stats.PoolSize != 15 {
		t.Fatalf("PoolSize=%d want=15", stats.PoolSize)
	}
	if stats.CircuitBreakerSt != china.StateClosed {
		t.Fatalf("state=%s want=closed", stats.CircuitBreakerSt)
	}
}
