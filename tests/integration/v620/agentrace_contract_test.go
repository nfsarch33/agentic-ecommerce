// File scope: v6.2.0 Story 1 contract test.
//
// Wires the production agentrace.Adapter -> NDJSON file ->
// evomap.AgentraceAdapter reader path so the smoke "trigger 10 tool
// calls -> verify >= 10 events ingestible" gate has CI coverage.
//
// The transport is a filesystem path (no raw IPs in argv per
// no-shell-leak.mdc). Production deployments swap the path for the
// runx-aliased writer to gpu-host-1; the contract proven here -- writer
// produces NDJSON the existing reader understands -- is identical.
package v620

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/coord"
	"github.com/nfsarch33/helixon-ec/internal/evomap"
	"github.com/nfsarch33/helixon-ec/internal/metrics"
	"github.com/nfsarch33/helixon-ec/internal/observability/agentrace"
	"github.com/nfsarch33/helixon-ec/internal/resilience"
	"github.com/nfsarch33/helixon-ec/internal/workerpool"
)

// safeFileSink is a filesystem sink that flushes after every write
// so the reader observes the events without depending on OS buffering.
type safeFileSink struct {
	mu sync.Mutex
	f  *os.File
}

func (s *safeFileSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.f.Write(p)
	if err != nil {
		return n, err
	}
	return n, s.f.Sync()
}

func TestV620_AgentraceSmokeTenEventsIngestible(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	if err := agentrace.ValidateTransportTarget(path); err != nil {
		t.Fatalf("ValidateTransportTarget(%q) err=%v want nil", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer func() { _ = f.Close() }()
	sink := &safeFileSink{f: f}

	logger := slog.Default()
	a, err := agentrace.NewAdapter(logger, agentrace.Config{
		Sink:           sink,
		BufferSize:     32,
		FlushInterval:  10 * time.Millisecond,
		WriteTimeout:   500 * time.Millisecond,
		TransportLabel: "alias:tmp.events",
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = a.Close(ctx)
	}()

	for i := 0; i < 10; i++ {
		if err := a.Emit(context.Background(), agentrace.Event{
			Type: "tool_call", Tool: "Shell", Outcome: "ok", SessionID: "smoke-v620",
		}); err != nil {
			t.Fatalf("Emit[%d]: %v", i, err)
		}
	}

	// Drain the writer loop so the file is flushed.
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.Close(closeCtx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	count := countNDJSONLines(t, path)
	if count < 10 {
		t.Fatalf("ndjson lines = %d want >= 10", count)
	}

	// Round-trip through the existing reader so we prove the schema
	// the EvoLoop pipeline already understands stays valid.
	reader := evomap.NewAgentraceAdapter(evomap.AgentraceAdapterConfig{
		HTTPURL:   "http://127.0.0.1:0", // forced HTTP failure -> falls back to JSONL
		JSONLPath: path,
		Logger:    logger,
	})
	kpis := reader.Read(context.Background())
	if !kpis.Available {
		t.Fatal("evomap reader did not surface NDJSON-backed KPIs")
	}
	if kpis.ToolCallCount < 10 {
		t.Fatalf("evomap ToolCallCount = %d want >= 10", kpis.ToolCallCount)
	}
}

// TestV620_CoordinatorMetricsFlowsThroughRegistry verifies the CF-16
// composition root wiring: the coord.MetricsAdapter -> metrics
// Counter -> /metrics scrape contract.
func TestV620_CoordinatorMetricsFlowsThroughRegistry(t *testing.T) {
	t.Parallel()
	registry := metrics.NewRegistry("contract-test")
	adapter := coord.NewMetricsAdapter(coordConflictEmitter{counter: registry.CoordConflictsTotal})
	if adapter == nil {
		t.Fatal("NewMetricsAdapter returned nil for non-nil counter")
	}
	adapter.RecordCoordinationConflict("tenant-1", "PricingAgent", "FulfilmentAgent", "last_write_wins")
	body := scrapeMetrics(t, registry)
	if !contains(body, "ec_coord_conflicts_total") {
		t.Fatalf("/metrics missing ec_coord_conflicts_total\n%s", body)
	}
}

// TestV620_WorkerpoolEmitsMetrics verifies the Story 3 hooks fire on
// real worker activity.
func TestV620_WorkerpoolEmitsMetrics(t *testing.T) {
	t.Parallel()
	registry := metrics.NewRegistry("contract-test")
	pool := workerpool.New(nil, workerpool.Config{
		Name:       "v620-contract",
		MinWorkers: 2,
		MaxWorkers: 2,
		QueueDepth: 4,
		Metrics: workerpoolMetricsAdapter{
			active:   registry.WorkerpoolActive,
			rejected: registry.WorkerpoolRejected,
		},
	})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = pool.Close(ctx)
	}()
	done := make(chan struct{}, 1)
	if err := pool.Submit(context.Background(), func(_ context.Context) error {
		done <- struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("task never ran")
	}
	body := scrapeMetrics(t, registry)
	if !contains(body, "ec_workerpool_active") {
		t.Fatalf("/metrics missing ec_workerpool_active\n%s", body)
	}
}

// TestV620_BreakerMetricsExposed verifies the Story 3 breaker hook
// flows transitions into the registry.
func TestV620_BreakerMetricsExposed(t *testing.T) {
	t.Parallel()
	registry := metrics.NewRegistry("contract-test")
	cb := resilience.NewCircuitBreaker(nil, resilience.CBConfig{
		Name:             "stripe",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		CooldownDuration: 5 * time.Millisecond,
		Metrics: breakerMetricsAdapter{
			open:     registry.BreakerOpenTotal,
			halfOpen: registry.BreakerHalfOpenTotal,
		},
	})
	for i := 0; i < 2; i++ {
		_ = cb.Do(context.Background(), func(_ context.Context) error { return errFail })
	}
	body := scrapeMetrics(t, registry)
	if !contains(body, "ec_breaker_open_total") {
		t.Fatalf("/metrics missing ec_breaker_open_total\n%s", body)
	}
}

// --- adapters that bridge interfaces to *Counter / *Gauge ---------

type workerpoolMetricsAdapter struct {
	active   *metrics.Gauge
	rejected *metrics.Counter
}

func (a workerpoolMetricsAdapter) SetActive(pool string, value int) {
	a.active.Set(float64(value), metrics.Labels{"pool": pool})
}

func (a workerpoolMetricsAdapter) IncRejected(pool, reason string) {
	a.rejected.Inc(metrics.Labels{"pool": pool, "reason": reason})
}

type breakerMetricsAdapter struct {
	open     *metrics.Counter
	halfOpen *metrics.Counter
}

func (a breakerMetricsAdapter) IncOpen(name string)     { a.open.Inc(metrics.Labels{"name": name}) }
func (a breakerMetricsAdapter) IncHalfOpen(name string) { a.halfOpen.Inc(metrics.Labels{"name": name}) }

// coordConflictEmitter bridges the metrics.Counter (which takes
// metrics.Labels) into the coord.MetricEmitter interface (which
// takes a plain map[string]string). The two types share the same
// underlying shape but Go's nominal typing rejects the direct cast.
type coordConflictEmitter struct {
	counter *metrics.Counter
}

func (e coordConflictEmitter) Inc(labels map[string]string) {
	e.counter.Inc(metrics.Labels(labels))
}

// --- helpers ------------------------------------------------------

var errFail = &intentionalErr{msg: "v620 contract upstream failure"}

type intentionalErr struct{ msg string }

func (e *intentionalErr) Error() string { return e.msg }

func countNDJSONLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open ndjson: %v", err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return count
}

func scrapeMetrics(t *testing.T, registry *metrics.Registry) string {
	t.Helper()
	rec := newScrapeRecorder()
	req := newScrapeRequest()
	registry.Handler().ServeHTTP(rec, req)
	return rec.body.String()
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
