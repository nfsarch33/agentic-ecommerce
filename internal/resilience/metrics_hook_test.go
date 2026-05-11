package resilience

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recordingBreakerMetrics struct {
	mu       sync.Mutex
	open     map[string]int
	halfOpen map[string]int
}

func (r *recordingBreakerMetrics) IncOpen(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.open == nil {
		r.open = map[string]int{}
	}
	r.open[name]++
}

func (r *recordingBreakerMetrics) IncHalfOpen(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.halfOpen == nil {
		r.halfOpen = map[string]int{}
	}
	r.halfOpen[name]++
}

func (r *recordingBreakerMetrics) snapshot() (map[string]int, map[string]int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o := make(map[string]int, len(r.open))
	for k, v := range r.open {
		o[k] = v
	}
	h := make(map[string]int, len(r.halfOpen))
	for k, v := range r.halfOpen {
		h[k] = v
	}
	return o, h
}

func TestCircuitBreaker_EmitsOpenAndHalfOpenMetrics(t *testing.T) {
	t.Parallel()
	current := time.Now().UTC()
	m := &recordingBreakerMetrics{}
	cb := NewCircuitBreaker(nil, CBConfig{
		Name:             "stripe",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		CooldownDuration: 10 * time.Millisecond,
		NowFunc:          func() time.Time { return current },
		Metrics:          m,
	})
	failing := func(_ context.Context) error { return errors.New("upstream 503") }
	for i := 0; i < 2; i++ {
		_ = cb.Do(context.Background(), failing)
	}
	open, halfOpen := m.snapshot()
	if open["stripe"] < 1 {
		t.Fatalf("open[stripe]=%d want >= 1", open["stripe"])
	}
	if halfOpen["stripe"] != 0 {
		t.Fatalf("halfOpen[stripe]=%d want 0", halfOpen["stripe"])
	}
	current = current.Add(20 * time.Millisecond)
	// Trigger the resolveState path so the breaker transitions to half-open.
	_ = cb.Do(context.Background(), func(_ context.Context) error { return nil })
	_, halfOpen = m.snapshot()
	if halfOpen["stripe"] < 1 {
		t.Fatalf("halfOpen[stripe]=%d want >= 1", halfOpen["stripe"])
	}
}
