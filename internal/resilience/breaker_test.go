package resilience_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/resilience"
)

var errFail = errors.New("failure")

func TestBreaker_ClosedStatePassesThrough(t *testing.T) {
	t.Parallel()
	b := resilience.NewBreaker(resilience.BreakerConfig{FailureThreshold: 3, RecoveryTimeout: time.Second})
	if err := b.Execute(func() error { return nil }); err != nil {
		t.Fatalf("expected nil from closed breaker, got %v", err)
	}
}

func TestBreaker_FailuresTripsToOpen(t *testing.T) {
	t.Parallel()
	b := resilience.NewBreaker(resilience.BreakerConfig{FailureThreshold: 3, RecoveryTimeout: time.Second})
	for i := 0; i < 3; i++ {
		b.Execute(func() error { return errFail })
	}
	if b.State() != "open" {
		t.Fatalf("expected open after 3 failures, got %s", b.State())
	}
}

func TestBreaker_OpenStateRejectsImmediately(t *testing.T) {
	t.Parallel()
	b := resilience.NewBreaker(resilience.BreakerConfig{FailureThreshold: 1, RecoveryTimeout: time.Second})
	b.Execute(func() error { return errFail })
	err := b.Execute(func() error { return nil })
	if err != resilience.ErrBreakerOpen {
		t.Fatalf("expected ErrBreakerOpen, got %v", err)
	}
}

func TestBreaker_HalfOpenAllowsProbe(t *testing.T) {
	t.Parallel()
	b := resilience.NewBreaker(resilience.BreakerConfig{FailureThreshold: 1, RecoveryTimeout: time.Millisecond})
	b.Execute(func() error { return errFail })
	time.Sleep(5 * time.Millisecond)
	// First call after timeout should be allowed (half-open probe)
	err := b.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("expected probe to succeed, got %v", err)
	}
	if b.State() != "closed" {
		t.Fatalf("expected closed after successful probe, got %s", b.State())
	}
}

func TestBreaker_FailedProbeReopens(t *testing.T) {
	t.Parallel()
	b := resilience.NewBreaker(resilience.BreakerConfig{FailureThreshold: 1, RecoveryTimeout: time.Millisecond})
	b.Execute(func() error { return errFail })
	time.Sleep(5 * time.Millisecond)
	b.Execute(func() error { return errFail })
	if b.State() != "open" {
		t.Fatalf("expected open after failed probe, got %s", b.State())
	}
}

func TestBreaker_SuccessfulProbeCloses(t *testing.T) {
	t.Parallel()
	b := resilience.NewBreaker(resilience.BreakerConfig{FailureThreshold: 2, RecoveryTimeout: time.Millisecond})
	b.Execute(func() error { return errFail })
	b.Execute(func() error { return errFail })
	time.Sleep(5 * time.Millisecond)
	b.Execute(func() error { return nil })
	if b.State() != "closed" {
		t.Fatalf("expected closed, got %s", b.State())
	}
}

func TestBreaker_ConcurrentExecutionSafe(t *testing.T) {
	t.Parallel()
	b := resilience.NewBreaker(resilience.BreakerConfig{FailureThreshold: 100, RecoveryTimeout: time.Second})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Execute(func() error { return nil })
		}()
	}
	wg.Wait()
}
