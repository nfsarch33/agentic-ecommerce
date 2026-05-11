package hooks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/coord"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/resilience"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

func TestFromRegistry_NilReturnsNil(t *testing.T) {
	t.Parallel()
	if h := FromRegistry(nil); h != nil {
		t.Fatalf("FromRegistry(nil) = %v want nil", h)
	}
}

func TestFromRegistry_PopulatesAllPorts(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry("hooks-test")
	h := FromRegistry(reg)
	if h == nil {
		t.Fatal("FromRegistry returned nil")
	}
	if h.Pool == nil {
		t.Fatal("Hooks.Pool nil")
	}
	if h.Breaker == nil {
		t.Fatal("Hooks.Breaker nil")
	}
	if h.Coord == nil {
		t.Fatal("Hooks.Coord nil")
	}
}

// TestFromRegistry_PoolHook drives the workerpool adapter into the
// registry and confirms the metric names appear on /metrics.
func TestFromRegistry_PoolHook(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry("hooks-test")
	h := FromRegistry(reg)

	p := workerpool.New(nil, workerpool.Config{
		Name:       "hooks-pool",
		MinWorkers: 1,
		MaxWorkers: 1,
		QueueDepth: 1,
		Metrics:    h.Pool,
	})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

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
		t.Fatal("worker did not start")
	}
	if err := p.Submit(context.Background(), func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("Submit#2: %v", err)
	}
	if err := p.Submit(context.Background(), func(_ context.Context) error { return nil }); !errors.Is(err, workerpool.ErrPoolSaturated) {
		t.Fatalf("Submit#3 err=%v want ErrPoolSaturated", err)
	}
	close(hold)

	body := scrapeMetricsBody(t, reg)
	if !strings.Contains(body, "ec_workerpool_active") {
		t.Fatalf("/metrics missing ec_workerpool_active\n%s", body)
	}
	if !strings.Contains(body, "ec_workerpool_rejected_total") {
		t.Fatalf("/metrics missing ec_workerpool_rejected_total\n%s", body)
	}
	if !strings.Contains(body, `pool="hooks-pool"`) {
		t.Fatalf("/metrics missing pool label\n%s", body)
	}
}

// TestFromRegistry_BreakerHook drives the resilience adapter into
// the registry and confirms the open/half-open counter names appear.
func TestFromRegistry_BreakerHook(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry("hooks-test")
	h := FromRegistry(reg)
	current := time.Now().UTC()
	cb := resilience.NewCircuitBreaker(nil, resilience.CBConfig{
		Name:             "hooks-stripe",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		CooldownDuration: 10 * time.Millisecond,
		NowFunc:          func() time.Time { return current },
		Metrics:          h.Breaker,
	})
	failing := func(_ context.Context) error { return errors.New("upstream 503") }
	for i := 0; i < 2; i++ {
		_ = cb.Do(context.Background(), failing)
	}
	current = current.Add(20 * time.Millisecond)
	_ = cb.Do(context.Background(), func(_ context.Context) error { return nil })

	body := scrapeMetricsBody(t, reg)
	if !strings.Contains(body, "ec_breaker_open_total") {
		t.Fatalf("/metrics missing ec_breaker_open_total\n%s", body)
	}
	if !strings.Contains(body, "ec_breaker_half_open_total") {
		t.Fatalf("/metrics missing ec_breaker_half_open_total\n%s", body)
	}
	if !strings.Contains(body, `name="hooks-stripe"`) {
		t.Fatalf("/metrics missing name label\n%s", body)
	}
}

// TestFromRegistry_CoordHook drives the coord.MetricsAdapter into
// the registry and confirms the conflicts counter name appears.
func TestFromRegistry_CoordHook(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry("hooks-test")
	h := FromRegistry(reg)
	if h.Coord == nil {
		t.Fatal("Coord adapter nil")
	}
	h.Coord.RecordCoordinationConflict("tenant-7", "PricingAgent", "FulfilmentAgent", "last_write_wins")
	body := scrapeMetricsBody(t, reg)
	if !strings.Contains(body, "ec_coord_conflicts_total") {
		t.Fatalf("/metrics missing ec_coord_conflicts_total\n%s", body)
	}
	for _, want := range []string{
		`tenant_id="tenant-7"`,
		`agent_a="PricingAgent"`,
		`agent_b="FulfilmentAgent"`,
		`resolution="last_write_wins"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing label %q\n%s", want, body)
		}
	}
}

// TestPoolAdapter_NilSafe confirms the nil-counter guards stay
// silent so a misconfigured composition root cannot panic the
// request path.
func TestPoolAdapter_NilSafe(t *testing.T) {
	t.Parallel()
	a := poolAdapter{}
	a.SetActive("any", 5)
	a.IncRejected("any", "saturated")
}

func TestBreakerAdapter_NilSafe(t *testing.T) {
	t.Parallel()
	a := breakerAdapter{}
	a.IncOpen("any")
	a.IncHalfOpen("any")
}

func TestCoordEmitter_NilSafe(t *testing.T) {
	t.Parallel()
	e := coordEmitter{}
	e.Inc(map[string]string{"tenant_id": "t"})
}

// _ unused; pull in the coord interface so the import survives even
// if the production adapter path stops referencing it.
var _ coord.CoordinatorMetrics = (*coord.MetricsAdapter)(nil)

func scrapeMetricsBody(t *testing.T, reg *metrics.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reg.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}
