// Package lifecycle wires the runtime contract every binary needs:
// signal-driven cancellation, graceful drain of in-flight work, and
// reverse-order Closer invocation with a bounded shutdown deadline.
//
// The Manager is the single resource-aware orchestrator referenced by
// v2.10.0 Story 1. Every cmd/* binary builds its dependency graph,
// registers each component as a Closer, then calls Run with the
// long-lived workload (HTTP server, Temporal worker, scheduler tick,
// etc.).
//
// Design notes:
//
//   - Run is single-shot. A second invocation after shutdown returns nil
//     immediately so cmd/* binaries that re-enter the function during
//     test fixtures do not double-close pools.
//   - Close ordering is LIFO so adapters that depend on transport
//     (e.g. HTTP server holds Postgres pool) shut down before their
//     dependencies disappear.
//   - Errors from Closer invocations are aggregated via errors.Join so
//     no failure is silently dropped.
//   - The shutdown deadline is enforced by a context.WithTimeout
//     supplied to each Closer. Closers MUST honour ctx.Done().
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Closer is the minimal contract every Manager-managed component
// implements. The supplied context carries the shutdown deadline.
type Closer interface {
	Close(ctx context.Context) error
}

// CloserFunc adapts a plain function to Closer.
type CloserFunc func(ctx context.Context) error

// Close invokes the underlying function.
func (f CloserFunc) Close(ctx context.Context) error { return f(ctx) }

// ErrShutdownTimeout is returned (joined with the underlying ctx.Err)
// when a Closer fails to honour the shutdown deadline.
var ErrShutdownTimeout = errors.New("lifecycle: shutdown deadline exceeded")

type entry struct {
	name   string
	closer Closer
}

// Manager coordinates startup workload + reverse-order shutdown.
type Manager struct {
	logger          *slog.Logger
	shutdownTimeout time.Duration

	mu       sync.Mutex
	entries  []entry
	shutdown bool
}

// New returns a Manager. shutdownTimeout caps the total drain budget
// applied across all Closers (each receives a context.WithTimeout of
// the remaining budget).
func New(logger *slog.Logger, shutdownTimeout time.Duration) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = 30 * time.Second
	}
	return &Manager{logger: logger, shutdownTimeout: shutdownTimeout}
}

// Register appends a Closer. Nil closers are silently ignored so cmd/*
// callers can register conditionally without nil checks at the call
// site.
func (m *Manager) Register(name string, closer Closer) {
	if closer == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shutdown {
		// Shutdown already happened: close immediately to avoid leaks.
		ctx, cancel := context.WithTimeout(context.Background(), m.shutdownTimeout)
		defer cancel()
		_ = closer.Close(ctx)
		return
	}
	m.entries = append(m.entries, entry{name: name, closer: closer})
}

// Run executes work with a child context that cancels when work
// returns OR when the parent context is cancelled (via signal). After
// work returns the Manager drains all Closers in reverse registration
// order. Errors from work and Closers are joined into a single return
// value.
func (m *Manager) Run(parent context.Context, work func(ctx context.Context) error) error {
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	workCtx, cancelWork := context.WithCancel(parent)
	defer cancelWork()

	workErrCh := make(chan error, 1)
	go func() { workErrCh <- work(workCtx) }()

	var workErr error
	select {
	case workErr = <-workErrCh:
		// work returned by itself.
	case <-parent.Done():
		// signal/parent cancellation: cancel work and wait for it.
		cancelWork()
		select {
		case workErr = <-workErrCh:
		case <-time.After(m.shutdownTimeout):
			workErr = fmt.Errorf("lifecycle: work did not return within %s of cancel: %w", m.shutdownTimeout, ErrShutdownTimeout)
		}
	}

	closeErr := m.shutdownNow()
	return errors.Join(workErr, closeErr)
}

// shutdownNow drains every registered Closer in reverse order with a
// shared deadline. Subsequent invocations are no-ops.
func (m *Manager) shutdownNow() error {
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return nil
	}
	m.shutdown = true
	entries := m.entries
	m.entries = nil
	m.mu.Unlock()

	if len(entries) == 0 {
		return nil
	}

	deadline := time.Now().Add(m.shutdownTimeout)
	var errs []error
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		remaining := time.Until(deadline)
		if remaining <= 0 {
			errs = append(errs, fmt.Errorf("lifecycle: %s skipped: %w", e.name, ErrShutdownTimeout))
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), remaining)
		start := time.Now()
		err := e.closer.Close(ctx)
		cancel()
		dur := time.Since(start)
		if err != nil {
			m.logger.Error("lifecycle.close_failed", "component", e.name, "duration_ms", dur.Milliseconds(), "error", err)
			errs = append(errs, fmt.Errorf("close %s: %w", e.name, err))
			continue
		}
		m.logger.Info("lifecycle.closed", "component", e.name, "duration_ms", dur.Milliseconds())
	}
	return errors.Join(errs...)
}

// Shutdown forces the drain path without invoking Run. Used by tests
// and emergency shutdown paths (memwatch ceiling breach).
func (m *Manager) Shutdown() error { return m.shutdownNow() }
