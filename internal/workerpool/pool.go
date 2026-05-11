// Package workerpool exposes a bounded goroutine pool with explicit
// queue depth, panic isolation, and Closer-compatible drain semantics.
//
// v2.10.0 Story 2: every long-lived background workload SHOULD route
// through a Pool so the runtime cannot accumulate unbounded
// goroutines. Foreground request handling stays on the net/http
// goroutine-per-request model; pools are reserved for fan-out work
// (event consumers, parallel activities, scheduler dispatch).
//
// The implementation is deliberately small (~150 LOC) so cyclomatic
// complexity stays under the v2.10.0 sentrux ceiling.
package workerpool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Errors surfaced by Submit / Close.
var (
	ErrPoolSaturated = errors.New("workerpool: queue saturated")
	ErrPoolClosed    = errors.New("workerpool: closed")
)

// Task is the unit of work executed by a Pool worker.
type Task func(ctx context.Context) error

// Config controls Pool sizing. Zero-values pick safe defaults via
// New so callers can construct minimal configs.
type Config struct {
	Name        string        // metric/log label
	MinWorkers  int           // floor (also fixed worker count today)
	MaxWorkers  int           // ceiling
	QueueDepth  int           // bounded buffer between Submit and worker
	IdleTimeout time.Duration // reserved for future scaling

	// Metrics is the optional v6.2.0 metric sink. nil-safe; the pool
	// emits ec_workerpool_active gauge updates + ec_workerpool_rejected_total
	// counter increments through this hook.
	Metrics PoolMetrics
}

// PoolMetrics is the optional metric sink the v6.2.0 pool calls when
// activity changes. Implemented by the cmd/* composition root using
// the metrics.Registry surface.
type PoolMetrics interface {
	SetActive(pool string, value int)
	IncRejected(pool string, reason string)
}

// Stats exposes pool counters for tests + observability.
type Stats struct {
	Workers         int
	Queued          int
	Submitted       int64
	Completed       int64
	Saturated       int64
	PanicsRecovered int64
}

// Pool is the bounded worker pool.
type Pool struct {
	cfg    Config
	logger *slog.Logger

	tasks chan poolTask
	wg    sync.WaitGroup

	mu     sync.Mutex
	closed bool

	submitted       atomic.Int64
	completed       atomic.Int64
	saturated       atomic.Int64
	panicsRecovered atomic.Int64
}

type poolTask struct {
	ctx context.Context
	fn  Task
}

// New returns a started Pool. Workers are started immediately and live
// for the Pool lifetime; Close drains them.
func New(logger *slog.Logger, cfg Config) *Pool {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Name == "" {
		cfg.Name = "default"
	}
	if cfg.MinWorkers <= 0 {
		cfg.MinWorkers = 1
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = clampInt(runtime.NumCPU(), cfg.MinWorkers, 64)
	}
	if cfg.MaxWorkers < cfg.MinWorkers {
		cfg.MaxWorkers = cfg.MinWorkers
	}
	if cfg.QueueDepth <= 0 {
		cfg.QueueDepth = 16
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
	}

	p := &Pool{
		cfg:    cfg,
		logger: logger,
		tasks:  make(chan poolTask, cfg.QueueDepth),
	}
	for i := 0; i < cfg.MaxWorkers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	p.emitActive()
	return p
}

// emitActive forwards the current active worker count to the
// configured PoolMetrics. nil-safe so tests and callers without
// observability stay decoupled.
func (p *Pool) emitActive() {
	if p.cfg.Metrics == nil {
		return
	}
	p.cfg.Metrics.SetActive(p.cfg.Name, p.activeCount())
}

func (p *Pool) activeCount() int {
	return int(p.submitted.Load() - p.completed.Load())
}

// Config returns a copy of the resolved configuration. Useful for
// tests and admin surfaces that report the running shape.
func (p *Pool) Config() Config { return p.cfg }

// Stats returns a point-in-time snapshot of pool counters.
func (p *Pool) Stats() Stats {
	return Stats{
		Workers:         p.cfg.MaxWorkers,
		Queued:          len(p.tasks),
		Submitted:       p.submitted.Load(),
		Completed:       p.completed.Load(),
		Saturated:       p.saturated.Load(),
		PanicsRecovered: p.panicsRecovered.Load(),
	}
}

// Submit enqueues a task. Returns ErrPoolSaturated if the queue is
// full, ErrPoolClosed if the pool has been closed, or context errors
// if ctx is cancelled before the task is accepted.
func (p *Pool) Submit(ctx context.Context, task Task) error {
	if task == nil {
		return errors.New("workerpool: nil task")
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.recordRejected("closed")
		return ErrPoolClosed
	}
	p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case p.tasks <- poolTask{ctx: ctx, fn: task}:
		p.submitted.Add(1)
		p.emitActive()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		p.saturated.Add(1)
		p.recordRejected("saturated")
		p.logger.Warn("workerpool.saturated", "pool", p.cfg.Name, "queue_depth", p.cfg.QueueDepth)
		return ErrPoolSaturated
	}
}

func (p *Pool) recordRejected(reason string) {
	if p.cfg.Metrics == nil {
		return
	}
	p.cfg.Metrics.IncRejected(p.cfg.Name, reason)
}

// Close marks the pool closed, drains in-flight + queued tasks, and
// blocks until all workers exit or ctx fires. Implements lifecycle.Closer.
func (p *Pool) Close(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.tasks)
	p.mu.Unlock()

	doneCh := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("workerpool[%s] drain: %w", p.cfg.Name, ctx.Err())
	}
}

func (p *Pool) worker(idx int) {
	defer p.wg.Done()
	for task := range p.tasks {
		p.runTask(idx, task)
	}
}

// runTask isolates a single task execution, recovering panics and
// counting completions. Kept separate from worker so tests + the
// recovery path are easier to reason about.
func (p *Pool) runTask(idx int, task poolTask) {
	defer func() {
		if r := recover(); r != nil {
			p.panicsRecovered.Add(1)
			p.logger.Error("workerpool.panic_recovered",
				"pool", p.cfg.Name,
				"worker", idx,
				"panic", fmt.Sprintf("%v", r),
				"stack", string(debug.Stack()),
			)
		}
		p.completed.Add(1)
		p.emitActive()
	}()
	if task.ctx == nil {
		task.ctx = context.Background()
	}
	if err := task.fn(task.ctx); err != nil && !errors.Is(err, context.Canceled) {
		p.logger.Warn("workerpool.task_error",
			"pool", p.cfg.Name,
			"worker", idx,
			"error", err,
		)
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
