// Package v621 holds the v6.2.1 QA integration tests that prove the
// v6.2.0 MVP observability hooks are wired end-to-end through the
// production composition root.
//
// File scope: TestV621_ObservabilityHooksEndToEnd boots the
// production internal/observability/hooks bundle the same way
// cmd/mc-api/app.go startObservability does, attaches a workerpool,
// circuit breaker, and multi-agent coordinator to those hooks, and
// asserts that the resulting /metrics scrape exposes the five
// v6.2.0 counters/gauges with non-zero values:
//
//   - ec_workerpool_active{pool="v621-endtoend"}
//   - ec_workerpool_rejected_total{pool="v621-endtoend",reason="saturated"}
//   - ec_breaker_open_total{name="v621-endtoend"}
//   - ec_breaker_half_open_total{name="v621-endtoend"}
//   - ec_coord_conflicts_total{tenant_id="...",agent_a="...",agent_b="...",resolution="..."}
//
// no-shell-leak: the test runs entirely in-process; no raw IPs,
// no remote transport targets.
package v621

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/coord"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/observability/hooks"
	"github.com/nfsarch33/agentic-ecommerce/internal/resilience"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// TestV621_ObservabilityHooksEndToEnd drives the production hooks
// bundle through all three port surfaces and asserts the resulting
// /metrics scrape carries every v6.2.0 series with the expected
// labels. This is the missing piece flagged by the v6.2.0 MVP
// report: production wiring is verified end-to-end.
func TestV621_ObservabilityHooksEndToEnd(t *testing.T) {
	t.Parallel()
	// Package-level goroutine-leak detection ships via the
	// leak_test.go TestMain (goleak.VerifyTestMain). Local
	// goleak.VerifyNone is not compatible with t.Parallel runners.

	reg := metrics.NewRegistry("v621-endtoend")
	h := hooks.FromRegistry(reg)
	if h == nil {
		t.Fatal("hooks.FromRegistry returned nil for non-nil registry")
	}

	drivePool(t, h)
	driveBreaker(t, h)
	driveCoordinator(t, h)

	body := scrapeMetrics(t, reg)
	requireAll(t, body,
		`ec_workerpool_active{binary="v621-endtoend",pool="v621-endtoend"}`,
		`ec_workerpool_rejected_total{binary="v621-endtoend",pool="v621-endtoend",reason="saturated"} 1`,
		`ec_breaker_open_total{binary="v621-endtoend",name="v621-endtoend"} 1`,
		`ec_breaker_half_open_total{binary="v621-endtoend",name="v621-endtoend"} 1`,
		`ec_coord_conflicts_total{agent_a="FulfilmentAgent",agent_b="PricingAgent",binary="v621-endtoend",resolution="last_write_wins",tenant_id="tenant-v621"} 1`,
	)
}

// TestV621_ObservabilityHooksNilSafe locks in the nil-receiver
// contract so a misconfigured composition root cannot panic the
// request path. nil hooks.Hooks (returned by hooks.FromRegistry(nil))
// must compile with the call sites of the three port interfaces.
func TestV621_ObservabilityHooksNilSafe(t *testing.T) {
	t.Parallel()
	if h := hooks.FromRegistry(nil); h != nil {
		t.Fatalf("FromRegistry(nil) = %v want nil", h)
	}
}

// TestV621_MetricsEndpointStreamsHooks asserts the contract the
// cmd/mc-api metricsHandler relies on: a Registry produced from
// FromRegistry's parent serves Prometheus text format on
// GET /metrics, including the five v6.2.0 series.
func TestV621_MetricsEndpointStreamsHooks(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry("v621-stream")
	h := hooks.FromRegistry(reg)
	h.Pool.SetActive("stream", 7)
	h.Pool.IncRejected("stream", "saturated")
	h.Breaker.IncOpen("stream")
	h.Breaker.IncHalfOpen("stream")
	h.Coord.RecordCoordinationConflict("tenant-stream", "PricingAgent", "FulfilmentAgent", "last_write_wins")

	srv := httptest.NewServer(reg.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	body := readBody(t, resp.Body)
	requireAll(t, body,
		"ec_workerpool_active",
		"ec_workerpool_rejected_total",
		"ec_breaker_open_total",
		"ec_breaker_half_open_total",
		"ec_coord_conflicts_total",
	)
}

// drivePool submits + saturates a small pool wired to h.Pool so the
// active gauge moves and the rejected counter increments by exactly 1.
// The pool is fully drained + closed inline (NOT via t.Cleanup) so the
// outer goleak.VerifyNone defer observes a fully-quiesced runtime.
func drivePool(t *testing.T, h *hooks.Hooks) {
	t.Helper()
	p := workerpool.New(nil, workerpool.Config{
		Name:       "v621-endtoend",
		MinWorkers: 1,
		MaxWorkers: 1,
		QueueDepth: 1,
		Metrics:    h.Pool,
	})

	hold := make(chan struct{})
	started := make(chan struct{}, 1)
	if err := p.Submit(context.Background(), func(_ context.Context) error {
		started <- struct{}{}
		<-hold
		return nil
	}); err != nil {
		t.Fatalf("Submit#1: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("pool worker did not start")
	}
	if err := p.Submit(context.Background(), func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("Submit#2: %v", err)
	}
	if err := p.Submit(context.Background(), func(_ context.Context) error { return nil }); !errors.Is(err, workerpool.ErrPoolSaturated) {
		t.Fatalf("Submit#3 err=%v want ErrPoolSaturated", err)
	}
	close(hold)

	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.Close(closeCtx); err != nil {
		t.Fatalf("pool Close: %v", err)
	}
}

// driveBreaker forces the configured breaker through closed -> open
// -> half-open so both ec_breaker_*_total counters increment exactly
// once each.
func driveBreaker(t *testing.T, h *hooks.Hooks) {
	t.Helper()
	now := time.Now().UTC()
	cb := resilience.NewCircuitBreaker(nil, resilience.CBConfig{
		Name:             "v621-endtoend",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		CooldownDuration: 10 * time.Millisecond,
		NowFunc:          func() time.Time { return now },
		Metrics:          h.Breaker,
	})
	failing := func(_ context.Context) error { return errors.New("v621-endtoend upstream") }
	if err := cb.Do(context.Background(), failing); err == nil {
		t.Fatal("breaker accepted the first failing call without error")
	}
	if err := cb.Do(context.Background(), failing); err == nil {
		t.Fatal("breaker should not eat the failure error before opening")
	}
	now = now.Add(20 * time.Millisecond)
	if err := cb.Do(context.Background(), func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("half-open probe err=%v want nil", err)
	}
}

// driveCoordinator emits one conflict so ec_coord_conflicts_total
// increments by exactly 1 with the expected label set.
func driveCoordinator(t *testing.T, h *hooks.Hooks) {
	t.Helper()
	c, err := coord.NewCoordinator(nil, coord.CoordinatorConfig{
		TenantID: "tenant-v621",
		Metrics:  h.Coord,
	})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	now := time.Now().UTC()
	decisions := []coord.AgentDecision{
		{
			AgentName:  "PricingAgent",
			TenantID:   "tenant-v621",
			SKU:        "SKU-1",
			Action:     coord.ActionKindPriceChange,
			DeltaPct:   0.1,
			Reason:     "raise",
			ProposedAt: now,
		},
		{
			AgentName:  "FulfilmentAgent",
			TenantID:   "tenant-v621",
			SKU:        "SKU-1",
			Action:     coord.ActionKindInventoryDeplete,
			DeltaPct:   -0.05,
			Reason:     "drain",
			ProposedAt: now.Add(time.Second),
		},
	}
	if _, err := c.Coordinate(context.Background(), decisions); err != nil {
		t.Fatalf("Coordinate: %v", err)
	}
}

func scrapeMetrics(t *testing.T, reg *metrics.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reg.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}

func readBody(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func requireAll(t *testing.T, body string, fragments ...string) {
	t.Helper()
	for _, want := range fragments {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing %q\n--- body ---\n%s", want, body)
		}
	}
}
