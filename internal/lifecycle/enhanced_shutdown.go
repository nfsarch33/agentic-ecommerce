package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ShutdownPhase defines a single phase of the enhanced shutdown sequence.
type ShutdownPhase struct {
	Name     string
	Duration time.Duration
	Fn       func(ctx context.Context) error
}

// EnhancedShutdown orchestrates phased shutdown with per-phase deadlines.
type EnhancedShutdown struct {
	logger  *slog.Logger
	phases  []ShutdownPhase
	timeout time.Duration
}

// NewEnhancedShutdown creates a phased shutdown coordinator.
func NewEnhancedShutdown(logger *slog.Logger, phases []ShutdownPhase, timeout time.Duration) *EnhancedShutdown {
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &EnhancedShutdown{logger: logger, phases: phases, timeout: timeout}
}

// Execute runs all phases sequentially, each with its own deadline
// carved from the overall shutdown budget. Returns joined errors
// from any phase that fails or times out.
func (es *EnhancedShutdown) Execute(ctx context.Context) error {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, es.timeout)
		defer cancel()
		deadline = time.Now().Add(es.timeout)
	}

	var errs []error
	for i, phase := range es.phases {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			errs = append(errs, fmt.Errorf("lifecycle: phase %q skipped: timeout exceeded", phase.Name))
			continue
		}

		phaseBudget := phase.Duration
		if phaseBudget > remaining {
			phaseBudget = remaining
		}

		phaseErr := es.runPhase(ctx, phase, phaseBudget, i)
		if phaseErr != nil {
			errs = append(errs, phaseErr)
		}
	}
	return errors.Join(errs...)
}

func (es *EnhancedShutdown) runPhase(parent context.Context, phase ShutdownPhase, budget time.Duration, idx int) error {
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()

	start := time.Now()
	doneCh := make(chan error, 1)
	go func() { doneCh <- phase.Fn(ctx) }()

	select {
	case err := <-doneCh:
		dur := time.Since(start)
		if err != nil {
			es.logger.Error("lifecycle.enhanced_shutdown.phase_failed",
				"phase", phase.Name, "index", idx, "duration_ms", dur.Milliseconds(), "error", err)
			return fmt.Errorf("phase %q: %w", phase.Name, err)
		}
		es.logger.Info("lifecycle.enhanced_shutdown.phase_complete",
			"phase", phase.Name, "index", idx, "duration_ms", dur.Milliseconds())
		return nil
	case <-ctx.Done():
		dur := time.Since(start)
		es.logger.Error("lifecycle.enhanced_shutdown.phase_timeout",
			"phase", phase.Name, "index", idx, "duration_ms", dur.Milliseconds())
		return fmt.Errorf("phase %q: %w", phase.Name, ErrShutdownTimeout)
	}
}
