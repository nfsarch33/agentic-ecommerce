package workerpool

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func silentLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestNewClampsConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  Config
		want Config
	}{
		{
			name: "zero values pick safe defaults",
			cfg:  Config{MinWorkers: 1, MaxWorkers: 1},
			want: Config{Name: "default", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 16, IdleTimeout: 30 * time.Second},
		},
		{
			name: "max below min is clamped to min",
			cfg:  Config{Name: "p", MinWorkers: 4, MaxWorkers: 2, QueueDepth: 8, IdleTimeout: time.Second},
			want: Config{Name: "p", MinWorkers: 4, MaxWorkers: 4, QueueDepth: 8, IdleTimeout: time.Second},
		},
		{
			name: "negative queue depth becomes default",
			cfg:  Config{Name: "p", MinWorkers: 1, MaxWorkers: 4, QueueDepth: -3, IdleTimeout: 0},
			want: Config{Name: "p", MinWorkers: 1, MaxWorkers: 4, QueueDepth: 16, IdleTimeout: 30 * time.Second},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool := New(silentLogger(), tc.cfg)
			defer pool.Close(context.Background())
			got := pool.Config()
			if got.Name != tc.want.Name || got.MinWorkers != tc.want.MinWorkers ||
				got.MaxWorkers != tc.want.MaxWorkers || got.QueueDepth != tc.want.QueueDepth ||
				got.IdleTimeout != tc.want.IdleTimeout {
				t.Fatalf("Config()=%+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSubmitExecutesUnderCap(t *testing.T) {
	t.Parallel()
	cfg := Config{Name: "exec", MinWorkers: 1, MaxWorkers: 4, QueueDepth: 8}
	pool := New(silentLogger(), cfg)
	defer pool.Close(context.Background())

	var counter atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		err := pool.Submit(context.Background(), func(ctx context.Context) error {
			defer wg.Done()
			counter.Add(1)
			return nil
		})
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}
	wg.Wait()
	if counter.Load() != 8 {
		t.Fatalf("counter=%d, want 8", counter.Load())
	}
}

func TestSubmitReturnsSaturationWhenQueueFull(t *testing.T) {
	t.Parallel()
	cfg := Config{Name: "sat", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1}
	pool := New(silentLogger(), cfg)

	block := make(chan struct{})
	// LIFO: pool.Close registered first (runs last), close(block) registered
	// second (runs first), so the holder unblocks before Close drains.
	defer pool.Close(context.Background())
	defer close(block)
	released := make(chan struct{})
	holderActive := make(chan struct{})
	// Hold the single worker.
	if err := pool.Submit(context.Background(), func(ctx context.Context) error {
		close(holderActive)
		<-block
		close(released)
		return nil
	}); err != nil {
		t.Fatalf("Submit holder: %v", err)
	}
	// Wait for the worker to actually consume the holder so the queue is empty.
	<-holderActive
	// Fill the single queue slot.
	if err := pool.Submit(context.Background(), func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("Submit queued: %v", err)
	}
	// Now any submit must saturate.
	err := pool.Submit(context.Background(), func(ctx context.Context) error { return nil })
	if !errors.Is(err, ErrPoolSaturated) {
		t.Fatalf("Submit got %v, want ErrPoolSaturated", err)
	}
}

func TestSubmitContextCancelDuringQueueWait(t *testing.T) {
	t.Parallel()
	cfg := Config{Name: "cancel", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1}
	pool := New(silentLogger(), cfg)
	hold := make(chan struct{})
	// LIFO defers: pool.Close registered first (runs last), close(hold) registered
	// second (runs first), so holder unblocks before drain.
	defer pool.Close(context.Background())
	defer close(hold)
	holderActive := make(chan struct{})
	if err := pool.Submit(context.Background(), func(ctx context.Context) error {
		close(holderActive)
		<-hold
		return nil
	}); err != nil {
		t.Fatalf("Submit holder: %v", err)
	}
	<-holderActive
	if err := pool.Submit(context.Background(), func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("Submit queued: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := pool.Submit(ctx, func(ctx context.Context) error { return nil })
	if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrPoolSaturated) {
		t.Fatalf("Submit got %v, want context.Canceled or ErrPoolSaturated", err)
	}
}

func TestPanicIsolation(t *testing.T) {
	t.Parallel()
	cfg := Config{Name: "panic", MinWorkers: 1, MaxWorkers: 2, QueueDepth: 4}
	pool := New(silentLogger(), cfg)
	defer pool.Close(context.Background())

	done := make(chan struct{})
	if err := pool.Submit(context.Background(), func(ctx context.Context) error {
		panic("boom")
	}); err != nil {
		t.Fatalf("Submit panic-task: %v", err)
	}
	if err := pool.Submit(context.Background(), func(ctx context.Context) error {
		close(done)
		return nil
	}); err != nil {
		t.Fatalf("Submit follow-up: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not survive panic")
	}
	if got := pool.Stats().PanicsRecovered; got < 1 {
		t.Fatalf("PanicsRecovered=%d, want >=1", got)
	}
}

func TestCloseDrainsInFlight(t *testing.T) {
	t.Parallel()
	cfg := Config{Name: "drain", MinWorkers: 2, MaxWorkers: 2, QueueDepth: 8}
	pool := New(silentLogger(), cfg)

	var counter atomic.Int32
	for i := 0; i < 4; i++ {
		err := pool.Submit(context.Background(), func(ctx context.Context) error {
			time.Sleep(20 * time.Millisecond)
			counter.Add(1)
			return nil
		})
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if counter.Load() != 4 {
		t.Fatalf("counter=%d, want 4 (drain incomplete)", counter.Load())
	}
}

func TestSubmitAfterClose(t *testing.T) {
	t.Parallel()
	cfg := Config{Name: "closed", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1}
	pool := New(silentLogger(), cfg)
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := pool.Submit(context.Background(), func(ctx context.Context) error { return nil })
	if !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("Submit after close = %v, want ErrPoolClosed", err)
	}
}

func TestStatsReflectActivity(t *testing.T) {
	t.Parallel()
	cfg := Config{Name: "stats", MinWorkers: 2, MaxWorkers: 2, QueueDepth: 32}
	pool := New(silentLogger(), cfg)
	defer pool.Close(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		if err := pool.Submit(context.Background(), func(ctx context.Context) error {
			defer wg.Done()
			return nil
		}); err != nil {
			wg.Done()
			t.Fatalf("Submit %d: %v", i, err)
		}
	}
	wg.Wait()
	s := pool.Stats()
	if s.Submitted < 6 {
		t.Fatalf("Submitted=%d, want >=6", s.Submitted)
	}
	if s.Completed < 6 {
		t.Fatalf("Completed=%d, want >=6", s.Completed)
	}
	if s.Workers != 2 {
		t.Fatalf("Workers=%d, want 2", s.Workers)
	}
}

func TestClampWorkers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		val, lo, hi, want int
	}{
		{val: 0, lo: 1, hi: 4, want: 1},
		{val: 10, lo: 1, hi: 4, want: 4},
		{val: 3, lo: 1, hi: 4, want: 3},
	}
	for _, tc := range tests {
		if got := clampInt(tc.val, tc.lo, tc.hi); got != tc.want {
			t.Fatalf("clampInt(%d,%d,%d)=%d, want %d", tc.val, tc.lo, tc.hi, got, tc.want)
		}
	}
}
