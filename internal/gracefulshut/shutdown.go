package gracefulshut

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Token is an opaque handle for a single in-flight request.
type Token int64

// Tracker tracks in-flight requests and supports draining.
type Tracker struct {
	counter  int64       // monotonic token counter
	inFlight int64       // current in-flight count
	mu       sync.Mutex
	cond     *sync.Cond
}

// NewTracker returns an initialised Tracker.
func NewTracker() *Tracker {
	t := &Tracker{}
	t.cond = sync.NewCond(&t.mu)
	return t
}

// Begin registers a new in-flight request and returns its token.
func (t *Tracker) Begin() Token {
	atomic.AddInt64(&t.inFlight, 1)
	id := atomic.AddInt64(&t.counter, 1)
	return Token(id)
}

// End signals that the request associated with the token has completed.
func (t *Tracker) End(_ Token) {
	remaining := atomic.AddInt64(&t.inFlight, -1)
	if remaining <= 0 {
		t.mu.Lock()
		t.cond.Broadcast()
		t.mu.Unlock()
	}
}

// InFlight returns the number of currently in-flight requests.
func (t *Tracker) InFlight() int {
	return int(atomic.LoadInt64(&t.inFlight))
}

// Drain blocks until all in-flight requests complete or ctx expires.
// Returns context.DeadlineExceeded / context.Canceled if ctx expires first.
func (t *Tracker) Drain(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		t.mu.Lock()
		for atomic.LoadInt64(&t.inFlight) > 0 {
			t.cond.Wait()
		}
		t.mu.Unlock()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		// Wake the waiting goroutine so it exits cleanly.
		t.mu.Lock()
		t.cond.Broadcast()
		t.mu.Unlock()
		return ctx.Err()
	}
}

// ErrDrainTimeout is returned when Drain times out.
var ErrDrainTimeout = errors.New("gracefulshut: drain timeout")

// ShutdownManager orchestrates graceful shutdown with a drain timeout.
type ShutdownManager struct {
	drainTimeout time.Duration
	tracker      *Tracker
}

// New returns a ShutdownManager with the given drain timeout.
func New(drainTimeout time.Duration) *ShutdownManager {
	return &ShutdownManager{
		drainTimeout: drainTimeout,
		tracker:      NewTracker(),
	}
}

// Tracker returns the underlying request tracker.
func (m *ShutdownManager) Tracker() *Tracker {
	return m.tracker
}

// Shutdown drains in-flight requests within the configured timeout.
func (m *ShutdownManager) Shutdown(ctx context.Context) error {
	drainCtx, cancel := context.WithTimeout(ctx, m.drainTimeout)
	defer cancel()
	return m.tracker.Drain(drainCtx)
}
