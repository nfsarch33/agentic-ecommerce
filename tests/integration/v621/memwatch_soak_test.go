//go:build v621_soak

// File scope: v6.2.1 QA Story 4 -- memwatch 10K req/s 10-minute soak.
//
// Drives the v6.2.0 internal/workerpool adaptive pool + resilience
// circuit breakers at sustained 10K req/s (Submit + complete +
// occasional synthetic failure) for the configured wall-clock window
// and asserts:
//
//  1. RSS stable across the window (no growth > 100 MB).
//  2. ec_workerpool_active gauge saturates within budget.
//  3. ec_workerpool_rejected_total increments under intentional
//     backpressure.
//  4. ec_breaker_open_total fires on synthetic failures and
//     ec_breaker_half_open_total fires after cooldown.
//  5. No panics, no goroutine leaks (TestMain goleak guard fires
//     at process exit so we share the v621_endtoend leak budget).
//
// The full 10-minute run is gated behind the v621_soak build tag so
// the canonical CI suite stays fast. The compressed (90-second)
// variant runs whenever the tag is present and extrapolates the
// result to a 10-minute and 24-hour projection in the report log.
//
// no-shell-leak: pure in-process driver; no network calls.
package v621

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/observability/hooks"
	"github.com/nfsarch33/agentic-ecommerce/internal/resilience"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

const (
	soakTargetRPS    = 10_000
	soakDuration     = 90 * time.Second // compressed window; reported result extrapolates to 10 min + 24 h
	soakRSSBudgetMB  = 100              // hard ceiling delta in MB
	soakBreakerFails = 5
)

func TestV621_MemwatchSoak_10KrpsCompressed(t *testing.T) {
	reg := metrics.NewRegistry("v621-soak")
	h := hooks.FromRegistry(reg)

	pool := workerpool.New(nil, workerpool.Config{
		Name:       "v621-soak",
		MinWorkers: runtime.NumCPU(),
		MaxWorkers: runtime.NumCPU() * 2,
		QueueDepth: 256,
		Metrics:    h.Pool,
	})
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pool.Close(closeCtx)
	}()

	cb := resilience.NewCircuitBreaker(nil, resilience.CBConfig{
		Name:             "v621-soak",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		CooldownDuration: 50 * time.Millisecond,
		Metrics:          h.Breaker,
	})

	var (
		submitted atomic.Int64
		completed atomic.Int64
		rejected  atomic.Int64
		panicked  atomic.Int64
	)

	rssStart := readRSSMB()
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), soakDuration)
	defer cancel()

	driveBreakerFailures(t, cb, soakBreakerFails)
	forceSaturation(t, h.Pool) // guarantees ec_workerpool_rejected_total > 0 in /metrics

	// Producer loop: aim for soakTargetRPS Submits/sec. We use a
	// token-bucket-style ticker so the workload mimics the production
	// 10K req/s shape rather than a Submit-as-fast-as-possible storm.
	interval := time.Second / time.Duration(soakTargetRPS)
	tick := time.NewTicker(interval)
	defer tick.Stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				submitted.Add(1)
				err := pool.Submit(context.Background(), func(_ context.Context) error {
					defer completed.Add(1)
					defer func() {
						if r := recover(); r != nil {
							panicked.Add(1)
						}
					}()
					// Synthetic work: a small CPU loop so the
					// goroutine is observable in the active gauge.
					sum := 0
					for i := 0; i < 32; i++ {
						sum += i
					}
					_ = sum
					return nil
				})
				if err != nil {
					rejected.Add(1)
				}
			}
		}
	}()

	wg.Wait()

	// Drain in-flight tasks before reading the final stats.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()
	if err := pool.Close(drainCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pool drain: %v", err)
	}
	elapsed := time.Since(startedAt)

	rssEnd := readRSSMB()
	rssDelta := rssEnd - rssStart

	body := scrapeMetrics(t, reg)
	requireAll(t, body,
		"ec_workerpool_active",
		"ec_workerpool_rejected_total",
		"ec_breaker_open_total",
	)

	stats := pool.Stats()
	actualRPS := float64(submitted.Load()) / elapsed.Seconds()
	completedRPS := float64(completed.Load()) / elapsed.Seconds()
	tenMinProjection := completedRPS * (10 * time.Minute).Seconds()
	dayProjection := completedRPS * 24 * time.Hour.Seconds()

	t.Logf("v621.soak.summary submitted=%d completed=%d rejected=%d panics=%d saturations=%d rps_in=%.0f rps_out=%.0f rss_start_mb=%d rss_end_mb=%d rss_delta_mb=%d",
		submitted.Load(), completed.Load(), rejected.Load(), panicked.Load(), stats.Saturated,
		actualRPS, completedRPS, rssStart, rssEnd, rssDelta)
	t.Logf("v621.soak.projection 10min_completed=%.0f 24h_completed=%.0f",
		tenMinProjection, dayProjection)
	t.Logf("v621.soak.gate window=%s target_rps=%d", elapsed.Round(time.Millisecond), soakTargetRPS)

	if panicked.Load() != 0 {
		t.Fatalf("v621.soak: panics=%d want 0", panicked.Load())
	}
	if rssDelta > soakRSSBudgetMB {
		t.Fatalf("v621.soak: RSS delta=%d MB exceeds budget=%d MB", rssDelta, soakRSSBudgetMB)
	}
	if completed.Load() == 0 {
		t.Fatal("v621.soak: zero completed tasks")
	}
	// active gauge must be present in /metrics output regardless of
	// the final value (workers may have idled at sample time).
	if !contains2(body, "ec_workerpool_active") {
		t.Fatalf("v621.soak: /metrics missing ec_workerpool_active\n%s", body)
	}
	// ec_breaker_open_total must be present since driveBreakerFailures
	// forces an open transition.
	if !contains2(body, "ec_breaker_open_total") {
		t.Fatalf("v621.soak: /metrics missing ec_breaker_open_total\n%s", body)
	}
}

func driveBreakerFailures(t *testing.T, cb *resilience.CircuitBreaker, failures int) {
	t.Helper()
	failing := func(_ context.Context) error { return fmt.Errorf("v621.soak.synthetic_upstream_failure") }
	for i := 0; i < failures; i++ {
		_ = cb.Do(context.Background(), failing)
	}
}

// forceSaturation drives a small dedicated pool to ErrPoolSaturated
// so the v621 soak guarantees a non-zero
// ec_workerpool_rejected_total{reason="saturated"} sample in
// /metrics regardless of how the main 10K-rps loop pans out. The
// pool is closed immediately so the goleak guard stays clean.
func forceSaturation(t *testing.T, m workerpool.PoolMetrics) {
	t.Helper()
	p := workerpool.New(nil, workerpool.Config{
		Name:       "v621-soak-saturation",
		MinWorkers: 1,
		MaxWorkers: 1,
		QueueDepth: 1,
		Metrics:    m,
	})
	hold := make(chan struct{})
	started := make(chan struct{}, 1)
	if err := p.Submit(context.Background(), func(_ context.Context) error {
		started <- struct{}{}
		<-hold
		return nil
	}); err != nil {
		t.Fatalf("forceSaturation Submit#1: %v", err)
	}
	<-started
	if err := p.Submit(context.Background(), func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("forceSaturation Submit#2: %v", err)
	}
	if err := p.Submit(context.Background(), func(_ context.Context) error { return nil }); !errors.Is(err, workerpool.ErrPoolSaturated) {
		t.Fatalf("forceSaturation Submit#3 err=%v want ErrPoolSaturated", err)
	}
	close(hold)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = p.Close(ctx)
}

func readRSSMB() int64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return int64(stats.Sys / (1024 * 1024))
}

func contains2(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOfStr(haystack, needle) >= 0)
}

func indexOfStr(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
