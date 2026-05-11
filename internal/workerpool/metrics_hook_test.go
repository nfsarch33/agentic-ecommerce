package workerpool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingPoolMetrics struct {
	mu       sync.Mutex
	active   map[string]int
	rejected map[string]map[string]int
	calls    atomic.Int64
}

func newRecordingPoolMetrics() *recordingPoolMetrics {
	return &recordingPoolMetrics{
		active:   make(map[string]int),
		rejected: make(map[string]map[string]int),
	}
}

func (r *recordingPoolMetrics) SetActive(pool string, value int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[pool] = value
	r.calls.Add(1)
}

func (r *recordingPoolMetrics) IncRejected(pool string, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rejected[pool] == nil {
		r.rejected[pool] = map[string]int{}
	}
	r.rejected[pool][reason]++
	r.calls.Add(1)
}

func (r *recordingPoolMetrics) snapshot() (map[string]int, map[string]map[string]int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := make(map[string]int, len(r.active))
	for k, v := range r.active {
		a[k] = v
	}
	rj := make(map[string]map[string]int, len(r.rejected))
	for k, m := range r.rejected {
		rj[k] = make(map[string]int, len(m))
		for r, c := range m {
			rj[k][r] = c
		}
	}
	return a, rj
}

func TestPool_EmitsMetricsOnSubmitAndComplete(t *testing.T) {
	t.Parallel()
	m := newRecordingPoolMetrics()
	p := New(nil, Config{Name: "v620-test", MinWorkers: 2, MaxWorkers: 2, QueueDepth: 4, Metrics: m})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	wait := make(chan struct{})
	go func() {
		_ = p.Submit(context.Background(), func(_ context.Context) error {
			<-wait
			return nil
		})
	}()
	// Allow the submission to land.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		active, _ := m.snapshot()
		if active["v620-test"] >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	close(wait)
	for time.Now().Before(time.Now().Add(time.Second)) {
		active, _ := m.snapshot()
		if active["v620-test"] == 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if calls := m.calls.Load(); calls < 2 {
		t.Fatalf("metrics calls=%d want >= 2", calls)
	}
}

func TestPool_IncRejectedOnSaturation(t *testing.T) {
	t.Parallel()
	m := newRecordingPoolMetrics()
	p := New(nil, Config{Name: "sat", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1, Metrics: m})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	started := make(chan struct{}, 1)
	hold := make(chan struct{})
	if err := p.Submit(context.Background(), func(_ context.Context) error {
		started <- struct{}{}
		<-hold
		return nil
	}); err != nil {
		t.Fatalf("Submit#1: %v", err)
	}
	// Wait until the worker has actually picked the task off the
	// queue so the queue is empty before the next Submit fills it.
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start task")
	}
	if err := p.Submit(context.Background(), func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("Submit#2: %v", err)
	}
	err := p.Submit(context.Background(), func(_ context.Context) error { return nil })
	if !errors.Is(err, ErrPoolSaturated) {
		t.Fatalf("Submit#3 err=%v want ErrPoolSaturated", err)
	}
	close(hold)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, rj := m.snapshot()
		if rj["sat"]["saturated"] >= 1 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("expected metric IncRejected[sat][saturated] >= 1")
}

func TestPool_IncRejectedOnClosed(t *testing.T) {
	t.Parallel()
	m := newRecordingPoolMetrics()
	p := New(nil, Config{Name: "closed", MinWorkers: 1, MaxWorkers: 1, Metrics: m})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.Submit(context.Background(), func(_ context.Context) error { return nil }); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("Submit after close err=%v want ErrPoolClosed", err)
	}
	_, rj := m.snapshot()
	if rj["closed"]["closed"] != 1 {
		t.Fatalf("rejected[closed][closed] = %d want 1", rj["closed"]["closed"])
	}
}
