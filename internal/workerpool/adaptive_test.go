package workerpool

import (
	"context"
	"io"
	"log/slog"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestAdaptivePool_ShrinkUnderPressure(t *testing.T) {
	t.Parallel()
	heapInUse := new(atomic.Uint64)
	heapInUse.Store(800 << 20) // 800 MiB => >70% of 1 GiB ceiling

	ap := NewAdaptivePool(testLogger(), AdaptiveConfig{
		PoolConfig:       Config{Name: "shrink", MinWorkers: 2, MaxWorkers: 16, QueueDepth: 8},
		HeapCeiling:      1 << 30, // 1 GiB
		ShrinkThreshold:  0.7,
		GrowThreshold:    0.4,
		SampleInterval:   50 * time.Millisecond,
		HysteresisWindow: 100 * time.Millisecond,
		SampleHeapFunc:   func() uint64 { return heapInUse.Load() },
	})
	defer ap.Close(context.Background())

	time.Sleep(200 * time.Millisecond)
	stats := ap.Stats()
	if stats.Workers >= 16 {
		t.Fatalf("expected shrink from 16, got workers=%d", stats.Workers)
	}
	if stats.Workers < 2 {
		t.Fatalf("workers=%d below min floor 2", stats.Workers)
	}
}

func TestAdaptivePool_GrowWhenIdle(t *testing.T) {
	t.Parallel()
	heapInUse := new(atomic.Uint64)
	heapInUse.Store(800 << 20) // start high

	ap := NewAdaptivePool(testLogger(), AdaptiveConfig{
		PoolConfig:       Config{Name: "grow", MinWorkers: 2, MaxWorkers: 16, QueueDepth: 8},
		HeapCeiling:      1 << 30,
		ShrinkThreshold:  0.7,
		GrowThreshold:    0.4,
		SampleInterval:   50 * time.Millisecond,
		HysteresisWindow: 80 * time.Millisecond,
		SampleHeapFunc:   func() uint64 { return heapInUse.Load() },
	})
	defer ap.Close(context.Background())

	time.Sleep(200 * time.Millisecond)
	shrunk := ap.Stats().Workers

	heapInUse.Store(300 << 20) // 300 MiB => <40% of 1 GiB
	time.Sleep(250 * time.Millisecond)

	grown := ap.Stats().Workers
	if grown <= shrunk {
		t.Fatalf("expected grow after pressure drop: shrunk=%d, grown=%d", shrunk, grown)
	}
}

func TestAdaptivePool_HysteresisPreventsOscillation(t *testing.T) {
	t.Parallel()
	heapInUse := new(atomic.Uint64)
	heapInUse.Store(750 << 20)

	resizeCount := new(atomic.Int64)
	ap := NewAdaptivePool(testLogger(), AdaptiveConfig{
		PoolConfig:       Config{Name: "hysteresis", MinWorkers: 2, MaxWorkers: 16, QueueDepth: 8},
		HeapCeiling:      1 << 30,
		ShrinkThreshold:  0.7,
		GrowThreshold:    0.4,
		SampleInterval:   20 * time.Millisecond,
		HysteresisWindow: 500 * time.Millisecond,
		SampleHeapFunc:   func() uint64 { return heapInUse.Load() },
		OnResize:         func(_, _ int) { resizeCount.Add(1) },
	})
	defer ap.Close(context.Background())

	time.Sleep(300 * time.Millisecond)
	count := resizeCount.Load()
	if count > 1 {
		t.Fatalf("hysteresis failed: resize_count=%d, want <=1 within window", count)
	}
}

func TestAdaptivePool_MinFloorRespected(t *testing.T) {
	t.Parallel()
	ap := NewAdaptivePool(testLogger(), AdaptiveConfig{
		PoolConfig:       Config{Name: "floor", MinWorkers: 4, MaxWorkers: 16, QueueDepth: 8},
		HeapCeiling:      1 << 30,
		ShrinkThreshold:  0.7,
		GrowThreshold:    0.4,
		SampleInterval:   50 * time.Millisecond,
		HysteresisWindow: 80 * time.Millisecond,
		SampleHeapFunc:   func() uint64 { return 950 << 20 }, // 950 MiB of 1 GiB
	})
	defer ap.Close(context.Background())

	time.Sleep(300 * time.Millisecond)
	stats := ap.Stats()
	if stats.Workers < 4 {
		t.Fatalf("workers=%d below min floor 4", stats.Workers)
	}
}

func TestAdaptivePool_MaxCeilingRespected(t *testing.T) {
	t.Parallel()
	ap := NewAdaptivePool(testLogger(), AdaptiveConfig{
		PoolConfig:       Config{Name: "ceiling", MinWorkers: 2, MaxWorkers: 8, QueueDepth: 8},
		HeapCeiling:      1 << 30,
		ShrinkThreshold:  0.7,
		GrowThreshold:    0.4,
		SampleInterval:   50 * time.Millisecond,
		HysteresisWindow: 80 * time.Millisecond,
		SampleHeapFunc:   func() uint64 { return 100 << 20 }, // very low
	})
	defer ap.Close(context.Background())

	time.Sleep(300 * time.Millisecond)
	stats := ap.Stats()
	if stats.Workers > 8 {
		t.Fatalf("workers=%d above max ceiling 8", stats.Workers)
	}
}

func TestAdaptivePool_DisabledModePassthrough(t *testing.T) {
	t.Parallel()
	ap := NewAdaptivePool(testLogger(), AdaptiveConfig{
		PoolConfig:       Config{Name: "disabled", MinWorkers: 2, MaxWorkers: 8, QueueDepth: 8},
		HeapCeiling:      1 << 30,
		ShrinkThreshold:  0.7,
		GrowThreshold:    0.4,
		Enabled:          boolPtr(false),
		SampleInterval:   50 * time.Millisecond,
		HysteresisWindow: 80 * time.Millisecond,
		SampleHeapFunc:   func() uint64 { return 900 << 20 },
	})
	defer ap.Close(context.Background())

	time.Sleep(200 * time.Millisecond)
	stats := ap.Stats()
	if stats.Workers != 8 {
		t.Fatalf("disabled mode: workers=%d, want 8 (unchanged)", stats.Workers)
	}

	var counter atomic.Int32
	for i := 0; i < 4; i++ {
		_ = ap.Submit(context.Background(), func(_ context.Context) error {
			counter.Add(1)
			return nil
		})
	}
	runtime.Gosched()
	time.Sleep(100 * time.Millisecond)
	if counter.Load() < 4 {
		t.Fatalf("disabled mode tasks incomplete: %d/4", counter.Load())
	}
}

func boolPtr(b bool) *bool { return &b }
