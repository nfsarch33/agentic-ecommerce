package workerpool

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recordingAdaptiveMetrics struct {
	mu      sync.Mutex
	size    map[string]int
	resizes map[string]map[string]int
}

func newRecordingAdaptiveMetrics() *recordingAdaptiveMetrics {
	return &recordingAdaptiveMetrics{
		size:    make(map[string]int),
		resizes: make(map[string]map[string]int),
	}
}

func (r *recordingAdaptiveMetrics) SetWorkerpoolSize(pool string, value int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.size[pool] = value
}

func (r *recordingAdaptiveMetrics) IncWorkerpoolResize(pool, direction string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resizes[pool] == nil {
		r.resizes[pool] = map[string]int{}
	}
	r.resizes[pool][direction]++
}

func (r *recordingAdaptiveMetrics) snapshot() (map[string]int, map[string]map[string]int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	size := make(map[string]int, len(r.size))
	for k, v := range r.size {
		size[k] = v
	}
	resizes := make(map[string]map[string]int, len(r.resizes))
	for pool, byDirection := range r.resizes {
		resizes[pool] = make(map[string]int, len(byDirection))
		for direction, count := range byDirection {
			resizes[pool][direction] = count
		}
	}
	return size, resizes
}

func TestAdaptivePool_EmitsResizeMetrics(t *testing.T) {
	t.Parallel()

	m := newRecordingAdaptiveMetrics()
	heap := uint64(90)
	ap := NewAdaptivePool(silentLogger(), AdaptiveConfig{
		PoolConfig:       Config{Name: "v8-adaptive", MinWorkers: 2, MaxWorkers: 8, QueueDepth: 8},
		HeapCeiling:      100,
		ShrinkThreshold:  0.7,
		GrowThreshold:    0.4,
		SampleInterval:   time.Hour,
		HysteresisWindow: time.Nanosecond,
		SampleHeapFunc:   func() uint64 { return heap },
		Metrics:          m,
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = ap.Close(ctx)
	})

	ap.evaluate()

	size, resizes := m.snapshot()
	if got := size["v8-adaptive"]; got != 6 {
		t.Fatalf("workerpool size metric = %d, want 6", got)
	}
	if got := resizes["v8-adaptive"]["shrink"]; got != 1 {
		t.Fatalf("workerpool shrink metric = %d, want 1", got)
	}
}
