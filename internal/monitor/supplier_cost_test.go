// File scope: v3.5.0 EC-6-1 supplier cost monitor RED tests.
// TDD-first per the v3.5.0 plan (story 2; depends on EC-6-2 fee
// calculator + the v3.1.0 EC-1-1/1-2 China adapters).
//
// Acceptance per ADR-028 EC-6-1:
//
//   - Detects price deltas > 5% (configurable) against stored baseline.
//   - Emits SupplierCostChangedEvent on the eventbus.
//   - Stable price within threshold produces no event.
//   - 5% default threshold; per-tenant override works.
//
// The monitor consumes a small port abstraction over the v3.1.0
// china.Client adapters so tests can wire deterministic fakes.
package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/china"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// fakeChinaClient is a minimal china.Client double for the
// supplier cost tests. Only Search + Source are exercised; detail
// fetches are not used by the EC-6-1 monitor.
type fakeChinaClient struct {
	source   china.Source
	products map[string][]china.Product
	err      error

	mu    sync.Mutex
	calls int
}

func (f *fakeChinaClient) Source() china.Source { return f.source }

func (f *fakeChinaClient) Search(_ context.Context, req china.SearchRequest) ([]china.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if products, ok := f.products[req.Keyword]; ok {
		return append([]china.Product(nil), products...), nil
	}
	return nil, nil
}

func (f *fakeChinaClient) ProductDetail(_ context.Context, _ china.ProductDetailRequest) (china.Product, error) {
	return china.Product{}, errors.New("not implemented")
}

func (f *fakeChinaClient) Close(_ context.Context) error { return nil }

func (f *fakeChinaClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// recordingPublisher captures every SupplierCostChanged event the
// monitor publishes. Mirrors the v3.3.0 channel test helper so the
// shape is consistent across the repo.
type recordingPublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (p *recordingPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *recordingPublisher) Close() error { return nil }

func (p *recordingPublisher) snapshot() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.events))
	copy(out, p.events)
	return out
}

// memoryBaselineStore is the in-memory SupplierBaselineStore used
// by tests. Goroutine-safe.
type memoryBaselineStore struct {
	mu   sync.Mutex
	rows map[string]SupplierBaselineRecord
}

func newMemoryBaselineStore() *memoryBaselineStore {
	return &memoryBaselineStore{rows: map[string]SupplierBaselineRecord{}}
}

func (s *memoryBaselineStore) key(tenantID string, source china.Source, sku string) string {
	return tenantID + "\x00" + string(source) + "\x00" + sku
}

func (s *memoryBaselineStore) Get(_ context.Context, tenantID string, source china.Source, sku string) (SupplierBaselineRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.rows[s.key(tenantID, source, sku)]
	if !ok {
		return SupplierBaselineRecord{}, ErrSupplierBaselineNotFound
	}
	return rec, nil
}

func (s *memoryBaselineStore) Upsert(_ context.Context, rec SupplierBaselineRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[s.key(rec.TenantID, rec.Source, rec.SupplierSKU)] = rec
	return nil
}

func newSupplierCostHarness(t *testing.T, products map[string][]china.Product, baselines []SupplierBaselineRecord) (*SupplierCostMonitor, *recordingPublisher, *memoryBaselineStore, *workerpool.Pool) {
	t.Helper()
	pool := workerpool.New(nil, workerpool.Config{Name: "supplier-cost-test", MinWorkers: 1, MaxWorkers: 2, QueueDepth: 4})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })

	client := &fakeChinaClient{source: china.Source1688, products: products}
	publisher := &recordingPublisher{}
	store := newMemoryBaselineStore()
	for _, base := range baselines {
		if err := store.Upsert(context.Background(), base); err != nil {
			t.Fatalf("seed baseline: %v", err)
		}
	}
	monitor, err := NewSupplierCostMonitor(nil, SupplierCostMonitorConfig{
		TenantID:      "tenant-1",
		Clients:       []china.Client{client},
		BaselineStore: store,
		Publisher:     publisher,
		Pool:          pool,
		ThresholdPct:  DefaultSupplierCostThresholdPct,
		Now:           func() time.Time { return time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewSupplierCostMonitor: %v", err)
	}
	t.Cleanup(func() { _ = monitor.Close(context.Background()) })
	return monitor, publisher, store, pool
}

func TestSupplierCostMonitor_DetectsPriceIncreaseAndEmitsEvent(t *testing.T) {
	t.Parallel()
	// Baseline 1000 cents (¥10.00), observed 1100 cents (10% higher).
	products := map[string][]china.Product{
		"wireless earbuds": {{
			ExternalID:    "prod-001",
			Source:        china.Source1688,
			Title:         "Wireless Earbuds Pro",
			PriceCNYCents: 1100,
			SupplierID:    "supplier-A",
		}},
	}
	baselines := []SupplierBaselineRecord{{
		TenantID:         "tenant-1",
		Source:           china.Source1688,
		SupplierSKU:      "prod-001",
		SupplierID:       "supplier-A",
		BaselineCNYCents: 1000,
		LastObservedCNY:  1000,
		ObservedAt:       time.Date(2026, 5, 9, 5, 0, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, 5, 9, 5, 0, 0, 0, time.UTC),
	}}
	monitor, publisher, store, _ := newSupplierCostHarness(t, products, baselines)
	report, err := monitor.Tick(context.Background(), []SupplierTrackingEntry{{Keyword: "wireless earbuds", SupplierSKU: "prod-001", Source: china.Source1688}})
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if report.Detected != 1 {
		t.Fatalf("Tick detected = %d, want 1 (10%% delta vs 5%% threshold)", report.Detected)
	}
	events := publisher.snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Type != eventbus.SupplierCostChanged {
		t.Fatalf("event type = %s, want %s", events[0].Type, eventbus.SupplierCostChanged)
	}
	if events[0].TenantID != "tenant-1" {
		t.Fatalf("event tenant = %s, want tenant-1", events[0].TenantID)
	}
	updated, err := store.Get(context.Background(), "tenant-1", china.Source1688, "prod-001")
	if err != nil {
		t.Fatalf("baseline get after Tick: %v", err)
	}
	if updated.LastObservedCNY != 1100 {
		t.Fatalf("baseline last_observed = %d, want 1100", updated.LastObservedCNY)
	}
	if updated.BaselineCNYCents != 1100 {
		t.Fatalf("baseline updated to %d, want 1100 (rolled forward)", updated.BaselineCNYCents)
	}
}

func TestSupplierCostMonitor_StablePriceProducesNoEvent(t *testing.T) {
	t.Parallel()
	// Baseline 1000, observed 1030 (3% delta; within 5% threshold).
	products := map[string][]china.Product{
		"thermos bottle": {{
			ExternalID:    "prod-002",
			Source:        china.Source1688,
			Title:         "Thermos Bottle 500ml",
			PriceCNYCents: 1030,
			SupplierID:    "supplier-B",
		}},
	}
	baselines := []SupplierBaselineRecord{{
		TenantID:         "tenant-1",
		Source:           china.Source1688,
		SupplierSKU:      "prod-002",
		BaselineCNYCents: 1000,
		LastObservedCNY:  1000,
		ObservedAt:       time.Date(2026, 5, 9, 5, 0, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, 5, 9, 5, 0, 0, 0, time.UTC),
	}}
	monitor, publisher, _, _ := newSupplierCostHarness(t, products, baselines)
	report, err := monitor.Tick(context.Background(), []SupplierTrackingEntry{{Keyword: "thermos bottle", SupplierSKU: "prod-002", Source: china.Source1688}})
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if report.Detected != 0 {
		t.Fatalf("Tick detected = %d, want 0 (3%% delta within 5%% threshold)", report.Detected)
	}
	if events := publisher.snapshot(); len(events) != 0 {
		t.Fatalf("events = %d, want 0 (no event for stable price)", len(events))
	}
}

func TestSupplierCostMonitor_PercentageThresholdConfigurable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		threshold float64
		observed  int
		baseline  int
		wantEmit  bool
	}{
		{name: "default_5pct_at_4pct_no_emit", threshold: 0.05, observed: 1040, baseline: 1000, wantEmit: false},
		{name: "default_5pct_at_5pct_no_emit", threshold: 0.05, observed: 1050, baseline: 1000, wantEmit: false},
		{name: "default_5pct_at_5_1pct_emit", threshold: 0.05, observed: 1051, baseline: 1000, wantEmit: true},
		{name: "override_10pct_at_8pct_no_emit", threshold: 0.10, observed: 1080, baseline: 1000, wantEmit: false},
		{name: "override_10pct_at_15pct_emit", threshold: 0.10, observed: 1150, baseline: 1000, wantEmit: true},
		{name: "decrease_above_threshold_emits", threshold: 0.05, observed: 900, baseline: 1000, wantEmit: true},
		{name: "decrease_below_threshold_no_emit", threshold: 0.05, observed: 980, baseline: 1000, wantEmit: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			products := map[string][]china.Product{
				"item": {{ExternalID: "p1", Source: china.Source1688, PriceCNYCents: tc.observed, Title: "x"}},
			}
			baselines := []SupplierBaselineRecord{{
				TenantID:         "tenant-1",
				Source:           china.Source1688,
				SupplierSKU:      "p1",
				BaselineCNYCents: tc.baseline,
				LastObservedCNY:  tc.baseline,
				ObservedAt:       time.Date(2026, 5, 9, 5, 0, 0, 0, time.UTC),
				UpdatedAt:        time.Date(2026, 5, 9, 5, 0, 0, 0, time.UTC),
			}}
			pool := workerpool.New(nil, workerpool.Config{Name: "supplier-cost-test-th", MinWorkers: 1, MaxWorkers: 2, QueueDepth: 4})
			t.Cleanup(func() { _ = pool.Close(context.Background()) })
			client := &fakeChinaClient{source: china.Source1688, products: products}
			pub := &recordingPublisher{}
			store := newMemoryBaselineStore()
			for _, b := range baselines {
				_ = store.Upsert(context.Background(), b)
			}
			monitor, err := NewSupplierCostMonitor(nil, SupplierCostMonitorConfig{
				TenantID:      "tenant-1",
				Clients:       []china.Client{client},
				BaselineStore: store,
				Publisher:     pub,
				Pool:          pool,
				ThresholdPct:  tc.threshold,
				Now:           func() time.Time { return time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC) },
			})
			if err != nil {
				t.Fatalf("NewSupplierCostMonitor: %v", err)
			}
			t.Cleanup(func() { _ = monitor.Close(context.Background()) })
			_, err = monitor.Tick(context.Background(), []SupplierTrackingEntry{{Keyword: "item", SupplierSKU: "p1", Source: china.Source1688}})
			if err != nil {
				t.Fatalf("Tick: %v", err)
			}
			gotEmit := len(pub.snapshot()) > 0
			if gotEmit != tc.wantEmit {
				t.Fatalf("%s: emitted=%v, want %v (threshold=%.2f observed=%d baseline=%d)", tc.name, gotEmit, tc.wantEmit, tc.threshold, tc.observed, tc.baseline)
			}
		})
	}
}

func TestSupplierCostMonitor_FirstObservationCreatesBaselineNoEvent(t *testing.T) {
	t.Parallel()
	products := map[string][]china.Product{
		"new product": {{ExternalID: "new-1", Source: china.Source1688, PriceCNYCents: 5000, Title: "new"}},
	}
	monitor, publisher, store, _ := newSupplierCostHarness(t, products, nil)
	_, err := monitor.Tick(context.Background(), []SupplierTrackingEntry{{Keyword: "new product", SupplierSKU: "new-1", Source: china.Source1688}})
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if events := publisher.snapshot(); len(events) != 0 {
		t.Fatalf("events = %d, want 0 (first observation should NOT emit)", len(events))
	}
	rec, err := store.Get(context.Background(), "tenant-1", china.Source1688, "new-1")
	if err != nil {
		t.Fatalf("baseline not seeded: %v", err)
	}
	if rec.BaselineCNYCents != 5000 {
		t.Fatalf("baseline = %d, want 5000", rec.BaselineCNYCents)
	}
}

func TestSupplierCostMonitor_KPIHookFires(t *testing.T) {
	t.Parallel()
	products := map[string][]china.Product{
		"item": {{ExternalID: "p1", Source: china.Source1688, PriceCNYCents: 1100, Title: "x"}},
	}
	baselines := []SupplierBaselineRecord{{
		TenantID:         "tenant-1",
		Source:           china.Source1688,
		SupplierSKU:      "p1",
		BaselineCNYCents: 1000,
		LastObservedCNY:  1000,
	}}
	pool := workerpool.New(nil, workerpool.Config{Name: "supplier-cost-test-kpi", MinWorkers: 1, MaxWorkers: 2, QueueDepth: 4})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	store := newMemoryBaselineStore()
	for _, b := range baselines {
		_ = store.Upsert(context.Background(), b)
	}
	pub := &recordingPublisher{}
	var (
		hookMu    sync.Mutex
		hookCalls []SupplierCostKPISample
	)
	monitor, err := NewSupplierCostMonitor(nil, SupplierCostMonitorConfig{
		TenantID:      "tenant-1",
		Clients:       []china.Client{&fakeChinaClient{source: china.Source1688, products: products}},
		BaselineStore: store,
		Publisher:     pub,
		Pool:          pool,
		ThresholdPct:  0.05,
		KPIHook: func(s SupplierCostKPISample) {
			hookMu.Lock()
			defer hookMu.Unlock()
			hookCalls = append(hookCalls, s)
		},
	})
	if err != nil {
		t.Fatalf("NewSupplierCostMonitor: %v", err)
	}
	t.Cleanup(func() { _ = monitor.Close(context.Background()) })
	_, err = monitor.Tick(context.Background(), []SupplierTrackingEntry{{Keyword: "item", SupplierSKU: "p1", Source: china.Source1688}})
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	hookMu.Lock()
	defer hookMu.Unlock()
	if len(hookCalls) == 0 {
		t.Fatalf("KPI hook never fired")
	}
	if hookCalls[0].Direction != SupplierCostDirectionUp {
		t.Fatalf("KPI direction = %s, want up", hookCalls[0].Direction)
	}
}

func TestSupplierCostMonitor_RejectsNilDeps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  SupplierCostMonitorConfig
	}{
		{name: "no_tenant", cfg: SupplierCostMonitorConfig{
			Clients:       []china.Client{&fakeChinaClient{source: china.Source1688}},
			BaselineStore: newMemoryBaselineStore(),
			Publisher:     &recordingPublisher{},
			Pool:          workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1}),
		}},
		{name: "no_clients", cfg: SupplierCostMonitorConfig{
			TenantID:      "tenant-1",
			BaselineStore: newMemoryBaselineStore(),
			Publisher:     &recordingPublisher{},
			Pool:          workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1}),
		}},
		{name: "no_store", cfg: SupplierCostMonitorConfig{
			TenantID:  "tenant-1",
			Clients:   []china.Client{&fakeChinaClient{source: china.Source1688}},
			Publisher: &recordingPublisher{},
			Pool:      workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1}),
		}},
		{name: "no_publisher", cfg: SupplierCostMonitorConfig{
			TenantID:      "tenant-1",
			Clients:       []china.Client{&fakeChinaClient{source: china.Source1688}},
			BaselineStore: newMemoryBaselineStore(),
			Pool:          workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1}),
		}},
		{name: "no_pool", cfg: SupplierCostMonitorConfig{
			TenantID:      "tenant-1",
			Clients:       []china.Client{&fakeChinaClient{source: china.Source1688}},
			BaselineStore: newMemoryBaselineStore(),
			Publisher:     &recordingPublisher{},
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if tc.cfg.Pool != nil {
					_ = tc.cfg.Pool.Close(context.Background())
				}
			}()
			_, err := NewSupplierCostMonitor(nil, tc.cfg)
			if !errors.Is(err, ErrSupplierCostMonitorUnconfigured) {
				t.Fatalf("%s: err=%v, want ErrSupplierCostMonitorUnconfigured", tc.name, err)
			}
		})
	}
}

func TestSupplierCostMonitor_TickAfterCloseRejects(t *testing.T) {
	t.Parallel()
	monitor, _, _, _ := newSupplierCostHarness(t, nil, nil)
	if err := monitor.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := monitor.Tick(context.Background(), nil)
	if !errors.Is(err, ErrSupplierCostMonitorClosed) {
		t.Fatalf("Tick after Close: err=%v, want ErrSupplierCostMonitorClosed", err)
	}
}

func TestSupplierCostMonitor_AdapterErrorIsLoggedNotFatal(t *testing.T) {
	t.Parallel()
	pool := workerpool.New(nil, workerpool.Config{Name: "supplier-cost-err", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 2})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	pub := &recordingPublisher{}
	store := newMemoryBaselineStore()
	client := &fakeChinaClient{source: china.Source1688, err: errors.New("fake transport down")}
	monitor, err := NewSupplierCostMonitor(nil, SupplierCostMonitorConfig{
		TenantID:      "tenant-1",
		Clients:       []china.Client{client},
		BaselineStore: store,
		Publisher:     pub,
		Pool:          pool,
		ThresholdPct:  0.05,
	})
	if err != nil {
		t.Fatalf("NewSupplierCostMonitor: %v", err)
	}
	t.Cleanup(func() { _ = monitor.Close(context.Background()) })
	report, err := monitor.Tick(context.Background(), []SupplierTrackingEntry{{Keyword: "x", SupplierSKU: "p", Source: china.Source1688}})
	if err != nil {
		t.Fatalf("Tick should not fatally error on adapter failure: %v", err)
	}
	if report.AdapterErrors != 1 {
		t.Fatalf("AdapterErrors = %d, want 1", report.AdapterErrors)
	}
}
