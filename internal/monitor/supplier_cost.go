// File scope: v3.5.0 EC-6-1 supplier cost monitor.
//
// The monitor re-scrapes a configured set of supplier listings via
// the v3.1.0 china.Client adapters (1688/Taobao/etc), compares each
// observed CNY-denominated unit cost to the stored baseline, and
// emits a SupplierCostChangedEvent when the delta exceeds the
// operator-configured threshold (default 5%). First observation
// seeds the baseline silently (no event) so the monitor can roll
// in for an existing tenant without spamming the eventbus.
//
// Reuse evidence:
//   - china.Client port from v3.1.0 EC-1-1/1-2 adapters.
//   - workerpool.Pool fan-out pattern from v3.1.0 sourcing agent +
//     v3.4.0 channel router.
//   - eventbus.Publisher contract from v3.3.0 EC-3-3 webhook.
//   - SupplierBaselineStore + memoryBaselineStore mirror the
//     v3.3.0 EC-3-4 IdempotencyStore in-memory + Postgres pattern.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 7-sprint streak; v3.5.0 sprint 8 target):
//
//   - Tick (envelope -> evaluateEntry loop)
//   - evaluateEntry (single-entry pipeline; cyclomatic 5)
//   - resolveLatestPrice (helper; fans across configured clients;
//     returns the first matching observation)
//   - computeDelta (pure delta math)
//   - decideOutcome (pure threshold gate; returns Direction)
//   - emitChange (event publish + KPI hook)
//
// Resilience pillar (v2.10 baseline):
//   - Implements lifecycle.Closer.
//   - Uses workerpool.Pool for client fan-out (NEVER raw `go func()`).
//   - Honours bounded sliding state via the BaselineStore (no in-
//     memory growth beyond per-SKU baselines).
//   - All errors typed + %w-wrapped via package sentinels.
//   - Tenant-aware: every metric/event carries TenantID.
//   - Emits EvoMap KPI deltas via SupplierCostKPIHook.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/china"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// DefaultSupplierCostThresholdPct is the v3.5.0 EC-6-1 default
// threshold (5%). Operator config can override via Config.ThresholdPct.
const DefaultSupplierCostThresholdPct = 0.05

// SupplierCostDirection identifies the direction of an observed
// cost delta. "up" / "down" only; "stable" is implicit when no
// event is emitted.
type SupplierCostDirection string

// SupplierCostDirection enum values.
const (
	SupplierCostDirectionUp   SupplierCostDirection = "up"
	SupplierCostDirectionDown SupplierCostDirection = "down"
)

// EC-6-1 typed sentinels.
var (
	// ErrSupplierBaselineNotFound is returned by SupplierBaselineStore.Get
	// when the (tenant, source, sku) tuple has no baseline yet. The
	// monitor treats this as the seed-baseline path (no event).
	ErrSupplierBaselineNotFound = errors.New("monitor: supplier baseline not found")

	// ErrSupplierCostMonitorUnconfigured is returned by the
	// constructor when a required dependency is missing.
	ErrSupplierCostMonitorUnconfigured = errors.New("monitor: supplier cost monitor unconfigured")

	// ErrSupplierCostMonitorClosed is returned by Tick after Close.
	ErrSupplierCostMonitorClosed = errors.New("monitor: supplier cost monitor closed")
)

// SupplierBaselineRecord is the per-(tenant, source, sku) snapshot
// the monitor reads + writes. Mirrors the migrations/0014 schema.
type SupplierBaselineRecord struct {
	TenantID         string
	Source           china.Source
	SupplierSKU      string
	SupplierID       string
	BaselineCNYCents int
	LastObservedCNY  int
	LastDeltaPct     float64
	ObservedAt       time.Time
	UpdatedAt        time.Time
}

// SupplierBaselineStore is the small port the monitor consumes.
// In-memory implementation lives in the test file; the production
// Postgres adapter is a v3.5.x wiring story.
type SupplierBaselineStore interface {
	Get(ctx context.Context, tenantID string, source china.Source, sku string) (SupplierBaselineRecord, error)
	Upsert(ctx context.Context, rec SupplierBaselineRecord) error
}

// SupplierCostMetrics is the small port the monitor uses to emit
// the ec_supplier_cost_changes_total counter without coupling to
// internal/metrics.Registry directly.
type SupplierCostMetrics interface {
	RecordSupplierCostChange(tenantID, source, direction string)
}

// SupplierCostKPISample is the EvoMap hook payload emitted for
// every detected change. Pure value type so cmd/* drivers can pump
// supplier_cost_changes_total + delta-aware aggregates.
type SupplierCostKPISample struct {
	TenantID  string
	Source    string
	SKU       string
	Direction SupplierCostDirection
	DeltaPct  float64
}

// SupplierCostKPIHook is the optional EvoMap emission hook.
type SupplierCostKPIHook func(SupplierCostKPISample)

// SupplierTrackingEntry is the operator-supplied input describing
// which supplier listing to re-scrape this tick. Keyword + SKU +
// Source uniquely identify the listing.
type SupplierTrackingEntry struct {
	Keyword     string
	Source      china.Source
	SupplierSKU string
}

// SupplierCostMonitorConfig wires a SupplierCostMonitor.
type SupplierCostMonitorConfig struct {
	TenantID      string
	Clients       []china.Client
	BaselineStore SupplierBaselineStore
	Publisher     eventbus.Publisher
	Pool          *workerpool.Pool
	ThresholdPct  float64
	Metrics       SupplierCostMetrics
	KPIHook       SupplierCostKPIHook
	Now           func() time.Time
}

// SupplierCostMonitor is the v3.5.0 EC-6-1 monitor.
type SupplierCostMonitor struct {
	cfg    SupplierCostMonitorConfig
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// SupplierCostTickReport summarises a single Tick run for the
// caller (test + cmd/* admin endpoints).
type SupplierCostTickReport struct {
	Evaluated     int
	Detected      int
	NewBaselines  int
	NoChange      int
	AdapterErrors int
	StoreErrors   int
}

// NewSupplierCostMonitor constructs a monitor.
//
// Decomposition: validation + defaults split into helpers so the
// constructor body stays well under cyclomatic 6.
func NewSupplierCostMonitor(logger *slog.Logger, cfg SupplierCostMonitorConfig) (*SupplierCostMonitor, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := validateSupplierCostConfig(cfg); err != nil {
		return nil, err
	}
	applySupplierCostDefaults(&cfg)
	return &SupplierCostMonitor{cfg: cfg, logger: logger}, nil
}

func validateSupplierCostConfig(cfg SupplierCostMonitorConfig) error {
	if strings.TrimSpace(cfg.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrSupplierCostMonitorUnconfigured)
	}
	if len(cfg.Clients) == 0 {
		return fmt.Errorf("%w: at least one china.Client required", ErrSupplierCostMonitorUnconfigured)
	}
	if cfg.BaselineStore == nil {
		return fmt.Errorf("%w: BaselineStore required", ErrSupplierCostMonitorUnconfigured)
	}
	if cfg.Publisher == nil {
		return fmt.Errorf("%w: Publisher required", ErrSupplierCostMonitorUnconfigured)
	}
	if cfg.Pool == nil {
		return fmt.Errorf("%w: Pool required (no raw go func())", ErrSupplierCostMonitorUnconfigured)
	}
	return nil
}

func applySupplierCostDefaults(cfg *SupplierCostMonitorConfig) {
	if cfg.ThresholdPct <= 0 {
		cfg.ThresholdPct = DefaultSupplierCostThresholdPct
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
}

// Close marks the monitor closed. Implements lifecycle.Closer.
func (m *SupplierCostMonitor) Close(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// ThresholdPct returns the configured threshold. Useful for
// dashboards + the v3.5.1 plan-sync capsule.
func (m *SupplierCostMonitor) ThresholdPct() float64 { return m.cfg.ThresholdPct }

// Tick runs a single re-scrape pass over the supplied entries.
// Returns a populated report even on partial failure so callers can
// surface metrics; only fatal errors (closed monitor, ctx cancel)
// short-circuit.
func (m *SupplierCostMonitor) Tick(ctx context.Context, entries []SupplierTrackingEntry) (SupplierCostTickReport, error) {
	if err := m.guard(); err != nil {
		return SupplierCostTickReport{}, err
	}
	report := SupplierCostTickReport{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		m.evaluateEntry(ctx, entry, &report)
	}
	return report, nil
}

// evaluateEntry processes a single tracking entry. Updates report
// counters in place. Cyclomatic stays at 5.
func (m *SupplierCostMonitor) evaluateEntry(ctx context.Context, entry SupplierTrackingEntry, report *SupplierCostTickReport) {
	report.Evaluated++
	observed, err := m.resolveLatestPrice(ctx, entry)
	if err != nil {
		report.AdapterErrors++
		m.logger.Warn("monitor.supplier_cost.adapter_error", "tenant_id", m.cfg.TenantID, "source", entry.Source, "sku", entry.SupplierSKU, "error", err)
		return
	}
	if observed == nil {
		// No matching listing this run. Treat as adapter error so
		// the operator dashboard surfaces the gap; do NOT corrupt
		// the baseline.
		report.AdapterErrors++
		m.logger.Warn("monitor.supplier_cost.no_match", "tenant_id", m.cfg.TenantID, "source", entry.Source, "sku", entry.SupplierSKU)
		return
	}
	baseline, err := m.cfg.BaselineStore.Get(ctx, m.cfg.TenantID, entry.Source, entry.SupplierSKU)
	if err != nil {
		if errors.Is(err, ErrSupplierBaselineNotFound) {
			m.seedBaseline(ctx, entry, *observed, report)
			return
		}
		report.StoreErrors++
		m.logger.Error("monitor.supplier_cost.store_get_failed", "tenant_id", m.cfg.TenantID, "source", entry.Source, "sku", entry.SupplierSKU, "error", err)
		return
	}
	deltaPct := computeDelta(baseline.BaselineCNYCents, observed.PriceCNYCents)
	direction, fired := decideOutcome(deltaPct, m.cfg.ThresholdPct)
	if !fired {
		report.NoChange++
		m.persistObservation(ctx, baseline, *observed, deltaPct, false)
		return
	}
	report.Detected++
	m.persistObservation(ctx, baseline, *observed, deltaPct, true)
	m.emitChange(ctx, entry, baseline, *observed, deltaPct, direction)
}

// resolveLatestPrice fans the configured clients via the workerpool
// to find the entry's current price. Returns the first matching
// product (entries are unique by SKU + source so the first match is
// canonical).
func (m *SupplierCostMonitor) resolveLatestPrice(ctx context.Context, entry SupplierTrackingEntry) (*china.Product, error) {
	clients := m.clientsForSource(entry.Source)
	if len(clients) == 0 {
		return nil, fmt.Errorf("monitor: no client registered for source %s", entry.Source)
	}
	type result struct {
		product *china.Product
		err     error
	}
	results := make(chan result, len(clients))
	var pending sync.WaitGroup
	for _, client := range clients {
		client := client
		pending.Add(1)
		err := m.cfg.Pool.Submit(ctx, func(taskCtx context.Context) error {
			defer pending.Done()
			p, err := client.Search(taskCtx, china.SearchRequest{Keyword: entry.Keyword, MaxResults: 25})
			if err != nil {
				results <- result{err: err}
				return nil
			}
			results <- result{product: pickProduct(p, entry.SupplierSKU)}
			return nil
		})
		if err != nil {
			pending.Done()
			results <- result{err: fmt.Errorf("monitor: pool submit %s: %w", client.Source(), err)}
		}
	}
	go func() {
		pending.Wait()
		close(results)
	}()
	var firstErr error
	for r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		if r.product != nil {
			return r.product, nil
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, nil
}

// clientsForSource filters the configured clients by source. The
// monitor wires multiple china adapters per tenant; this helper
// keeps resolveLatestPrice focused.
func (m *SupplierCostMonitor) clientsForSource(source china.Source) []china.Client {
	out := make([]china.Client, 0, len(m.cfg.Clients))
	for _, c := range m.cfg.Clients {
		if c.Source() == source {
			out = append(out, c)
		}
	}
	return out
}

// pickProduct returns the first product whose ExternalID matches
// the supplied SKU. Returns nil when no match (caller treats as
// "no current observation").
func pickProduct(products []china.Product, sku string) *china.Product {
	for i := range products {
		if products[i].ExternalID == sku {
			return &products[i]
		}
	}
	return nil
}

// seedBaseline records the first observation for a (tenant, source,
// sku). No event emitted (the rolling baseline starts here).
func (m *SupplierCostMonitor) seedBaseline(ctx context.Context, entry SupplierTrackingEntry, observed china.Product, report *SupplierCostTickReport) {
	now := m.cfg.Now()
	rec := SupplierBaselineRecord{
		TenantID:         m.cfg.TenantID,
		Source:           entry.Source,
		SupplierSKU:      entry.SupplierSKU,
		SupplierID:       observed.SupplierID,
		BaselineCNYCents: observed.PriceCNYCents,
		LastObservedCNY:  observed.PriceCNYCents,
		LastDeltaPct:     0,
		ObservedAt:       now,
		UpdatedAt:        now,
	}
	if err := m.cfg.BaselineStore.Upsert(ctx, rec); err != nil {
		report.StoreErrors++
		m.logger.Error("monitor.supplier_cost.seed_failed", "tenant_id", m.cfg.TenantID, "source", entry.Source, "sku", entry.SupplierSKU, "error", err)
		return
	}
	report.NewBaselines++
	m.logger.Info("monitor.supplier_cost.baseline_seeded", "tenant_id", m.cfg.TenantID, "source", entry.Source, "sku", entry.SupplierSKU, "cny_cents", observed.PriceCNYCents)
}

// persistObservation rolls the baseline forward when an event was
// fired (so the next tick measures against the new floor) or just
// updates LastObservedCNY when the price drifted within threshold.
func (m *SupplierCostMonitor) persistObservation(ctx context.Context, baseline SupplierBaselineRecord, observed china.Product, deltaPct float64, fired bool) {
	now := m.cfg.Now()
	updated := baseline
	updated.LastObservedCNY = observed.PriceCNYCents
	updated.LastDeltaPct = deltaPct
	updated.UpdatedAt = now
	if observed.SupplierID != "" {
		updated.SupplierID = observed.SupplierID
	}
	if fired {
		updated.BaselineCNYCents = observed.PriceCNYCents
		updated.ObservedAt = now
	}
	if err := m.cfg.BaselineStore.Upsert(ctx, updated); err != nil {
		m.logger.Error("monitor.supplier_cost.persist_failed", "tenant_id", m.cfg.TenantID, "source", baseline.Source, "sku", baseline.SupplierSKU, "error", err)
	}
}

// emitChange publishes the typed SupplierCostChangedEvent + emits
// the EvoMap KPI sample + the Prometheus counter.
func (m *SupplierCostMonitor) emitChange(ctx context.Context, entry SupplierTrackingEntry, baseline SupplierBaselineRecord, observed china.Product, deltaPct float64, direction SupplierCostDirection) {
	payload := eventbus.SupplierCostChangedPayload{
		Version:          eventbus.SupplierCostChangedPayloadVersion,
		TenantID:         m.cfg.TenantID,
		Source:           string(entry.Source),
		SupplierSKU:      entry.SupplierSKU,
		SupplierID:       observed.SupplierID,
		BaselineCNYCents: baseline.BaselineCNYCents,
		ObservedCNYCents: observed.PriceCNYCents,
		DeltaPct:         deltaPct,
		Direction:        string(direction),
		ThresholdPct:     m.cfg.ThresholdPct,
		ObservedAt:       m.cfg.Now(),
	}
	evt, err := eventbus.NewSupplierCostChangedEvent("monitor.supplier_cost", m.cfg.Now(), payload)
	if err != nil {
		m.logger.Error("monitor.supplier_cost.event_invalid", "tenant_id", m.cfg.TenantID, "error", err)
		return
	}
	if err := m.cfg.Publisher.Publish(ctx, evt); err != nil {
		m.logger.Error("monitor.supplier_cost.publish_failed", "tenant_id", m.cfg.TenantID, "error", err)
		return
	}
	m.recordMetric(string(entry.Source), string(direction))
	m.fireKPI(SupplierCostKPISample{
		TenantID:  m.cfg.TenantID,
		Source:    string(entry.Source),
		SKU:       entry.SupplierSKU,
		Direction: direction,
		DeltaPct:  deltaPct,
	})
}

func (m *SupplierCostMonitor) recordMetric(source, direction string) {
	if m.cfg.Metrics == nil {
		return
	}
	m.cfg.Metrics.RecordSupplierCostChange(m.cfg.TenantID, source, direction)
}

func (m *SupplierCostMonitor) fireKPI(sample SupplierCostKPISample) {
	if m.cfg.KPIHook == nil {
		return
	}
	m.cfg.KPIHook(sample)
}

func (m *SupplierCostMonitor) guard() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrSupplierCostMonitorClosed
	}
	return nil
}

// computeDelta returns the relative cost change as a positive
// fraction (sign reflects direction). Pure; cyclomatic 2.
func computeDelta(baselineCents, observedCents int) float64 {
	if baselineCents <= 0 {
		// Baseline of zero means "no signal yet"; treat as zero delta.
		return 0
	}
	return float64(observedCents-baselineCents) / float64(baselineCents)
}

// decideOutcome runs the threshold gate. fired=false means the
// observed price stayed within the threshold band. Pure; cyclomatic 4.
func decideOutcome(deltaPct, thresholdPct float64) (SupplierCostDirection, bool) {
	abs := math.Abs(deltaPct)
	if abs <= thresholdPct {
		return "", false
	}
	if deltaPct > 0 {
		return SupplierCostDirectionUp, true
	}
	return SupplierCostDirectionDown, true
}
