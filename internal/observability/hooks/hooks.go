// Package hooks promotes the v6.2.0 MVP per-port adapter fixtures
// (workerpool.PoolMetrics, resilience.BreakerMetrics,
// coord.CoordinatorMetrics) into a single production wiring surface
// that cmd/mc-api/app.go startObservability can hand back to the
// composition root.
//
// Rationale: the v6.2.0 MVP shipped per-port adapter implementations
// only inside tests/integration/v620 contract test fixtures. The MVP
// report flagged that production traffic was not yet exercising the
// new ec_workerpool_* / ec_breaker_* / ec_coord_conflicts_total
// counters because cmd/* never constructed the adapter set. This
// v6.2.1 QA package closes that gap by:
//
//   - Providing one nil-safe constructor (FromRegistry) that maps the
//     existing metrics.Registry counters/gauges into the three port
//     interfaces.
//   - Letting the cmd/mc-api composition root expose the Hooks via
//     the same atomic.Pointer pattern already used for ecRegistry,
//     so any current or future workerpool / resilience / coord call
//     site can opt in without further wiring churn.
//
// Decomposition discipline (HARD GATE): every adapter method stays
// at cyclomatic 1 so the v6.2.0 complex_fn streak (held at 5 in
// this cycle) does not regress.
package hooks

import (
	"github.com/nfsarch33/agentic-ecommerce/internal/coord"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/resilience"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// Hooks bundles the v6.2.0 production adapter implementations. The
// fields are interface-typed so callers depend only on the port
// surface, never on the metrics.Registry concrete type.
type Hooks struct {
	// Pool implements workerpool.PoolMetrics and forwards into
	// ec_workerpool_active{pool} + ec_workerpool_rejected_total{pool,reason}.
	Pool workerpool.PoolMetrics

	// AdaptivePool implements workerpool.AdaptiveMetrics and forwards
	// adaptive target size + resize direction counters.
	AdaptivePool workerpool.AdaptiveMetrics

	// Breaker implements resilience.BreakerMetrics and forwards into
	// ec_breaker_open_total{name} + ec_breaker_half_open_total{name}.
	Breaker resilience.BreakerMetrics

	// Coord implements coord.CoordinatorMetrics (via coord.MetricsAdapter)
	// and forwards into ec_coord_conflicts_total{tenant_id, agent_a,
	// agent_b, resolution}.
	Coord coord.CoordinatorMetrics
}

// FromRegistry wires Hooks to the supplied metrics.Registry. Returns
// nil when reg is nil so callers can rely on the nil-safe contract
// already used by the v6.2.0 port interfaces.
func FromRegistry(reg *metrics.Registry) *Hooks {
	if reg == nil {
		return nil
	}
	return &Hooks{
		Pool: poolAdapter{
			active:   reg.WorkerpoolActive,
			rejected: reg.WorkerpoolRejected,
		},
		AdaptivePool: adaptivePoolAdapter{
			size:   reg.WorkerpoolSize,
			resize: reg.WorkerpoolResizeTotal,
		},
		Breaker: breakerAdapter{
			open:     reg.BreakerOpenTotal,
			halfOpen: reg.BreakerHalfOpenTotal,
		},
		Coord: coord.NewMetricsAdapter(coordEmitter{counter: reg.CoordConflictsTotal}),
	}
}

// poolAdapter implements workerpool.PoolMetrics against the v6.2.0
// ec_workerpool_active gauge + ec_workerpool_rejected_total counter.
type poolAdapter struct {
	active   *metrics.Gauge
	rejected *metrics.Counter
}

func (a poolAdapter) SetActive(pool string, value int) {
	if a.active == nil {
		return
	}
	a.active.Set(float64(value), metrics.Labels{"pool": pool})
}

func (a poolAdapter) IncRejected(pool string, reason string) {
	if a.rejected == nil {
		return
	}
	a.rejected.Inc(metrics.Labels{"pool": pool, "reason": reason})
}

type adaptivePoolAdapter struct {
	size   *metrics.Gauge
	resize *metrics.Counter
}

func (a adaptivePoolAdapter) SetWorkerpoolSize(pool string, value int) {
	if a.size == nil {
		return
	}
	a.size.Set(float64(value), metrics.Labels{"pool": pool})
}

func (a adaptivePoolAdapter) IncWorkerpoolResize(pool, direction string) {
	if a.resize == nil {
		return
	}
	a.resize.Inc(metrics.Labels{"pool": pool, "direction": direction})
}

// breakerAdapter implements resilience.BreakerMetrics against the
// v6.2.0 ec_breaker_open_total + ec_breaker_half_open_total counters.
type breakerAdapter struct {
	open     *metrics.Counter
	halfOpen *metrics.Counter
}

func (a breakerAdapter) IncOpen(name string) {
	if a.open == nil {
		return
	}
	a.open.Inc(metrics.Labels{"name": name})
}

func (a breakerAdapter) IncHalfOpen(name string) {
	if a.halfOpen == nil {
		return
	}
	a.halfOpen.Inc(metrics.Labels{"name": name})
}

// coordEmitter bridges metrics.Counter (which takes metrics.Labels)
// into coord.MetricEmitter (which takes plain map[string]string).
// Go's nominal typing requires the adapter even though the
// underlying shape is identical.
type coordEmitter struct {
	counter *metrics.Counter
}

func (e coordEmitter) Inc(labels map[string]string) {
	if e.counter == nil {
		return
	}
	e.counter.Inc(metrics.Labels(labels))
}

// Compile-time interface checks: any drift between port and adapter
// signatures fails the build instead of silently going un-wired.
var (
	_ workerpool.PoolMetrics     = poolAdapter{}
	_ workerpool.AdaptiveMetrics = adaptivePoolAdapter{}
	_ resilience.BreakerMetrics  = breakerAdapter{}
	_ coord.MetricEmitter        = coordEmitter{}
)
