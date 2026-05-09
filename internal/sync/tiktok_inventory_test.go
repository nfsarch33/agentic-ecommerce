package sync

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingAdjuster struct {
	mu       sync.Mutex
	calls    []StockAdjustRequest
	errs     []error
	cursor   int
	failOnce error
}

func (r *recordingAdjuster) Adjust(_ context.Context, req StockAdjustRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	if r.failOnce != nil {
		err := r.failOnce
		r.failOnce = nil
		return err
	}
	if r.cursor < len(r.errs) {
		err := r.errs[r.cursor]
		r.cursor++
		return err
	}
	return nil
}

func (r *recordingAdjuster) totalDelta() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	sum := 0
	for _, c := range r.calls {
		sum += c.Delta
	}
	return sum
}

func (r *recordingAdjuster) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

type recordingInventoryMetrics struct {
	mu      sync.Mutex
	entries []string
}

func (m *recordingInventoryMetrics) RecordInventorySync(_, direction, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, direction+":"+status)
}

func (m *recordingInventoryMetrics) snapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.entries))
	copy(out, m.entries)
	return out
}

func newSyncHarness(t *testing.T) (*TikTokInventorySync, *recordingAdjuster, *recordingAdjuster, *recordingInventoryMetrics) {
	t.Helper()
	wc := &recordingAdjuster{}
	tt := &recordingAdjuster{}
	metrics := &recordingInventoryMetrics{}
	sync, err := NewTikTokInventorySync(nil, InventorySyncConfig{
		WC:      wc,
		TikTok:  tt,
		Metrics: metrics,
		Now:     func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewTikTokInventorySync: %v", err)
	}
	t.Cleanup(func() { _ = sync.Close(context.Background()) })
	return sync, wc, tt, metrics
}

// TestInventorySync_ReducesTikTokStockOnWCOrderFulfilled is the
// EC-3-4 RED acceptance test. Driving the WC->TikTok direction with
// a successful WC + TikTok adjustment confirms the bidirectional
// sync executes both sides exactly once.
func TestInventorySync_ReducesTikTokStockOnWCOrderFulfilled(t *testing.T) {
	t.Parallel()

	saga, wc, tt, metrics := newSyncHarness(t)
	req := StockAdjustRequest{
		TenantID: "tenant-1",
		SKU:      "SKU-1",
		Delta:    -2,
		OrderID:  "wc-order-1",
	}
	if err := saga.ApplyWCFulfilment(context.Background(), req); err != nil {
		t.Fatalf("ApplyWCFulfilment: %v", err)
	}
	if got := wc.totalDelta(); got != -2 {
		t.Fatalf("wc total delta = %d, want -2", got)
	}
	if got := tt.totalDelta(); got != -2 {
		t.Fatalf("tt total delta = %d, want -2 (decrement TikTok stock)", got)
	}
	if got := metrics.snapshot(); len(got) != 1 || got[0] != "wc_to_tiktok:ok" {
		t.Fatalf("metrics = %v", got)
	}
}

func TestInventorySync_ReducesWCStockOnTikTokOrder(t *testing.T) {
	t.Parallel()
	saga, wc, tt, metrics := newSyncHarness(t)
	req := StockAdjustRequest{
		TenantID: "tenant-1",
		SKU:      "SKU-1",
		Delta:    -1,
		OrderID:  "tt-order-1",
	}
	if err := saga.ApplyTikTokOrder(context.Background(), req); err != nil {
		t.Fatalf("ApplyTikTokOrder: %v", err)
	}
	if got := tt.callCount(); got != 0 {
		t.Fatalf("TikTok adjuster should NOT be called when TikTok is the source (informational); got %d", got)
	}
	if got := wc.totalDelta(); got != -1 {
		t.Fatalf("wc total delta = %d, want -1 (TikTok->WC reduction)", got)
	}
	got := metrics.snapshot()
	if len(got) != 1 || got[0] != "tiktok_to_wc:ok" {
		t.Fatalf("metrics = %v", got)
	}
}

// TestInventorySync_DualPlatformOrderStormZeroOversell mirrors the
// EC-3-4 acceptance: a synthetic dual-platform order storm must
// not cause double-decrement. Concurrent calls with the same
// (tenant, order_id, channel) tuple resolve to a single source-of-
// truth decrement.
func TestInventorySync_DualPlatformOrderStormZeroOversell(t *testing.T) {
	t.Parallel()

	saga, wc, tt, metrics := newSyncHarness(t)
	const N = 16
	var wg sync.WaitGroup
	var dups atomic.Int64
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			err := saga.ApplyWCFulfilment(context.Background(), StockAdjustRequest{
				TenantID: "tenant-1",
				SKU:      "SKU-shared",
				Delta:    -1,
				OrderID:  "shared-order-1",
			})
			if errors.Is(err, ErrInventorySyncDuplicate) {
				dups.Add(1)
				return
			}
			if err != nil {
				t.Errorf("ApplyWCFulfilment: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := wc.callCount(); got != 1 {
		t.Fatalf("wc.callCount = %d, want 1 (zero oversell)", got)
	}
	if got := tt.callCount(); got != 1 {
		t.Fatalf("tt.callCount = %d, want 1", got)
	}
	if dups.Load() != int64(N-1) {
		t.Fatalf("duplicates = %d, want %d", dups.Load(), N-1)
	}
	got := metrics.snapshot()
	gotOK := 0
	gotDup := 0
	for _, e := range got {
		switch e {
		case "wc_to_tiktok:ok":
			gotOK++
		case "wc_to_tiktok:duplicate":
			gotDup++
		}
	}
	if gotOK != 1 || gotDup != int(N-1) {
		t.Fatalf("metrics ok=%d dup=%d", gotOK, gotDup)
	}
}

// TestInventorySync_SagaRollbackOnTargetFailure asserts the
// compensating action runs when the target adjuster fails. WC was
// the source (decremented) so the rollback re-adds the stock.
func TestInventorySync_SagaRollbackOnTargetFailure(t *testing.T) {
	t.Parallel()

	wc := &recordingAdjuster{}
	tt := &recordingAdjuster{failOnce: errors.New("tiktok 500")}
	metrics := &recordingInventoryMetrics{}
	saga, err := NewTikTokInventorySync(nil, InventorySyncConfig{
		WC:      wc,
		TikTok:  tt,
		Metrics: metrics,
	})
	if err != nil {
		t.Fatalf("NewTikTokInventorySync: %v", err)
	}
	t.Cleanup(func() { _ = saga.Close(context.Background()) })
	req := StockAdjustRequest{TenantID: "tenant-1", SKU: "SKU-1", Delta: -3, OrderID: "order-rb"}
	err = saga.ApplyWCFulfilment(context.Background(), req)
	if !errors.Is(err, ErrInventorySyncTargetFailed) {
		t.Fatalf("err = %v, want ErrInventorySyncTargetFailed", err)
	}
	if got := wc.totalDelta(); got != 0 {
		t.Fatalf("wc net delta = %d, want 0 (decrement + compensating add cancel out)", got)
	}
	if got := wc.callCount(); got != 2 {
		t.Fatalf("wc calls = %d, want 2 (decrement + compensation)", got)
	}
	got := metrics.snapshot()
	hasTargetFailed := false
	hasRolledBack := false
	for _, e := range got {
		if e == "wc_to_tiktok:target_failed" {
			hasTargetFailed = true
		}
		if e == "wc_to_tiktok:rolled_back" {
			hasRolledBack = true
		}
	}
	if !hasTargetFailed || !hasRolledBack {
		t.Fatalf("metrics missing target_failed or rolled_back: %v", got)
	}
}

func TestInventorySync_RollbackFailureWrapsBothErrors(t *testing.T) {
	t.Parallel()

	wc := &recordingAdjuster{
		errs: []error{nil, errors.New("compensation failed")},
	}
	tt := &recordingAdjuster{failOnce: errors.New("tiktok 500")}
	metrics := &recordingInventoryMetrics{}
	saga, err := NewTikTokInventorySync(nil, InventorySyncConfig{WC: wc, TikTok: tt, Metrics: metrics})
	if err != nil {
		t.Fatalf("NewTikTokInventorySync: %v", err)
	}
	t.Cleanup(func() { _ = saga.Close(context.Background()) })
	req := StockAdjustRequest{TenantID: "tenant-1", SKU: "SKU-1", Delta: -1, OrderID: "order-deep-fail"}
	err = saga.ApplyWCFulfilment(context.Background(), req)
	if !errors.Is(err, ErrInventorySyncRollbackFailed) {
		t.Fatalf("err = %v, want ErrInventorySyncRollbackFailed", err)
	}
}

func TestInventorySync_SourceFailureReturnsBeforeTarget(t *testing.T) {
	t.Parallel()

	wc := &recordingAdjuster{failOnce: errors.New("wc 503")}
	tt := &recordingAdjuster{}
	saga, err := NewTikTokInventorySync(nil, InventorySyncConfig{WC: wc, TikTok: tt})
	if err != nil {
		t.Fatalf("NewTikTokInventorySync: %v", err)
	}
	t.Cleanup(func() { _ = saga.Close(context.Background()) })
	req := StockAdjustRequest{TenantID: "tenant-1", SKU: "SKU-1", Delta: -1, OrderID: "order-source-fail"}
	err = saga.ApplyWCFulfilment(context.Background(), req)
	if err == nil {
		t.Fatalf("expected source error")
	}
	if tt.callCount() != 0 {
		t.Fatalf("target adjuster should not be called when source fails")
	}
}

func TestInventorySync_ValidateApplyRequest(t *testing.T) {
	t.Parallel()
	saga, _, _, _ := newSyncHarness(t)
	cases := []StockAdjustRequest{
		{SKU: "S", Delta: -1, OrderID: "o"},
		{TenantID: "t", Delta: -1, OrderID: "o"},
		{TenantID: "t", SKU: "S", OrderID: "o"},
		{TenantID: "t", SKU: "S", Delta: -1},
	}
	for i, c := range cases {
		err := saga.ApplyTikTokOrder(context.Background(), c)
		if !errors.Is(err, ErrInventorySyncUnconfigured) {
			t.Fatalf("case %d err = %v", i, err)
		}
	}
}

func TestInventorySync_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	saga, _, _, _ := newSyncHarness(t)
	_ = saga.Close(context.Background())
	err := saga.ApplyWCFulfilment(context.Background(), StockAdjustRequest{TenantID: "t", SKU: "S", Delta: -1, OrderID: "o"})
	if !errors.Is(err, ErrInventorySyncClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewTikTokInventorySync_Validation(t *testing.T) {
	t.Parallel()
	wc := StockAdjusterFunc(func(_ context.Context, _ StockAdjustRequest) error { return nil })
	tt := StockAdjusterFunc(func(_ context.Context, _ StockAdjustRequest) error { return nil })
	if _, err := NewTikTokInventorySync(nil, InventorySyncConfig{TikTok: tt}); !errors.Is(err, ErrInventorySyncUnconfigured) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewTikTokInventorySync(nil, InventorySyncConfig{WC: wc}); !errors.Is(err, ErrInventorySyncUnconfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestMemoryIdempotencyStore_RoundTrip(t *testing.T) {
	t.Parallel()
	store := NewMemoryIdempotencyStore()
	ok, err := store.Reserve(context.Background(), "k1")
	if err != nil || !ok {
		t.Fatalf("first reserve: ok=%v err=%v", ok, err)
	}
	ok, err = store.Reserve(context.Background(), "k1")
	if err != nil || ok {
		t.Fatalf("dup reserve: ok=%v err=%v", ok, err)
	}
	if err := store.Release(context.Background(), "k1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	ok, _ = store.Reserve(context.Background(), "k1")
	if !ok {
		t.Fatalf("after release, reserve should succeed")
	}
}

func TestMemoryIdempotencyStore_RejectsEmpty(t *testing.T) {
	t.Parallel()
	store := NewMemoryIdempotencyStore()
	if _, err := store.Reserve(context.Background(), ""); !errors.Is(err, ErrInventorySyncUnconfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildIdempotencyKey_PrefersExplicit(t *testing.T) {
	t.Parallel()
	with := buildIdempotencyKey(DirectionWCToTikTok, StockAdjustRequest{TenantID: "t", IdempotencyKey: "explicit", OrderID: "o", SKU: "S"})
	without := buildIdempotencyKey(DirectionWCToTikTok, StockAdjustRequest{TenantID: "t", OrderID: "o", SKU: "S"})
	if with == without {
		t.Fatalf("explicit key should override derived")
	}
}

func TestStockAdjusterFunc_Adapts(t *testing.T) {
	t.Parallel()
	called := atomic.Bool{}
	adj := StockAdjusterFunc(func(_ context.Context, _ StockAdjustRequest) error {
		called.Store(true)
		return nil
	})
	if err := adj.Adjust(context.Background(), StockAdjustRequest{}); err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if !called.Load() {
		t.Fatalf("func not invoked")
	}
}
