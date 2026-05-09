package memwatch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestSamplerHonorsConfigDefaults(t *testing.T) {
	t.Parallel()
	s := NewSampler(quietLogger(), Config{})
	cfg := s.Config()
	if cfg.SampleInterval == 0 || cfg.HeapCeilingBytes == 0 || cfg.GoroutineCeiling == 0 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestSamplerEmitsSamples(t *testing.T) {
	t.Parallel()
	var received atomic.Int32
	s := NewSampler(quietLogger(), Config{
		BinaryName:     "test",
		SampleInterval: 5 * time.Millisecond,
		Sink: SinkFunc(func(_ context.Context, sample Sample) {
			if sample.Binary != "test" {
				t.Errorf("Binary=%s, want test", sample.Binary)
			}
			received.Add(1)
		}),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := s.Run(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error: %v", err)
	}
	if got := received.Load(); got < 2 {
		t.Fatalf("received=%d samples, want >=2", got)
	}
}

func TestSamplerCloseStopsLoop(t *testing.T) {
	t.Parallel()
	s := NewSampler(quietLogger(), Config{
		BinaryName:     "test",
		SampleInterval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	doneCh := make(chan error, 1)
	go func() { doneCh <- s.Run(ctx) }()

	time.Sleep(20 * time.Millisecond)
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	select {
	case err := <-doneCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Close")
	}
}

func TestHeapCeilingFiresAfterDwell(t *testing.T) {
	t.Parallel()
	var alarms atomic.Int32
	cfg := Config{
		BinaryName:        "test",
		SampleInterval:    5 * time.Millisecond,
		HeapCeilingBytes:  1, // any heap exceeds 1 byte
		HeapCeilingDwell:  10 * time.Millisecond,
		GoroutineCeiling:  100000,
		GoroutineDwell:    time.Second,
		HeapAlarmCallback: func() { alarms.Add(1) },
	}
	s := NewSampler(quietLogger(), cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx)
	if alarms.Load() == 0 {
		t.Fatalf("heap alarm not fired (got %d)", alarms.Load())
	}
}

func TestGoroutineCeilingFiresAfterDwell(t *testing.T) {
	t.Parallel()
	var alarms atomic.Int32
	cfg := Config{
		BinaryName:             "test",
		SampleInterval:         5 * time.Millisecond,
		HeapCeilingBytes:       1 << 60, // never fires
		HeapCeilingDwell:       time.Second,
		GoroutineCeiling:       1, // any goroutine count exceeds 1
		GoroutineDwell:         10 * time.Millisecond,
		GoroutineAlarmCallback: func() { alarms.Add(1) },
	}
	s := NewSampler(quietLogger(), cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx)
	if alarms.Load() == 0 {
		t.Fatalf("goroutine alarm not fired (got %d)", alarms.Load())
	}
}

func TestSamplerStatsPopulated(t *testing.T) {
	t.Parallel()
	s := NewSampler(quietLogger(), Config{BinaryName: "stats", SampleInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx)
	if s.SampleCount() == 0 {
		t.Fatal("SampleCount returned 0")
	}
	last := s.Latest()
	if last.HeapInUseBytes == 0 {
		t.Fatalf("Latest heap is zero: %+v", last)
	}
	if last.GoroutineCount == 0 {
		t.Fatalf("Latest goroutine count is zero: %+v", last)
	}
}

func TestRunAfterCloseIsNoop(t *testing.T) {
	t.Parallel()
	s := NewSampler(quietLogger(), Config{BinaryName: "x", SampleInterval: 5 * time.Millisecond})
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run after Close should be no-op, got %v", err)
	}
}
