package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recordingCloser captures invocation order and optionally returns an error.
type recordingCloser struct {
	name      string
	delay     time.Duration
	closeErr  error
	called    atomic.Int32
	closedAt  atomic.Int64
	closedSeq *atomic.Int64
	mu        sync.Mutex
	order     int
}

func (r *recordingCloser) Close(ctx context.Context) error {
	r.called.Add(1)
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if r.closedSeq != nil {
		r.mu.Lock()
		r.order = int(r.closedSeq.Add(1))
		r.mu.Unlock()
	}
	r.closedAt.Store(time.Now().UnixNano())
	return r.closeErr
}

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestManagerRunCompletesWhenWorkReturnsNil(t *testing.T) {
	t.Parallel()
	m := New(newSilentLogger(), 5*time.Second)
	c := &recordingCloser{name: "db"}
	m.Register("db", c)

	err := m.Run(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if c.called.Load() != 1 {
		t.Fatalf("Closer called %d times, want 1", c.called.Load())
	}
}

func TestManagerRunPropagatesWorkError(t *testing.T) {
	t.Parallel()
	m := New(newSilentLogger(), 5*time.Second)
	want := errors.New("boom")
	err := m.Run(context.Background(), func(ctx context.Context) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
}

func TestManagerRunCancellationCallsClosersInReverseOrder(t *testing.T) {
	t.Parallel()
	var seq atomic.Int64
	c1 := &recordingCloser{name: "c1", closedSeq: &seq}
	c2 := &recordingCloser{name: "c2", closedSeq: &seq}
	c3 := &recordingCloser{name: "c3", closedSeq: &seq}

	m := New(newSilentLogger(), 5*time.Second)
	m.Register("c1", c1)
	m.Register("c2", c2)
	m.Register("c3", c3)

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		_ = m.Run(ctx, func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		})
		close(doneCh)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	// c3 registered last must close first (reverse order).
	if c3.order >= c2.order || c2.order >= c1.order {
		t.Fatalf("close order wrong: c1=%d c2=%d c3=%d (want c3 < c2 < c1)", c1.order, c2.order, c3.order)
	}
}

func TestManagerCloserErrorAggregation(t *testing.T) {
	t.Parallel()
	c1 := &recordingCloser{name: "c1", closeErr: errors.New("c1-fail")}
	c2 := &recordingCloser{name: "c2", closeErr: errors.New("c2-fail")}

	m := New(newSilentLogger(), 5*time.Second)
	m.Register("c1", c1)
	m.Register("c2", c2)

	err := m.Run(context.Background(), func(ctx context.Context) error { return nil })
	if err == nil {
		t.Fatal("expected aggregated close error")
	}
	if !errors.Is(err, c1.closeErr) || !errors.Is(err, c2.closeErr) {
		t.Fatalf("missing wrapped errors: %v", err)
	}
}

func TestManagerDoubleShutdownIdempotent(t *testing.T) {
	t.Parallel()
	c := &recordingCloser{name: "c"}
	m := New(newSilentLogger(), 5*time.Second)
	m.Register("c", c)

	if err := m.Run(context.Background(), func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("first Run error: %v", err)
	}
	// Second invocation should be a no-op (manager already drained).
	if err := m.Run(context.Background(), func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("second Run error: %v", err)
	}
	if got := c.called.Load(); got != 1 {
		t.Fatalf("Closer called %d times, want 1", got)
	}
}

func TestManagerDrainTimeoutExceeded(t *testing.T) {
	t.Parallel()
	slow := &recordingCloser{name: "slow", delay: 200 * time.Millisecond}
	m := New(newSilentLogger(), 50*time.Millisecond)
	m.Register("slow", slow)

	err := m.Run(context.Background(), func(ctx context.Context) error { return nil })
	if err == nil {
		t.Fatal("expected drain timeout error")
	}
	if !errors.Is(err, ErrShutdownTimeout) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want ErrShutdownTimeout or DeadlineExceeded", err)
	}
}

func TestManagerCloserFnAdapter(t *testing.T) {
	t.Parallel()
	called := atomic.Int32{}
	m := New(newSilentLogger(), 5*time.Second)
	m.Register("fn", CloserFunc(func(ctx context.Context) error {
		called.Add(1)
		return nil
	}))
	if err := m.Run(context.Background(), func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if called.Load() != 1 {
		t.Fatalf("CloserFunc called %d, want 1", called.Load())
	}
}

func TestManagerNilCloserIgnored(t *testing.T) {
	t.Parallel()
	m := New(newSilentLogger(), 5*time.Second)
	m.Register("nil", nil) // must not panic
	if err := m.Run(context.Background(), func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestManagerSignalDuringHandlerCallsClosers(t *testing.T) {
	t.Parallel()
	c := &recordingCloser{name: "c"}
	m := New(newSilentLogger(), 5*time.Second)
	m.Register("c", c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- m.Run(ctx, func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		})
	}()
	cancel()
	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if c.called.Load() != 1 {
		t.Fatalf("Closer called %d, want 1", c.called.Load())
	}
}

func TestManagerWorkErrorTriggersDrain(t *testing.T) {
	t.Parallel()
	c := &recordingCloser{name: "c"}
	m := New(newSilentLogger(), 5*time.Second)
	m.Register("c", c)

	want := fmt.Errorf("worker exploded")
	err := m.Run(context.Background(), func(ctx context.Context) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want wraps %v", err, want)
	}
	if c.called.Load() != 1 {
		t.Fatalf("Closer called %d, want 1", c.called.Load())
	}
}
