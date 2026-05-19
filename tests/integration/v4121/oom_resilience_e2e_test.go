//go:build v4121_smoke

package v4121

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/lifecycle"
	"github.com/nfsarch33/helixon-ec/internal/memwatch"
	"github.com/nfsarch33/helixon-ec/internal/resilience"
	"github.com/nfsarch33/helixon-ec/internal/workerpool"
)

func testLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestMemoryPressureSimulation(t *testing.T) {
	t.Parallel()
	heapVal := new(atomic.Uint64)
	heapVal.Store(870 << 20) // 870 MiB > 80% of 1 GiB => Critical

	ap := workerpool.NewAdaptivePool(testLogger(), workerpool.AdaptiveConfig{
		PoolConfig:       workerpool.Config{Name: "e2e-pressure", MinWorkers: 2, MaxWorkers: 16, QueueDepth: 8},
		HeapCeiling:      1 << 30,
		ShrinkThreshold:  0.7,
		GrowThreshold:    0.4,
		SampleInterval:   50 * time.Millisecond,
		HysteresisWindow: 80 * time.Millisecond,
		SampleHeapFunc:   func() uint64 { return heapVal.Load() },
	})
	defer ap.Close(context.Background())

	bp := memwatch.NewBackpressure(testLogger(), memwatch.BackpressureConfig{
		HeapCeiling: 1 << 30,
		SampleFunc:  func() uint64 { return heapVal.Load() },
	})

	time.Sleep(250 * time.Millisecond)

	stats := ap.Stats()
	if stats.Workers >= 16 {
		t.Fatalf("adaptive pool should have shrunk, got workers=%d", stats.Workers)
	}

	handler := bp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 under pressure, got %d", rec.Code)
	}
}

func TestRecoveryAfterPressureDrop(t *testing.T) {
	t.Parallel()
	heapVal := new(atomic.Uint64)
	heapVal.Store(850 << 20) // Critical

	ap := workerpool.NewAdaptivePool(testLogger(), workerpool.AdaptiveConfig{
		PoolConfig:       workerpool.Config{Name: "e2e-recovery", MinWorkers: 2, MaxWorkers: 16, QueueDepth: 8},
		HeapCeiling:      1 << 30,
		ShrinkThreshold:  0.7,
		GrowThreshold:    0.4,
		SampleInterval:   50 * time.Millisecond,
		HysteresisWindow: 80 * time.Millisecond,
		SampleHeapFunc:   func() uint64 { return heapVal.Load() },
	})
	defer ap.Close(context.Background())

	bp := memwatch.NewBackpressure(testLogger(), memwatch.BackpressureConfig{
		HeapCeiling: 1 << 30,
		SampleFunc:  func() uint64 { return heapVal.Load() },
	})

	time.Sleep(250 * time.Millisecond)
	shrunk := ap.Stats().Workers

	heapVal.Store(300 << 20) // drop to safe
	time.Sleep(250 * time.Millisecond)

	if ap.Stats().Workers <= shrunk {
		t.Fatalf("pool should have grown back: shrunk=%d, now=%d", shrunk, ap.Stats().Workers)
	}

	handler := bp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after recovery, got %d", rec.Code)
	}
}

func TestCircuitBreakerCascade(t *testing.T) {
	t.Parallel()
	reg := resilience.NewRegistry(testLogger())
	names := []string{"stripe", "alipay", "wechat"}
	errFail := errors.New("external service down")

	for _, name := range names {
		cb := reg.Get(name, resilience.CBConfig{
			FailureThreshold: 3,
			SuccessThreshold: 2,
			CooldownDuration: time.Minute,
		})
		for i := 0; i < 3; i++ {
			_ = cb.Do(context.Background(), func(_ context.Context) error { return errFail })
		}
	}

	health := reg.Health()
	if health.Open != 3 {
		t.Fatalf("expected all 3 breakers open, got %d open", health.Open)
	}

	for _, name := range names {
		cb := reg.Get(name, resilience.CBConfig{})
		err := cb.Do(context.Background(), func(_ context.Context) error { return nil })
		if !errors.Is(err, resilience.ErrCircuitOpen) {
			t.Fatalf("breaker %s should reject calls, got: %v", name, err)
		}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Degraded", "true")
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/products", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("degraded response should still serve, got %d", rec.Code)
	}
}

func TestGracefulDrainUnderLoad(t *testing.T) {
	t.Parallel()
	completed := new(atomic.Int32)
	pool := workerpool.New(testLogger(), workerpool.Config{
		Name: "e2e-drain", MinWorkers: 4, MaxWorkers: 4, QueueDepth: 16,
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		_ = pool.Submit(context.Background(), func(ctx context.Context) error {
			defer wg.Done()
			time.Sleep(20 * time.Millisecond)
			completed.Add(1)
			return nil
		})
	}

	phases := []lifecycle.ShutdownPhase{
		{Name: "drain_pool", Duration: 2 * time.Second, Fn: func(ctx context.Context) error {
			return pool.Close(ctx)
		}},
		{Name: "flush_metrics", Duration: 500 * time.Millisecond, Fn: func(ctx context.Context) error {
			return nil // simulate flush
		}},
	}
	es := lifecycle.NewEnhancedShutdown(testLogger(), phases, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := es.Execute(ctx); err != nil {
		t.Fatalf("graceful drain failed: %v", err)
	}
	wg.Wait()
	if completed.Load() != 10 {
		t.Fatalf("completed=%d, want 10 (all inflight)", completed.Load())
	}
}

func TestAutoTuneCeiling(t *testing.T) {
	t.Parallel()
	eightGiB := uint64(8) << 30
	at := memwatch.NewAutoTuner(testLogger(), memwatch.AutoTuneConfig{
		DetectMemFunc: func() uint64 { return eightGiB },
	})
	result := at.Tune()
	expectedHeap := uint64(float64(eightGiB) * 0.7)
	tolerance := uint64(100 << 20)
	if result.HeapCeiling < expectedHeap-tolerance || result.HeapCeiling > expectedHeap+tolerance {
		t.Fatalf("HeapCeiling=%d, want ~%d (70%% of 8GiB)", result.HeapCeiling, expectedHeap)
	}
	if result.GoroutineCeiling <= 0 {
		t.Fatalf("GoroutineCeiling=%d, want >0", result.GoroutineCeiling)
	}
}
