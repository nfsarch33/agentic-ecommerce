package gracefulshut

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTracker_BeginEnd(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	tok := tr.Begin()
	if tr.InFlight() != 1 {
		t.Errorf("InFlight = %d, want 1", tr.InFlight())
	}
	tr.End(tok)
	if tr.InFlight() != 0 {
		t.Errorf("InFlight after end = %d, want 0", tr.InFlight())
	}
}

func TestTracker_InFlightCount(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	tokens := make([]Token, 5)
	for i := range tokens {
		tokens[i] = tr.Begin()
	}
	if tr.InFlight() != 5 {
		t.Errorf("InFlight = %d, want 5", tr.InFlight())
	}
	for _, tok := range tokens {
		tr.End(tok)
	}
	if tr.InFlight() != 0 {
		t.Errorf("InFlight after all ends = %d, want 0", tr.InFlight())
	}
}

func TestTracker_DrainWaitsForZero(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	tok := tr.Begin()

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done <- tr.Drain(ctx)
	}()

	// End after a short delay.
	time.Sleep(20 * time.Millisecond)
	tr.End(tok)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Drain returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Drain did not return")
	}
}

func TestTracker_DrainContextCancel(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	tok := tr.Begin()
	defer tr.End(tok)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := tr.Drain(ctx)
	if err == nil {
		t.Error("expected context error")
	}
}

func TestTracker_ConcurrentBeginEnd(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok := tr.Begin()
			tr.End(tok)
		}()
	}
	wg.Wait()

	if tr.InFlight() != 0 {
		t.Errorf("InFlight after concurrent use = %d, want 0", tr.InFlight())
	}
}

func TestShutdownManager_Shutdown(t *testing.T) {
	t.Parallel()

	sm := New(2 * time.Second)
	tok := sm.Tracker().Begin()

	go func() {
		time.Sleep(20 * time.Millisecond)
		sm.Tracker().End(tok)
	}()

	if err := sm.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}
