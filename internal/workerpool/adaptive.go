package workerpool

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// AdaptiveConfig controls adaptive pool sizing behaviour.
type AdaptiveConfig struct {
	PoolConfig       Config
	HeapCeiling      uint64        // bytes; pool shrinks when HeapInuse exceeds ShrinkThreshold * ceiling
	ShrinkThreshold  float64       // fraction (0..1); default 0.7
	GrowThreshold    float64       // fraction (0..1); default 0.4
	SampleInterval   time.Duration // how often to sample RSS; default 10s
	HysteresisWindow time.Duration // minimum gap between resizes; default 30s
	Enabled          *bool         // nil or true = enabled; explicit false = disabled
	SampleHeapFunc   func() uint64 // injectable for testing; defaults to runtime.MemStats
	OnResize         func(oldSize, newSize int)
	Metrics          AdaptiveMetrics
}

// AdaptiveMetrics receives bounded resize metrics from AdaptivePool.
type AdaptiveMetrics interface {
	SetWorkerpoolSize(pool string, value int)
	IncWorkerpoolResize(pool, direction string)
}

// AdaptivePool wraps Pool with periodic RSS-based sizing adjustments.
type AdaptivePool struct {
	pool   *Pool
	cfg    AdaptiveConfig
	logger *slog.Logger

	mu           sync.Mutex
	currentSize  int
	lastResizeAt time.Time
	stopCh       chan struct{}
	doneCh       chan struct{}

	resizeEvents atomic.Int64
}

// NewAdaptivePool creates and starts an AdaptivePool.
func NewAdaptivePool(logger *slog.Logger, cfg AdaptiveConfig) *AdaptivePool {
	cfg = resolveAdaptiveDefaults(cfg)
	pool := New(logger, cfg.PoolConfig)

	ap := &AdaptivePool{
		pool:        pool,
		cfg:         cfg,
		logger:      logger,
		currentSize: cfg.PoolConfig.MaxWorkers,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
	ap.emitSize(cfg.PoolConfig.MaxWorkers)
	if ap.isEnabled() {
		go ap.samplerLoop()
	} else {
		close(ap.doneCh)
	}
	return ap
}

func (ap *AdaptivePool) isEnabled() bool {
	return ap.cfg.Enabled == nil || *ap.cfg.Enabled
}

// Submit delegates to the underlying Pool.
func (ap *AdaptivePool) Submit(ctx context.Context, task Task) error {
	return ap.pool.Submit(ctx, task)
}

// Stats returns the current pool stats, with Workers reflecting the
// adaptive current size rather than the original max.
func (ap *AdaptivePool) Stats() Stats {
	s := ap.pool.Stats()
	ap.mu.Lock()
	s.Workers = ap.currentSize
	ap.mu.Unlock()
	return s
}

// ResizeEvents returns the total number of resize operations.
func (ap *AdaptivePool) ResizeEvents() int64 { return ap.resizeEvents.Load() }

// Close stops the sampler loop and drains the pool.
func (ap *AdaptivePool) Close(ctx context.Context) error {
	select {
	case <-ap.stopCh:
	default:
		close(ap.stopCh)
	}
	<-ap.doneCh
	return ap.pool.Close(ctx)
}

func (ap *AdaptivePool) samplerLoop() {
	defer close(ap.doneCh)
	ticker := time.NewTicker(ap.cfg.SampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ap.stopCh:
			return
		case <-ticker.C:
			ap.evaluate()
		}
	}
}

func (ap *AdaptivePool) evaluate() {
	heapInUse := ap.sampleRSS()
	newSize := ap.calculateNewSize(heapInUse)

	ap.mu.Lock()
	if newSize == ap.currentSize {
		ap.mu.Unlock()
		return
	}
	if time.Since(ap.lastResizeAt) < ap.cfg.HysteresisWindow {
		ap.mu.Unlock()
		return
	}
	oldSize := ap.currentSize
	ap.currentSize = newSize
	ap.lastResizeAt = time.Now()
	ap.mu.Unlock()

	ap.applyResize(oldSize, newSize)
}

func (ap *AdaptivePool) sampleRSS() uint64 {
	if ap.cfg.SampleHeapFunc != nil {
		return ap.cfg.SampleHeapFunc()
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapInuse
}

func (ap *AdaptivePool) calculateNewSize(heapInUse uint64) int {
	ap.mu.Lock()
	current := ap.currentSize
	ap.mu.Unlock()

	ratio := float64(heapInUse) / float64(ap.cfg.HeapCeiling)
	target := current

	if ratio > ap.cfg.ShrinkThreshold {
		target = current - current/4 // shrink by 25%
	} else if ratio < ap.cfg.GrowThreshold {
		target = current + current/4 // grow by 25%
	}

	return clampInt(target, ap.cfg.PoolConfig.MinWorkers, ap.cfg.PoolConfig.MaxWorkers)
}

func (ap *AdaptivePool) applyResize(oldSize, newSize int) {
	direction := "grow"
	if newSize < oldSize {
		direction = "shrink"
	}
	ap.resizeEvents.Add(1)
	ap.emitResize(newSize, direction)
	ap.logger.Info("workerpool.adaptive_resize",
		"pool", ap.cfg.PoolConfig.Name,
		"direction", direction,
		"old_size", oldSize,
		"new_size", newSize,
	)
	if ap.cfg.OnResize != nil {
		ap.cfg.OnResize(oldSize, newSize)
	}
}

func (ap *AdaptivePool) emitSize(size int) {
	if ap.cfg.Metrics == nil {
		return
	}
	ap.cfg.Metrics.SetWorkerpoolSize(ap.cfg.PoolConfig.Name, size)
}

func (ap *AdaptivePool) emitResize(size int, direction string) {
	if ap.cfg.Metrics == nil {
		return
	}
	ap.cfg.Metrics.SetWorkerpoolSize(ap.cfg.PoolConfig.Name, size)
	ap.cfg.Metrics.IncWorkerpoolResize(ap.cfg.PoolConfig.Name, direction)
}

func resolveAdaptiveDefaults(cfg AdaptiveConfig) AdaptiveConfig {
	if cfg.HeapCeiling == 0 {
		cfg.HeapCeiling = 4 << 30 // 4 GiB
	}
	if cfg.ShrinkThreshold <= 0 || cfg.ShrinkThreshold >= 1 {
		cfg.ShrinkThreshold = 0.7
	}
	if cfg.GrowThreshold <= 0 || cfg.GrowThreshold >= 1 {
		cfg.GrowThreshold = 0.4
	}
	if cfg.SampleInterval <= 0 {
		cfg.SampleInterval = 10 * time.Second
	}
	if cfg.HysteresisWindow <= 0 {
		cfg.HysteresisWindow = 30 * time.Second
	}
	return cfg
}
