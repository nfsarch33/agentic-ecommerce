package lifecycle

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func esLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestEnhancedShutdown_AllPhasesComplete(t *testing.T) {
	t.Parallel()
	var executed [5]atomic.Bool
	phases := []ShutdownPhase{
		{Name: "drain_http", Duration: 50 * time.Millisecond, Fn: func(ctx context.Context) error { executed[0].Store(true); return nil }},
		{Name: "drain_temporal", Duration: 50 * time.Millisecond, Fn: func(ctx context.Context) error { executed[1].Store(true); return nil }},
		{Name: "flush_telemetry", Duration: 50 * time.Millisecond, Fn: func(ctx context.Context) error { executed[2].Store(true); return nil }},
		{Name: "close_connections", Duration: 50 * time.Millisecond, Fn: func(ctx context.Context) error { executed[3].Store(true); return nil }},
		{Name: "hard_kill", Duration: 50 * time.Millisecond, Fn: func(ctx context.Context) error { executed[4].Store(true); return nil }},
	}
	es := NewEnhancedShutdown(esLogger(), phases, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := es.Execute(ctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for i := range executed {
		if !executed[i].Load() {
			t.Fatalf("phase %d not executed", i)
		}
	}
}

func TestEnhancedShutdown_InflightDrainsBeforeClose(t *testing.T) {
	t.Parallel()
	drained := new(atomic.Bool)
	phases := []ShutdownPhase{
		{Name: "drain_http", Duration: 200 * time.Millisecond, Fn: func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond)
			drained.Store(true)
			return nil
		}},
		{Name: "close", Duration: 50 * time.Millisecond, Fn: func(ctx context.Context) error {
			if !drained.Load() {
				t.Error("close ran before drain completed")
			}
			return nil
		}},
	}
	es := NewEnhancedShutdown(esLogger(), phases, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := es.Execute(ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !drained.Load() {
		t.Fatal("drain never completed")
	}
}

func TestEnhancedShutdown_TemporalActivityCompletesBeforeKill(t *testing.T) {
	t.Parallel()
	activityDone := new(atomic.Bool)
	phases := []ShutdownPhase{
		{Name: "drain_temporal", Duration: 200 * time.Millisecond, Fn: func(ctx context.Context) error {
			time.Sleep(80 * time.Millisecond) // simulate activity completion
			activityDone.Store(true)
			return nil
		}},
		{Name: "hard_kill", Duration: 50 * time.Millisecond, Fn: func(ctx context.Context) error { return nil }},
	}
	es := NewEnhancedShutdown(esLogger(), phases, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := es.Execute(ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !activityDone.Load() {
		t.Fatal("Temporal activity did not complete before kill")
	}
}

func TestEnhancedShutdown_HardKillAtTimeout(t *testing.T) {
	t.Parallel()
	phases := []ShutdownPhase{
		{Name: "stuck_phase", Duration: 5 * time.Second, Fn: func(ctx context.Context) error {
			<-ctx.Done() // hangs until ctx cancelled
			return ctx.Err()
		}},
	}
	es := NewEnhancedShutdown(esLogger(), phases, 150*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := es.Execute(ctx)
	if err == nil {
		t.Fatal("expected error from stuck phase, got nil")
	}
}

func TestEnhancedShutdown_PhaseTimingRespected(t *testing.T) {
	t.Parallel()
	var timings [3]time.Duration
	phases := []ShutdownPhase{
		{Name: "phase1", Duration: 100 * time.Millisecond, Fn: func(ctx context.Context) error {
			start := time.Now()
			time.Sleep(30 * time.Millisecond)
			timings[0] = time.Since(start)
			return nil
		}},
		{Name: "phase2", Duration: 100 * time.Millisecond, Fn: func(ctx context.Context) error {
			start := time.Now()
			time.Sleep(30 * time.Millisecond)
			timings[1] = time.Since(start)
			return nil
		}},
		{Name: "phase3", Duration: 100 * time.Millisecond, Fn: func(ctx context.Context) error {
			start := time.Now()
			time.Sleep(30 * time.Millisecond)
			timings[2] = time.Since(start)
			return nil
		}},
	}
	es := NewEnhancedShutdown(esLogger(), phases, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := es.Execute(ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for i, d := range timings {
		if d < 25*time.Millisecond || d > 200*time.Millisecond {
			t.Fatalf("phase %d timing=%s out of expected range [25ms, 200ms]", i, d)
		}
	}
}
