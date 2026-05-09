// Package memwatch samples runtime memory + goroutine metrics on a
// fixed cadence and triggers callbacks (typically a graceful shutdown
// via lifecycle.Manager) when configured ceilings are breached for a
// dwell window.
//
// v2.10.0 Story 3: detect runaway memory before the OS OOM-killer
// arrives. The Sampler implements lifecycle.Closer so it plugs into
// every binary's drain path.
//
// Sources for the runtime metrics: runtime.MemStats (HeapInuse,
// HeapAlloc, GC pause histogram) and runtime.NumGoroutine. We avoid
// the more granular runtime/metrics package here to keep allocations
// low and the dependency surface minimal -- MemStats is sufficient for
// the OOM-detection use case.
package memwatch

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Sample is one observation emitted by the Sampler.
type Sample struct {
	Binary         string
	RecordedAt     time.Time
	HeapInUseBytes uint64
	HeapAllocBytes uint64
	GoroutineCount int
	NumGC          uint32
	GCPauseLastNs  uint64
}

// Sink consumes Sample emissions. Sinks should not block.
type Sink interface {
	Emit(ctx context.Context, sample Sample)
}

// SinkFunc adapts a function to Sink.
type SinkFunc func(ctx context.Context, sample Sample)

// Emit invokes the underlying function.
func (f SinkFunc) Emit(ctx context.Context, sample Sample) { f(ctx, sample) }

// Config controls Sampler behaviour. Zero-valued fields receive sane
// defaults via NewSampler.
type Config struct {
	BinaryName             string
	SampleInterval         time.Duration
	HeapCeilingBytes       uint64
	HeapCeilingDwell       time.Duration
	GoroutineCeiling       int
	GoroutineDwell         time.Duration
	Sink                   Sink
	HeapAlarmCallback      func()
	GoroutineAlarmCallback func()
	GoroutineDumpDir       string
}

// Sampler periodically reads memory + goroutine stats.
type Sampler struct {
	cfg    Config
	logger *slog.Logger

	mu           sync.Mutex
	closed       bool
	stopCh       chan struct{}
	doneCh       chan struct{}
	latest       Sample
	heapBreachAt time.Time
	gorBreachAt  time.Time
	sampleCount  atomic.Int64
}

// NewSampler returns a Sampler with safe defaults applied.
func NewSampler(logger *slog.Logger, cfg Config) *Sampler {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.BinaryName == "" {
		cfg.BinaryName = "unknown"
	}
	if cfg.SampleInterval <= 0 {
		cfg.SampleInterval = 5 * time.Second
	}
	if cfg.HeapCeilingBytes == 0 {
		cfg.HeapCeilingBytes = 4 << 30 // 4 GiB
	}
	if cfg.HeapCeilingDwell <= 0 {
		cfg.HeapCeilingDwell = 30 * time.Second
	}
	if cfg.GoroutineCeiling == 0 {
		cfg.GoroutineCeiling = 50_000
	}
	if cfg.GoroutineDwell <= 0 {
		cfg.GoroutineDwell = 60 * time.Second
	}
	return &Sampler{
		cfg:    cfg,
		logger: logger,
		stopCh: make(chan struct{}),
	}
}

// Config returns the resolved configuration.
func (s *Sampler) Config() Config { return s.cfg }

// SampleCount returns the number of samples taken so far.
func (s *Sampler) SampleCount() int64 { return s.sampleCount.Load() }

// Latest returns the most recent sample.
func (s *Sampler) Latest() Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latest
}

// Run blocks until ctx is cancelled or Close is called. Implements
// the standard "long-lived background loop" contract.
func (s *Sampler) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	if s.doneCh != nil {
		s.mu.Unlock()
		return nil // already running
	}
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	ticker := time.NewTicker(s.cfg.SampleInterval)
	defer ticker.Stop()
	defer close(s.doneCh)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.stopCh:
			return nil
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// Close signals the Run loop to exit and waits up to ctx for it.
// Implements lifecycle.Closer.
func (s *Sampler) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.stopCh)
	doneCh := s.doneCh
	s.mu.Unlock()
	if doneCh == nil {
		return nil
	}
	select {
	case <-doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Sampler) tick(ctx context.Context) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	gorCount := runtime.NumGoroutine()
	now := time.Now()

	pauseLast := uint64(0)
	if ms.NumGC > 0 {
		idx := (ms.NumGC + 255) % 256
		pauseLast = ms.PauseNs[idx]
	}

	sample := Sample{
		Binary:         s.cfg.BinaryName,
		RecordedAt:     now,
		HeapInUseBytes: ms.HeapInuse,
		HeapAllocBytes: ms.HeapAlloc,
		GoroutineCount: gorCount,
		NumGC:          ms.NumGC,
		GCPauseLastNs:  pauseLast,
	}

	s.mu.Lock()
	s.latest = sample
	s.mu.Unlock()
	s.sampleCount.Add(1)

	if s.cfg.Sink != nil {
		s.cfg.Sink.Emit(ctx, sample)
	}

	s.evaluateHeapCeiling(now, sample)
	s.evaluateGoroutineCeiling(now, sample)
}

func (s *Sampler) evaluateHeapCeiling(now time.Time, sample Sample) {
	if sample.HeapInUseBytes <= s.cfg.HeapCeilingBytes {
		s.mu.Lock()
		s.heapBreachAt = time.Time{}
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	if s.heapBreachAt.IsZero() {
		s.heapBreachAt = now
		s.mu.Unlock()
		s.logger.Warn("memwatch.heap_ceiling_breach",
			"binary", s.cfg.BinaryName,
			"heap_in_use_bytes", sample.HeapInUseBytes,
			"ceiling_bytes", s.cfg.HeapCeilingBytes,
		)
		return
	}
	dwell := now.Sub(s.heapBreachAt)
	s.mu.Unlock()
	if dwell >= s.cfg.HeapCeilingDwell {
		s.logger.Error("memwatch.heap_ceiling_critical",
			"binary", s.cfg.BinaryName,
			"heap_in_use_bytes", sample.HeapInUseBytes,
			"ceiling_bytes", s.cfg.HeapCeilingBytes,
			"dwell_ms", dwell.Milliseconds(),
		)
		if s.cfg.HeapAlarmCallback != nil {
			s.cfg.HeapAlarmCallback()
		}
		// Reset breach so we do not spam callback every tick.
		s.mu.Lock()
		s.heapBreachAt = now
		s.mu.Unlock()
	}
}

func (s *Sampler) evaluateGoroutineCeiling(now time.Time, sample Sample) {
	if sample.GoroutineCount <= s.cfg.GoroutineCeiling {
		s.mu.Lock()
		s.gorBreachAt = time.Time{}
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	if s.gorBreachAt.IsZero() {
		s.gorBreachAt = now
		s.mu.Unlock()
		s.logger.Warn("memwatch.goroutine_ceiling_breach",
			"binary", s.cfg.BinaryName,
			"goroutine_count", sample.GoroutineCount,
			"ceiling", s.cfg.GoroutineCeiling,
		)
		return
	}
	dwell := now.Sub(s.gorBreachAt)
	s.mu.Unlock()
	if dwell >= s.cfg.GoroutineDwell {
		s.logger.Error("memwatch.goroutine_ceiling_critical",
			"binary", s.cfg.BinaryName,
			"goroutine_count", sample.GoroutineCount,
			"ceiling", s.cfg.GoroutineCeiling,
			"dwell_ms", dwell.Milliseconds(),
		)
		if s.cfg.GoroutineAlarmCallback != nil {
			s.cfg.GoroutineAlarmCallback()
		}
		s.mu.Lock()
		s.gorBreachAt = now
		s.mu.Unlock()
	}
}
