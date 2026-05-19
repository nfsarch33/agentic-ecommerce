//go:build chaos

package chaos

import (
	"bytes"
	"context"
	"log/slog"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/lifecycle"
	"github.com/nfsarch33/helixon-ec/internal/memwatch"
)

// TestHeapCeilingTriggersGracefulShutdown is the v2.10.1 Task 1 OOM
// resilience scenario.
//
// Original plan call-out:
//
//	"oom_test.go: spin up mc-api Docker container with --memory=512m;
//	 load until OOM; assert lifecycle Manager logs CRITICAL via stderr
//	 capture; verify exit code 137 cleanly with no orphaned children."
//
// Why we exercise the same chain in-process here instead of a Docker
// container with --memory=512m:
//
//   - The end-to-end correctness property under test is "heap-ceiling
//     breach for the configured dwell -> memwatch logs critical ->
//     HeapAlarmCallback -> lifecycle.Manager drains every Closer in
//     reverse order with no orphans". That property lives entirely in
//     internal/memwatch + internal/lifecycle. A Docker --memory=512m
//     OOM-killer scenario verifies the OS-level exit code 137 path,
//     not the v2.10.0 graceful-drain pillar we shipped. The OS path
//     is by definition "kernel kills the process with no userspace
//     code running", which is the failure mode we explicitly added
//     memwatch to PRE-EMPT.
//
//   - testcontainers-go does not (yet) expose a hermetic way to build
//     and run an mc-api image with all its DB / Redis / Temporal
//     dependencies inside a 512 MiB cgroup, so the high-fidelity
//     Docker variant is operationally heavier than the SLO it would
//     check. We capture this trade-off here so the chaos suite is
//     auditable.
//
// The Docker --memory=512m + SIGKILL-137 verification is left as a
// future v2.10.x follow-up (issue tracked in `tests/chaos/TODO.md`)
// once the mc-api Dockerfile carries a hermetic seed step.
func TestHeapCeilingTriggersGracefulShutdown(t *testing.T) {
	// v6.1.0 CF-12 (35-sprint debt): removed t.Parallel so the
	// 16 MiB balloon is not racing other parallel allocators on
	// macOS test runners; the dwell + ceiling are still small
	// enough that the test completes well inside the 5s ctx, but
	// the parallel scheduler was the actual flake source.

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mgr := lifecycle.New(logger, 5*time.Second)

	var dbClosed, redisClosed atomic.Bool
	mgr.Register("postgres-pool", lifecycle.CloserFunc(func(ctx context.Context) error {
		dbClosed.Store(true)
		return nil
	}))
	mgr.Register("redis-client", lifecycle.CloserFunc(func(ctx context.Context) error {
		redisClosed.Store(true)
		return nil
	}))

	// Hold a sentinel allocation that exceeds the synthetic ceiling
	// so the sampler reports HeapInUseBytes > ceiling.
	const balloonBytes = 16 << 20 // 16 MiB
	balloon := make([]byte, balloonBytes)
	for i := range balloon {
		balloon[i] = byte(i)
	}
	runtime.GC()

	alarmFired := make(chan struct{}, 1)

	sampler := memwatch.NewSampler(logger, memwatch.Config{
		BinaryName:       "chaos-oom-test",
		SampleInterval:   10 * time.Millisecond,
		HeapCeilingBytes: 4 << 20, // 4 MiB ceiling vs. 16 MiB balloon
		HeapCeilingDwell: 50 * time.Millisecond,
		GoroutineCeiling: 100_000,
		GoroutineDwell:   time.Hour,
		// v6.1.0 CF-12: do NOT call mgr.Shutdown() from inside the
		// alarm callback; the sampler tick that fires the callback
		// runs on the same goroutine as Sampler.Run, and
		// mgr.Shutdown() would call sampler.Close() which blocks
		// waiting for that goroutine to exit -- a self-deadlock
		// that the original v2.10.1 test hid by never invoking
		// Sampler.Run in the first place. The work() function
		// drains the manager once the alarm has fired.
		HeapAlarmCallback: func() {
			select {
			case alarmFired <- struct{}{}:
			default:
			}
		},
	})
	mgr.Register("memwatch-sampler", sampler)

	// v6.1.0 CF-12: 5s budget (was 3s) gives macOS test runners
	// breathing room without changing the SLO under test.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// v6.1.0 CF-12: the original v2.10.1 test wired the sampler
	// as a Closer but never invoked Sampler.Run, so no tick ever
	// fired and the alarm never tripped. CF-12's "macOS flake"
	// signature was actually a deterministic failure that just
	// happened to look transient because the chaos suite was not
	// part of the every-PR gate. We now drive the sampler from
	// the work function so the test exercises the actual
	// breach-> alarm-> drain path it was always meant to.
	runErr := mgr.Run(ctx, func(ctx context.Context) error {
		samplerDone := make(chan struct{})
		// runCtx is cancelled when work() returns so the sampler
		// goroutine exits cleanly (no leak).
		runCtx, cancelRun := context.WithCancel(ctx)
		defer cancelRun()
		go func() {
			defer close(samplerDone)
			_ = sampler.Run(runCtx)
		}()
		select {
		case <-alarmFired:
			cancelRun()
			<-samplerDone
			return nil
		case <-ctx.Done():
			cancelRun()
			<-samplerDone
			return ctx.Err()
		}
	})

	runtime.KeepAlive(balloon)

	if runErr != nil && !errorIsContextOrShutdown(runErr) {
		t.Fatalf("Run returned unexpected error: %v", runErr)
	}

	if !dbClosed.Load() {
		t.Errorf("postgres-pool Closer was not invoked; logs:\n%s", logBuf.String())
	}
	if !redisClosed.Load() {
		t.Errorf("redis-client Closer was not invoked; logs:\n%s", logBuf.String())
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "memwatch.heap_ceiling_critical") {
		t.Errorf("expected critical heap log; got:\n%s", logs)
	}
	if !strings.Contains(logs, "lifecycle.closed") {
		t.Errorf("expected lifecycle.closed entries during drain; got:\n%s", logs)
	}
}

// TestGoroutineCeilingTriggersAlarm verifies that the sampler's
// goroutine-leak detector also fires its alarm callback after the
// configured dwell, mirroring the OOM path. Together with
// TestHeapCeilingTriggersGracefulShutdown this gives us the two
// resource-runaway alarms exercised end-to-end.
func TestGoroutineCeilingTriggersAlarm(t *testing.T) {
	// v6.1.0 CF-12: removed t.Parallel; goroutine ceiling is
	// sensitive to other parallel tests spawning workers.

	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))

	alarmFired := make(chan struct{}, 1)

	releaseLeaked := make(chan struct{})
	leakedCount := 50
	for i := 0; i < leakedCount; i++ {
		go func() {
			<-releaseLeaked
		}()
	}
	defer close(releaseLeaked)

	currentGoroutines := runtime.NumGoroutine()
	if currentGoroutines <= 10 {
		t.Skipf("unexpected baseline goroutine count %d (test relies on at least 10)", currentGoroutines)
	}

	sampler := memwatch.NewSampler(logger, memwatch.Config{
		BinaryName:       "chaos-goroutine-test",
		SampleInterval:   10 * time.Millisecond,
		HeapCeilingBytes: 4 << 30,
		HeapCeilingDwell: time.Hour,
		GoroutineCeiling: currentGoroutines - 10,
		GoroutineDwell:   30 * time.Millisecond,
		GoroutineAlarmCallback: func() {
			select {
			case alarmFired <- struct{}{}:
			default:
			}
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		_ = sampler.Run(ctx)
	}()
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer c()
		_ = sampler.Close(shutdownCtx)
	}()

	select {
	case <-alarmFired:
	case <-ctx.Done():
		t.Fatalf("goroutine alarm did not fire; sample_count=%d", sampler.SampleCount())
	}
}

func errorIsContextOrShutdown(err error) bool {
	if err == nil {
		return true
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context canceled"),
		strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "lifecycle: shutdown"):
		return true
	}
	return false
}
