//go:build v351_smoke

// File scope: v3.5.1 QA Task 3 -- supplier cost-change event
// validation (EC-6-1 hardening).
//
// Acceptance per the v3.5.1 plan + EC-6-1 spec:
//  1. >5% increase  -> SupplierCostChangedEvent direction="up"
//  2. >5% decrease  -> SupplierCostChangedEvent direction="down"
//  3. Exactly 5% change (boundary; default threshold)
//     -> NO event (the v3.5.0 shipped behaviour gates strictly
//     above the threshold; this test pins the contract for
//     the v3.9.0 EC-6-5 margin dashboard)
//  4. <5% noise     -> NO event
//  5. Configurable threshold (10%): 7% delta = no event;
//     12% delta = event
//  6. Multi-SKU batch: 5 of 10 cross threshold -> 5 events emitted
//
// Each scenario asserts:
//   - Event payload (tenant_id, sku, source, baseline_cny_cents,
//     observed_cny_cents, delta_pct, direction, threshold_pct,
//     observed_at) matches expected.
//   - The ec_supplier_cost_changes_total Prometheus counter
//     increments with the right (tenant, source, direction) labels.
//
// The smoke wires the production composition shape:
//
//	monitor.SupplierCostMonitor (real)
//	  + memoryBaselineStore (test fake; Postgres seed)
//	  + workerpool.Pool (real)
//	  + recordingPublisher (test fake)
//	  + observability.V350Metrics + metrics.Registry (real)
//
// Cite skill: go-clean-architecture (port + adapter; the test
// wires the in-memory baseline store + a deterministic china
// adapter so the monitor focuses on contract validation).
package v351

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/china"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/lifecycle"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/monitor"
	"github.com/nfsarch33/agentic-ecommerce/internal/observability"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// fixedV351Now is the canonical observation timestamp used across
// every scenario so the event payload assertions stay stable.
var fixedV351Now = time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)

// fakeChinaCostClient is the smoke-test adapter for monitor's
// china.Client port. It returns one product per keyword keyed by
// the supplier SKU. Goroutine-safe.
type fakeChinaCostClient struct {
	source china.Source

	mu       sync.Mutex
	products map[string][]china.Product
}

func (f *fakeChinaCostClient) Source() china.Source { return f.source }

func (f *fakeChinaCostClient) Search(_ context.Context, req china.SearchRequest) ([]china.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if got, ok := f.products[req.Keyword]; ok {
		return append([]china.Product(nil), got...), nil
	}
	return nil, nil
}

func (f *fakeChinaCostClient) ProductDetail(_ context.Context, _ china.ProductDetailRequest) (china.Product, error) {
	return china.Product{}, fmt.Errorf("not used by EC-6-1 monitor")
}

func (f *fakeChinaCostClient) Close(_ context.Context) error { return nil }

// memoryCostBaselineStore is a tiny in-memory monitor.SupplierBaselineStore.
// Goroutine-safe so the workerpool fan-out stays race-clean.
type memoryCostBaselineStore struct {
	mu   sync.Mutex
	rows map[string]monitor.SupplierBaselineRecord
}

func newMemoryCostBaselineStore() *memoryCostBaselineStore {
	return &memoryCostBaselineStore{rows: map[string]monitor.SupplierBaselineRecord{}}
}

func (s *memoryCostBaselineStore) key(tenantID string, source china.Source, sku string) string {
	return tenantID + "\x00" + string(source) + "\x00" + sku
}

func (s *memoryCostBaselineStore) Get(_ context.Context, tenantID string, source china.Source, sku string) (monitor.SupplierBaselineRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.rows[s.key(tenantID, source, sku)]
	if !ok {
		return monitor.SupplierBaselineRecord{}, monitor.ErrSupplierBaselineNotFound
	}
	return rec, nil
}

func (s *memoryCostBaselineStore) Upsert(_ context.Context, rec monitor.SupplierBaselineRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[s.key(rec.TenantID, rec.Source, rec.SupplierSKU)] = rec
	return nil
}

// recordingCostPublisher captures every event the monitor publishes
// so per-scenario assertions can verify direction + payload.
type recordingCostPublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func newRecordingCostPublisher() *recordingCostPublisher { return &recordingCostPublisher{} }

func (p *recordingCostPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *recordingCostPublisher) Close() error { return nil }

func (p *recordingCostPublisher) snapshot() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.events))
	copy(out, p.events)
	return out
}

// costMonitorHarness bundles every wired component. Returned by
// setupCostMonitorHarness so each scenario stays focused on
// scenario-specific configuration.
type costMonitorHarness struct {
	monitor   *monitor.SupplierCostMonitor
	publisher *recordingCostPublisher
	store     *memoryCostBaselineStore
	pool      *workerpool.Pool
	registry  *metrics.Registry
	v350      *observability.V350Metrics
	manager   *lifecycle.Manager
	tenantID  string
}

// setupCostMonitorHarness wires the monitor against the supplied
// products + threshold. Default threshold is the EC-6-1 5% baseline.
func setupCostMonitorHarness(t *testing.T, products map[string][]china.Product, baselines []monitor.SupplierBaselineRecord, thresholdPct float64) *costMonitorHarness {
	t.Helper()
	const tenantID = "tenant-v351"
	manager := lifecycle.New(nil, 5*time.Second)
	t.Cleanup(func() {
		if err := manager.Shutdown(); err != nil {
			t.Errorf("lifecycle.Manager.Shutdown: %v", err)
		}
	})
	pool := workerpool.New(nil, workerpool.Config{Name: "v351-cost-monitor", MinWorkers: 2, MaxWorkers: 4, QueueDepth: 32})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	store := newMemoryCostBaselineStore()
	for _, base := range baselines {
		if err := store.Upsert(context.Background(), base); err != nil {
			t.Fatalf("seed baseline: %v", err)
		}
	}
	publisher := newRecordingCostPublisher()
	client := &fakeChinaCostClient{source: china.Source1688, products: products}
	registry := metrics.NewRegistry("v351-smoke")
	v350 := observability.NewV350Metrics(registry)
	mon, err := monitor.NewSupplierCostMonitor(nil, monitor.SupplierCostMonitorConfig{
		TenantID:      tenantID,
		Clients:       []china.Client{client},
		BaselineStore: store,
		Publisher:     publisher,
		Pool:          pool,
		ThresholdPct:  thresholdPct,
		Metrics:       v350,
		Now:           func() time.Time { return fixedV351Now },
	})
	if err != nil {
		t.Fatalf("NewSupplierCostMonitor: %v", err)
	}
	t.Cleanup(func() { _ = mon.Close(context.Background()) })
	return &costMonitorHarness{
		monitor:   mon,
		publisher: publisher,
		store:     store,
		pool:      pool,
		registry:  registry,
		v350:      v350,
		manager:   manager,
		tenantID:  tenantID,
	}
}

// baselineFor builds a baseline record for the given SKU + cost.
// Pulled out so the per-scenario fixtures stay one-line.
func baselineFor(sku string, baselineCNYCents int) monitor.SupplierBaselineRecord {
	return monitor.SupplierBaselineRecord{
		TenantID:         "tenant-v351",
		Source:           china.Source1688,
		SupplierSKU:      sku,
		SupplierID:       "supplier-v351",
		BaselineCNYCents: baselineCNYCents,
		LastObservedCNY:  baselineCNYCents,
		ObservedAt:       fixedV351Now.Add(-24 * time.Hour),
		UpdatedAt:        fixedV351Now.Add(-24 * time.Hour),
	}
}

// productFor returns the canonical china.Product fixture for the
// given keyword + observed CNY price.
func productFor(sku string, observedCNYCents int) china.Product {
	return china.Product{
		ExternalID:    sku,
		Source:        china.Source1688,
		Title:         "Cost Monitor Fixture " + sku,
		PriceCNYCents: observedCNYCents,
		SupplierID:    "supplier-v351",
	}
}

// trackingFor builds the tracking entry for the given sku/keyword.
func trackingFor(sku, keyword string) monitor.SupplierTrackingEntry {
	return monitor.SupplierTrackingEntry{
		Keyword:     keyword,
		Source:      china.Source1688,
		SupplierSKU: sku,
	}
}

// TestSupplierCostEventValidation_FullScenarioMatrix walks every
// v3.5.1 acceptance scenario as a table-driven sub-test. Each
// scenario asserts both the event payload + the Prometheus
// counter labels.
func TestSupplierCostEventValidation_FullScenarioMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		baselineCNYCents int
		observedCNYCents int
		thresholdPct     float64
		wantEmit         bool
		wantDirection    string
	}{
		// Scenario 1: > 5% increase fires direction=up.
		{name: "1_above_5pct_increase_fires_up", baselineCNYCents: 1000, observedCNYCents: 1100, thresholdPct: 0.05, wantEmit: true, wantDirection: "up"},
		// Scenario 2: > 5% decrease fires direction=down.
		{name: "2_above_5pct_decrease_fires_down", baselineCNYCents: 1000, observedCNYCents: 900, thresholdPct: 0.05, wantEmit: true, wantDirection: "down"},
		// Scenario 3: exactly 5% delta is at the boundary; the
		// v3.5.0 shipped contract gates STRICTLY above (delta_pct
		// > threshold_pct), so this case emits NO event. Documents
		// the production behaviour for the v3.9.0 EC-6-5 margin
		// dashboard.
		{name: "3_exactly_threshold_no_emit", baselineCNYCents: 1000, observedCNYCents: 1050, thresholdPct: 0.05, wantEmit: false, wantDirection: ""},
		// Scenario 4: sub-threshold noise produces no event.
		{name: "4_sub_threshold_noise_no_emit", baselineCNYCents: 1000, observedCNYCents: 1030, thresholdPct: 0.05, wantEmit: false, wantDirection: ""},
		// Scenario 5a: configurable 10% threshold; 7% delta = no event.
		{name: "5a_threshold_10pct_below_no_emit", baselineCNYCents: 1000, observedCNYCents: 1070, thresholdPct: 0.10, wantEmit: false, wantDirection: ""},
		// Scenario 5b: configurable 10% threshold; 12% delta fires.
		{name: "5b_threshold_10pct_above_fires", baselineCNYCents: 1000, observedCNYCents: 1120, thresholdPct: 0.10, wantEmit: true, wantDirection: "up"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runCostScenario(t, tc.name, tc.baselineCNYCents, tc.observedCNYCents, tc.thresholdPct, tc.wantEmit, tc.wantDirection)
		})
	}
}

// runCostScenario drives one single-SKU scenario through the
// monitor + asserts the event + metric outcome. Pulled out so the
// table-driven loop body stays cyclomatic 3.
func runCostScenario(t *testing.T, name string, baselineCents, observedCents int, thresholdPct float64, wantEmit bool, wantDirection string) {
	t.Helper()
	const sku = "sku-cost-1"
	const keyword = "fixture-keyword"
	products := map[string][]china.Product{keyword: {productFor(sku, observedCents)}}
	baselines := []monitor.SupplierBaselineRecord{baselineFor(sku, baselineCents)}
	h := setupCostMonitorHarness(t, products, baselines, thresholdPct)
	report, err := h.monitor.Tick(context.Background(), []monitor.SupplierTrackingEntry{trackingFor(sku, keyword)})
	if err != nil {
		t.Fatalf("%s: Tick: %v", name, err)
	}
	events := h.publisher.snapshot()
	if wantEmit {
		assertSingleCostEvent(t, name, events, "tenant-v351", sku, baselineCents, observedCents, thresholdPct, wantDirection)
		assertSupplierCostMetric(t, h.registry, "tenant-v351", "1688", wantDirection, 1)
		if report.Detected != 1 {
			t.Fatalf("%s: report.Detected = %d, want 1", name, report.Detected)
		}
		return
	}
	if len(events) != 0 {
		t.Fatalf("%s: events = %d, want 0 (sub-threshold; %d delta vs %.4f threshold)", name, len(events), observedCents-baselineCents, thresholdPct)
	}
	if report.Detected != 0 {
		t.Fatalf("%s: report.Detected = %d, want 0", name, report.Detected)
	}
}

// TestSupplierCostEventValidation_BatchMultiSKU drives Tick over
// 10 SKUs where 5 cross the threshold and 5 stay within. Asserts
// the per-SKU event isolation + the cumulative Prometheus counter.
func TestSupplierCostEventValidation_BatchMultiSKU(t *testing.T) {
	t.Parallel()
	const tenantID = "tenant-v351"
	products := map[string][]china.Product{}
	baselines := make([]monitor.SupplierBaselineRecord, 0, 10)
	entries := make([]monitor.SupplierTrackingEntry, 0, 10)
	for i := 0; i < 10; i++ {
		sku := fmt.Sprintf("sku-batch-%02d", i)
		keyword := fmt.Sprintf("kw-batch-%02d", i)
		baseline := 1000
		observed := baseline + 30 // sub-threshold (3%)
		if i < 5 {
			observed = baseline + 100 // 10%, above the 5% threshold
		}
		products[keyword] = []china.Product{productFor(sku, observed)}
		baselines = append(baselines, baselineFor(sku, baseline))
		entries = append(entries, trackingFor(sku, keyword))
	}
	h := setupCostMonitorHarness(t, products, baselines, 0.05)
	report, err := h.monitor.Tick(context.Background(), entries)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if report.Detected != 5 {
		t.Fatalf("report.Detected = %d, want 5 (5/10 SKUs cross threshold)", report.Detected)
	}
	if report.NoChange != 5 {
		t.Fatalf("report.NoChange = %d, want 5", report.NoChange)
	}
	events := h.publisher.snapshot()
	if len(events) != 5 {
		t.Fatalf("events = %d, want 5", len(events))
	}
	skuOrder := []string{}
	for _, evt := range events {
		sku, _ := evt.Payload["supplier_sku"].(string)
		skuOrder = append(skuOrder, sku)
	}
	sort.Strings(skuOrder)
	for i, sku := range skuOrder {
		want := fmt.Sprintf("sku-batch-%02d", i)
		if sku != want {
			t.Fatalf("event[%d] sku = %s, want %s (5 cross-threshold SKUs)", i, sku, want)
		}
	}
	assertSupplierCostMetric(t, h.registry, tenantID, "1688", "up", 5)
	t.Logf("v3.5.1 cost-change batch validation: detected=%d no_change=%d events=%d", report.Detected, report.NoChange, len(events))
}

// assertSingleCostEvent verifies the captured event sequence
// contains exactly one SupplierCostChangedEvent matching the
// expected payload + Prometheus contract.
func assertSingleCostEvent(t *testing.T, name string, events []eventbus.Event, tenantID, sku string, baselineCents, observedCents int, thresholdPct float64, wantDirection string) {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("%s: events = %d (types=%s), want 1", name, len(events), eventTypesString(events))
	}
	evt := events[0]
	if evt.Type != eventbus.SupplierCostChanged {
		t.Fatalf("%s: event type = %s, want %s", name, evt.Type, eventbus.SupplierCostChanged)
	}
	if evt.TenantID != tenantID {
		t.Fatalf("%s: event tenant = %s, want %s", name, evt.TenantID, tenantID)
	}
	gotSKU, _ := evt.Payload["supplier_sku"].(string)
	if gotSKU != sku {
		t.Fatalf("%s: payload supplier_sku = %s, want %s", name, gotSKU, sku)
	}
	gotSource, _ := evt.Payload["source"].(string)
	if gotSource != "1688" {
		t.Fatalf("%s: payload source = %s, want 1688", name, gotSource)
	}
	gotDirection, _ := evt.Payload["direction"].(string)
	if gotDirection != wantDirection {
		t.Fatalf("%s: payload direction = %s, want %s", name, gotDirection, wantDirection)
	}
	if got, _ := evt.Payload["baseline_cny_cents"].(int); got != baselineCents {
		t.Fatalf("%s: payload baseline = %d, want %d", name, got, baselineCents)
	}
	if got, _ := evt.Payload["observed_cny_cents"].(int); got != observedCents {
		t.Fatalf("%s: payload observed = %d, want %d", name, got, observedCents)
	}
	if got, _ := evt.Payload["threshold_pct"].(float64); got != thresholdPct {
		t.Fatalf("%s: payload threshold_pct = %.4f, want %.4f", name, got, thresholdPct)
	}
	if _, ok := evt.Payload["delta_pct"]; !ok {
		t.Fatalf("%s: payload missing delta_pct", name)
	}
	if _, ok := evt.Payload["observed_at"]; !ok {
		t.Fatalf("%s: payload missing observed_at", name)
	}
	t.Logf("%s -- payload OK: tenant=%s sku=%s source=%s baseline=%d observed=%d direction=%s threshold=%.4f", name, tenantID, sku, gotSource, baselineCents, observedCents, gotDirection, thresholdPct)
}

// assertSupplierCostMetric verifies the
// ec_supplier_cost_changes_total counter incremented with the
// expected (tenant, source, direction) labels + delta. Mirrors the
// dropship metric assertion shape so PR reviewers see consistent
// gating across the v3.5.1 smoke.
func assertSupplierCostMetric(t *testing.T, registry *metrics.Registry, tenantID, source, direction string, want int) {
	t.Helper()
	exposition := scrapeRegistry(t, registry)
	needle := fmt.Sprintf(`ec_supplier_cost_changes_total{binary="v351-smoke",direction=%q,source=%q,tenant_id=%q} %d`, direction, source, tenantID, want)
	if !strings.Contains(exposition, needle) {
		t.Fatalf("metric not found:\nwant: %s\nfull exposition:\n%s", needle, exposition)
	}
}
