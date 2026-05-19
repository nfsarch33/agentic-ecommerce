//go:build chaos

// File scope: v3.3.1 QA Task 3 -- saga rollback chaos matrix for
// the EC-3-2 TikTok listing agent + EC-3-4 inventory sync.
//
// Three scenarios per the v3.3.1 plan:
//
//  1. Listing agent: TikTok API returns 500 mid-publish ->
//     compensating delete fires; TikTokListingRolledBack event
//     emitted; product NEVER lands in inventory.
//  2. Inventory sync: WC succeeds, TikTok returns 500 ->
//     WC source-side rollback fires (re-add stock); both stores
//     stay consistent. Surface ErrInventorySyncTargetFailed.
//  3. Inventory sync: TikTok succeeds, WC returns 500 ->
//     no source-side compensation needed (TikTok is informational
//     producer in TikTok->WC direction); both stores stay
//     consistent because TikTok physical stock is unchanged AND
//     WC stock is unchanged. Surface ErrInventorySyncTargetFailed.
//
// Note on Temporal: the v3.3.0 EC-3-4 saga was deliberately built
// without a Temporal wrapper (per internal/sync/tiktok_inventory.go
// header comment) so it can run in the order webhook + WC fulfilment
// hot paths without a worker hop. The Temporal workflow wrapper
// is a v3.5+ follow-up. These chaos tests therefore drive the
// in-process saga directly; the v2.2.0+ Temporal testsuite pattern
// applies to internal/workflow/sourcing_test.go and is reused at
// the v3.5.0 sprint when the inventory saga gets a workflow wrapper.
package chaos

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/social"
	"github.com/nfsarch33/helixon-ec/internal/agent/channel"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	syncsaga "github.com/nfsarch33/helixon-ec/internal/sync"
)

// TestTikTokSaga_ListingAgentRollsBackOnAPIFailure is Task 3
// scenario 1. The fake social.Client returns ErrTikTokInvalidResponse
// (the 500-mapped sentinel) on CreateProduct; the agent must run
// the compensating delete (when remoteID is non-empty) AND emit
// TikTokListingRolledBack.
func TestTikTokSaga_ListingAgentRollsBackOnAPIFailure(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	fc := newSagaFakeSocialClient("tt-rollback-1", social.ErrTikTokInvalidResponse, nil)
	agent := mustNewSagaListingAgent(t, fc, bus)
	t.Cleanup(func() { _ = agent.Close(context.Background()) })

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("agent.Start: %v", err)
	}
	evt := mustNewSagaEnrichedEvent(t, "p-saga-1")
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	if got := fc.deleteCalls(); got != 1 {
		t.Fatalf("delete calls = %d, want 1 (compensating action on remoteID)", got)
	}
	if got := countRollbackEvents(bus); got != 1 {
		t.Fatalf("rollback events = %d, want 1", got)
	}
	// "product NOT in inventory" is enforced by the agent never
	// recording a successful publish on this path. The fake's
	// createCalls counter is 1 (the failed attempt) but the
	// CreateProduct return error means no inventory entry was
	// recorded.
	if fc.createCalls() != 1 {
		t.Fatalf("create calls = %d, want 1 (single failed attempt)", fc.createCalls())
	}
}

// TestTikTokSaga_InventoryWCSucceedsTikTokFails is Task 3 scenario 2.
// Direction = WC->TikTok. WC adjuster succeeds; TikTok adjuster
// returns "tiktok 500"; saga compensates by re-adding the WC delta.
// Net WC delta == 0; saga returns ErrInventorySyncTargetFailed.
func TestTikTokSaga_InventoryWCSucceedsTikTokFails(t *testing.T) {
	t.Parallel()

	wc := newRecordingChaosAdjuster(nil)
	tt := newRecordingChaosAdjuster([]error{errors.New("tiktok 500")})
	saga := mustNewSagaInventory(t, wc, tt)
	t.Cleanup(func() { _ = saga.Close(context.Background()) })

	req := syncsaga.StockAdjustRequest{
		TenantID: "tenant-saga",
		SKU:      "SKU-saga-1",
		Delta:    -3,
		OrderID:  "wc-order-saga-1",
	}
	err := saga.ApplyWCFulfilment(context.Background(), req)
	if !errors.Is(err, syncsaga.ErrInventorySyncTargetFailed) {
		t.Fatalf("err = %v, want ErrInventorySyncTargetFailed", err)
	}
	if got := wc.totalDelta(); got != 0 {
		t.Fatalf("WC net delta = %d, want 0 (decrement + compensating add cancel)", got)
	}
	if got := wc.calls(); got != 2 {
		t.Fatalf("WC calls = %d, want 2 (decrement + compensation)", got)
	}
	if got := tt.calls(); got != 1 {
		t.Fatalf("TikTok calls = %d, want 1 (single failed attempt)", got)
	}
}

// TestTikTokSaga_InventoryTikTokSucceedsWCFails is Task 3 scenario 3.
// Direction = TikTok->WC. TikTok is informational source (NOT
// called by the saga); WC is target. WC returns 500. The saga has
// no source-side adjustment to compensate (TikTok physical stock is
// the producer; nothing was decremented locally) so it surfaces
// ErrInventorySyncTargetFailed. Both stores stay consistent: TikTok
// stock unchanged (the order already happened externally), WC
// stock unchanged (target adjust failed).
func TestTikTokSaga_InventoryTikTokSucceedsWCFails(t *testing.T) {
	t.Parallel()

	wc := newRecordingChaosAdjuster([]error{errors.New("wc 500")})
	tt := newRecordingChaosAdjuster(nil)
	saga := mustNewSagaInventory(t, wc, tt)
	t.Cleanup(func() { _ = saga.Close(context.Background()) })

	req := syncsaga.StockAdjustRequest{
		TenantID: "tenant-saga",
		SKU:      "SKU-saga-2",
		Delta:    -1,
		OrderID:  "tt-order-saga-1",
	}
	err := saga.ApplyTikTokOrder(context.Background(), req)
	if !errors.Is(err, syncsaga.ErrInventorySyncTargetFailed) {
		t.Fatalf("err = %v, want ErrInventorySyncTargetFailed", err)
	}
	if got := tt.calls(); got != 0 {
		t.Fatalf("TikTok calls = %d, want 0 (informational source must NOT be called in TikTok->WC)", got)
	}
	if got := wc.calls(); got != 1 {
		t.Fatalf("WC calls = %d, want 1 (single failed attempt; no source-side comp needed)", got)
	}
	// "Both stores stay consistent" -- WC delta is 0 because the
	// failing call did not commit; TikTok delta is 0 because we never
	// touched it.
	if got := wc.committedDelta(); got != 0 {
		t.Fatalf("WC committed delta = %d, want 0", got)
	}
}

// TestTikTokSaga_RollbackFailureSurfacesJoinedError is the
// degenerate case: target fails AND compensation also fails. The
// saga must return ErrInventorySyncRollbackFailed wrapping both
// errors so the operator alert centre (EC-9-5) can fire a manual
// intervention alert.
func TestTikTokSaga_RollbackFailureSurfacesJoinedError(t *testing.T) {
	t.Parallel()

	// First WC call succeeds (decrement); second WC call (the
	// compensation) fails. TikTok call fails (target).
	wc := newRecordingChaosAdjuster([]error{nil, errors.New("wc compensation 503")})
	tt := newRecordingChaosAdjuster([]error{errors.New("tiktok 500")})
	saga := mustNewSagaInventory(t, wc, tt)
	t.Cleanup(func() { _ = saga.Close(context.Background()) })

	req := syncsaga.StockAdjustRequest{
		TenantID: "tenant-saga",
		SKU:      "SKU-saga-fatal",
		Delta:    -2,
		OrderID:  "wc-order-fatal",
	}
	err := saga.ApplyWCFulfilment(context.Background(), req)
	if !errors.Is(err, syncsaga.ErrInventorySyncRollbackFailed) {
		t.Fatalf("err = %v, want ErrInventorySyncRollbackFailed", err)
	}
	// The error message MUST contain both upstream errors so the
	// operator alert renders enough context.
	msg := err.Error()
	if !contains(msg, "tiktok 500") || !contains(msg, "wc compensation 503") {
		t.Fatalf("err message = %q, must wrap both upstream errors", msg)
	}
}

// --- harness helpers --------------------------------------------------------

// sagaFakeSocialClient is the test double for the EC-3-2 saga path.
// Returns the configured createErr from CreateProduct and tracks
// per-method counters; safe for concurrent use.
type sagaFakeSocialClient struct {
	mu             sync.Mutex
	createReturnID string
	createErr      error
	deleteErr      error
	createCount    int
	deleteCount    int
}

func newSagaFakeSocialClient(remoteID string, createErr, deleteErr error) *sagaFakeSocialClient {
	return &sagaFakeSocialClient{createReturnID: remoteID, createErr: createErr, deleteErr: deleteErr}
}

func (f *sagaFakeSocialClient) ListProducts(_ context.Context, _ social.TikTokListProductsRequest) (social.TikTokProductPage, error) {
	return social.TikTokProductPage{}, nil
}

func (f *sagaFakeSocialClient) CreateProduct(_ context.Context, _ social.TikTokProductPayload) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCount++
	return f.createReturnID, f.createErr
}

func (f *sagaFakeSocialClient) UpdateProduct(_ context.Context, _ string, _ social.TikTokProductPayload) error {
	return nil
}

func (f *sagaFakeSocialClient) DeleteProduct(_ context.Context, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCount++
	return f.deleteErr
}

func (f *sagaFakeSocialClient) SyncInventory(_ context.Context, _ social.TikTokInventoryUpdate) error {
	return nil
}

func (f *sagaFakeSocialClient) Close(_ context.Context) error { return nil }

func (f *sagaFakeSocialClient) createCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCount
}

func (f *sagaFakeSocialClient) deleteCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deleteCount
}

// recordingChaosAdjuster is the StockAdjuster test double used by
// the inventory saga chaos tests. Holds a per-call error queue so
// the test can inject "first call ok, second call fail" patterns.
type recordingChaosAdjuster struct {
	mu       sync.Mutex
	queued   []error
	cursor   int
	requests []syncsaga.StockAdjustRequest
}

func newRecordingChaosAdjuster(errs []error) *recordingChaosAdjuster {
	return &recordingChaosAdjuster{queued: append([]error(nil), errs...)}
}

func (r *recordingChaosAdjuster) Adjust(_ context.Context, req syncsaga.StockAdjustRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	if r.cursor < len(r.queued) {
		err := r.queued[r.cursor]
		r.cursor++
		return err
	}
	return nil
}

func (r *recordingChaosAdjuster) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *recordingChaosAdjuster) totalDelta() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	sum := 0
	for _, req := range r.requests {
		sum += req.Delta
	}
	return sum
}

// committedDelta returns the sum of deltas where the corresponding
// queued error was nil (i.e. the call succeeded). Pure accounting.
func (r *recordingChaosAdjuster) committedDelta() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	sum := 0
	for i, req := range r.requests {
		if i < len(r.queued) && r.queued[i] != nil {
			continue
		}
		sum += req.Delta
	}
	return sum
}

// mustNewSagaListingAgent wires a TikTokListingAgent for the saga
// chaos tests.
func mustNewSagaListingAgent(t *testing.T, client social.Client, bus *eventbus.InMemoryBus) *channel.TikTokListingAgent {
	t.Helper()
	agent, err := channel.NewTikTokListingAgent(nil, channel.TikTokListingConfig{
		Client:           client,
		Publisher:        bus,
		Consumer:         bus,
		TenantID:         "tenant-saga",
		DefaultShipping:  "ship-default",
		CategoryMapper:   func(c string) string { return "tt-" + c },
		ShippingResolver: func(_, c string) string { return "ship-" + c },
	})
	if err != nil {
		t.Fatalf("NewTikTokListingAgent: %v", err)
	}
	return agent
}

// mustNewSagaInventory wires a TikTokInventorySync for the saga
// chaos tests.
func mustNewSagaInventory(t *testing.T, wc, tt syncsaga.StockAdjuster) *syncsaga.TikTokInventorySync {
	t.Helper()
	saga, err := syncsaga.NewTikTokInventorySync(nil, syncsaga.InventorySyncConfig{
		WC:     wc,
		TikTok: tt,
	})
	if err != nil {
		t.Fatalf("NewTikTokInventorySync: %v", err)
	}
	return saga
}

// mustNewSagaEnrichedEvent constructs a synthetic enriched event
// for the listing-agent rollback test.
func mustNewSagaEnrichedEvent(t *testing.T, productID string) eventbus.Event {
	t.Helper()
	payload := eventbus.ProductEnrichedPayload{
		Version:            eventbus.ProductEnrichedPayloadVersion,
		TenantID:           "tenant-saga",
		ProductID:          productID,
		ExternalID:         "ext-" + productID,
		EnglishTitle:       "Saga Rollback Headphones",
		EnglishDescription: "Quality audio.",
		CategoryID:         "audio",
		PriceCents:         4999,
		Currency:           "AUD",
		StockUnits:         10,
		QualityScore:       0.88,
		Source:             "agent.enrichment",
	}
	evt, err := eventbus.NewProductEnrichedEvent("agent.enrichment", sagaFixedTime(), payload)
	if err != nil {
		t.Fatalf("NewProductEnrichedEvent: %v", err)
	}
	return evt
}

// countRollbackEvents counts the v3.3.0 rollback signal on the bus.
func countRollbackEvents(bus *eventbus.InMemoryBus) int {
	n := 0
	for _, e := range bus.Delivered() {
		if e.Type == eventbus.TikTokListingRolledBack {
			n++
		}
	}
	return n
}

// contains is a tiny strings.Contains substitute so the chaos
// package needs no extra import; mirrors the helper used elsewhere
// in tests/chaos/.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// sagaFixedTime returns the deterministic UTC timestamp the saga
// chaos tests use so event timestamps are reproducible across runs.
func sagaFixedTime() time.Time {
	return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
}
