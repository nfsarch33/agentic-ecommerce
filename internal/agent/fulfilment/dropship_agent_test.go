// File scope: v3.5.0 EC-7-2 drop-ship supplier order agent RED tests.
// TDD-first per the v3.5.0 plan (story 5; depends on EC-7-1
// OrderNormalisedEvent + the v3.1.0 China adapters + the v3.5.0
// AliExpress fallback adapter).
//
// Acceptance per ADR-028 EC-7-2:
//   - Subscribes to OrderNormalisedEvent.
//   - Places drop-ship order via primary supplier (1688/Taobao).
//   - Falls back to AliExpress on primary failure.
//   - Operator approval gate fires for orders > A$500.
//   - Saga rollback emits DropshipOrderRolledBack when every
//     supplier adapter fails.
package fulfilment

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

// fakeSupplierClient is a SupplierOrderClient double. Returns the
// configured supplierOrderID + err.
type fakeSupplierClient struct {
	name            string
	supplierOrderID string
	err             error

	mu    sync.Mutex
	calls int
}

func (f *fakeSupplierClient) SupplierName() string { return f.name }

func (f *fakeSupplierClient) PlaceOrder(_ context.Context, req SupplierOrderRequest) (SupplierOrderResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return SupplierOrderResult{}, f.err
	}
	return SupplierOrderResult{
		SupplierOrderID: f.supplierOrderID,
		PlacedAt:        time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC),
	}, nil
}

func (f *fakeSupplierClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// recordingDropshipPublisher captures emitted events. Mirrors the
// v3.5.0 EC-6-1 / EC-6-3 test harness pattern.
type recordingDropshipPublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (p *recordingDropshipPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *recordingDropshipPublisher) Close() error { return nil }

func (p *recordingDropshipPublisher) snapshot() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.events))
	copy(out, p.events)
	return out
}

// recordingFulfilmentTrigger captures every customer-side
// fulfilment trigger so saga rollback assertions can verify the
// compensating action ran.
type recordingFulfilmentTrigger struct {
	mu         sync.Mutex
	triggered  []string
	rolledBack []string
}

func (r *recordingFulfilmentTrigger) Trigger(_ context.Context, orderID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.triggered = append(r.triggered, orderID)
	return nil
}

func (r *recordingFulfilmentTrigger) Rollback(_ context.Context, orderID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rolledBack = append(r.rolledBack, orderID)
	return nil
}

func (r *recordingFulfilmentTrigger) snapshot() (triggered, rolledBack []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := make([]string, len(r.triggered))
	copy(t, r.triggered)
	rb := make([]string, len(r.rolledBack))
	copy(rb, r.rolledBack)
	return t, rb
}

func newDropshipHarness(t *testing.T, primary, fallback *fakeSupplierClient, threshold int) (*DropshipAgent, *recordingDropshipPublisher, *recordingFulfilmentTrigger) {
	t.Helper()
	pub := &recordingDropshipPublisher{}
	trigger := &recordingFulfilmentTrigger{}
	cfg := DropshipAgentConfig{
		TenantID:                 "tenant-1",
		Primary:                  primary,
		Publisher:                pub,
		FulfilmentTrigger:        trigger,
		LargeOrderThresholdCents: threshold,
		Now:                      func() time.Time { return time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC) },
	}
	if fallback != nil {
		cfg.Fallback = fallback
	}
	agent, err := NewDropshipAgent(nil, cfg)
	if err != nil {
		t.Fatalf("NewDropshipAgent: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close(context.Background()) })
	return agent, pub, trigger
}

func sampleNormalisedOrder(totalCents int) NormalisedOrder {
	return NormalisedOrder{
		TenantID:        "tenant-1",
		OrderID:         "ord-tiktok-T1",
		ExternalOrderID: "T1",
		Channel:         "tiktok",
		BuyerEmail:      "buyer@example.com",
		TotalAUDCents:   totalCents,
		Currency:        "AUD",
		Items:           []NormalisedOrderLine{{SKU: "sku-A", Quantity: 1, UnitCents: totalCents}},
	}
}

func TestDropshipAgent_PlacesSupplierOrderOnCustomerOrderReceived(t *testing.T) {
	t.Parallel()
	primary := &fakeSupplierClient{name: "1688", supplierOrderID: "1688-A100"}
	fallback := &fakeSupplierClient{name: "aliexpress", supplierOrderID: "AE-fallback"}
	agent, pub, trigger := newDropshipHarness(t, primary, fallback, 50000)
	res, err := agent.Place(context.Background(), sampleNormalisedOrder(20000))
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if !res.Placed {
		t.Fatalf("Placed=false, want true")
	}
	if res.Supplier != "1688" {
		t.Fatalf("Supplier = %s, want 1688 (primary)", res.Supplier)
	}
	if res.SupplierOrderID != "1688-A100" {
		t.Fatalf("SupplierOrderID = %s, want 1688-A100", res.SupplierOrderID)
	}
	if primary.callCount() != 1 {
		t.Fatalf("primary called %d times, want 1", primary.callCount())
	}
	if fallback.callCount() != 0 {
		t.Fatalf("fallback called %d times, want 0", fallback.callCount())
	}
	events := pub.snapshot()
	if len(events) != 1 || events[0].Type != eventbus.DropshipOrderPlaced {
		t.Fatalf("events = %+v, want one DropshipOrderPlaced", events)
	}
	triggered, rolledBack := trigger.snapshot()
	if len(triggered) != 1 || triggered[0] != "ord-tiktok-T1" {
		t.Fatalf("triggered = %+v, want one ord-tiktok-T1", triggered)
	}
	if len(rolledBack) != 0 {
		t.Fatalf("rolledBack = %+v, want empty", rolledBack)
	}
}

func TestDropshipAgent_LargeOrderApprovalGateFires(t *testing.T) {
	t.Parallel()
	primary := &fakeSupplierClient{name: "1688", supplierOrderID: "1688-LARGE"}
	agent, pub, trigger := newDropshipHarness(t, primary, nil, 50000)
	// 60000 cents = A$600 -- above the 50000 threshold.
	res, err := agent.Place(context.Background(), sampleNormalisedOrder(60000))
	if err != nil {
		t.Fatalf("Place large: %v", err)
	}
	if res.PendingApproval == false {
		t.Fatalf("PendingApproval=false, want true (>$500 gate)")
	}
	if res.Placed {
		t.Fatalf("Placed=true, want false (waiting on approval)")
	}
	if primary.callCount() != 0 {
		t.Fatalf("primary called %d times, want 0 (waiting on approval)", primary.callCount())
	}
	events := pub.snapshot()
	if len(events) != 1 || events[0].Type != eventbus.LargeDropshipOrderPendingApproval {
		t.Fatalf("events = %+v, want one LargeDropshipOrderPendingApproval", events)
	}
	triggered, _ := trigger.snapshot()
	if len(triggered) != 0 {
		t.Fatalf("triggered = %+v, want empty (held)", triggered)
	}
}

func TestDropshipAgent_FallsBackToAliExpressOnPrimaryFailure(t *testing.T) {
	t.Parallel()
	primary := &fakeSupplierClient{name: "1688", err: errors.New("primary 502")}
	fallback := &fakeSupplierClient{name: "aliexpress", supplierOrderID: "AE-rescue"}
	agent, pub, trigger := newDropshipHarness(t, primary, fallback, 50000)
	res, err := agent.Place(context.Background(), sampleNormalisedOrder(20000))
	if err != nil {
		t.Fatalf("Place fallback: %v", err)
	}
	if !res.Placed {
		t.Fatalf("Placed=false, want true (fallback succeeded)")
	}
	if res.Supplier != "aliexpress" {
		t.Fatalf("Supplier = %s, want aliexpress", res.Supplier)
	}
	if res.SupplierOrderID != "AE-rescue" {
		t.Fatalf("SupplierOrderID = %s, want AE-rescue", res.SupplierOrderID)
	}
	if primary.callCount() != 1 || fallback.callCount() != 1 {
		t.Fatalf("primary calls=%d, fallback calls=%d, want 1/1", primary.callCount(), fallback.callCount())
	}
	events := pub.snapshot()
	if len(events) != 1 || events[0].Type != eventbus.DropshipOrderPlaced {
		t.Fatalf("events = %+v, want one DropshipOrderPlaced", events)
	}
	triggered, _ := trigger.snapshot()
	if len(triggered) != 1 {
		t.Fatalf("triggered = %+v, want 1 (fallback succeeded)", triggered)
	}
}

func TestDropshipAgent_SagaRollbackOnSupplierFailure(t *testing.T) {
	t.Parallel()
	primary := &fakeSupplierClient{name: "1688", err: errors.New("primary 502")}
	fallback := &fakeSupplierClient{name: "aliexpress", err: errors.New("fallback 503")}
	agent, pub, trigger := newDropshipHarness(t, primary, fallback, 50000)
	res, err := agent.Place(context.Background(), sampleNormalisedOrder(20000))
	if !errors.Is(err, ErrAllSuppliersFailed) {
		t.Fatalf("Place: err=%v, want ErrAllSuppliersFailed", err)
	}
	if res.Placed {
		t.Fatalf("Placed=true, want false (all suppliers failed)")
	}
	if !res.SagaRolledBack {
		t.Fatalf("SagaRolledBack=false, want true")
	}
	if primary.callCount() != 1 || fallback.callCount() != 1 {
		t.Fatalf("primary=%d fallback=%d, want 1/1", primary.callCount(), fallback.callCount())
	}
	events := pub.snapshot()
	if len(events) != 1 || events[0].Type != eventbus.DropshipOrderRolledBack {
		t.Fatalf("events = %+v, want one DropshipOrderRolledBack", events)
	}
	triggered, rolledBack := trigger.snapshot()
	if len(triggered) != 0 {
		t.Fatalf("triggered = %+v, want empty (no fulfillment fired before saga rollback)", triggered)
	}
	if len(rolledBack) != 1 || rolledBack[0] != "ord-tiktok-T1" {
		t.Fatalf("rolledBack = %+v, want one rollback for ord-tiktok-T1", rolledBack)
	}
}

func TestDropshipAgent_FallsBackWhenNoFallbackConfigured(t *testing.T) {
	t.Parallel()
	primary := &fakeSupplierClient{name: "1688", err: errors.New("primary 502")}
	agent, _, _ := newDropshipHarness(t, primary, nil, 50000)
	_, err := agent.Place(context.Background(), sampleNormalisedOrder(20000))
	if !errors.Is(err, ErrAllSuppliersFailed) {
		t.Fatalf("Place: err=%v, want ErrAllSuppliersFailed (no fallback)", err)
	}
}

func TestDropshipAgent_ApprovalThresholdConfigurable(t *testing.T) {
	t.Parallel()
	primary := &fakeSupplierClient{name: "1688", supplierOrderID: "1688-low-thresh"}
	// Set threshold to 10000 (A$100). 15000 cent order is above.
	agent, pub, _ := newDropshipHarness(t, primary, nil, 10000)
	res, err := agent.Place(context.Background(), sampleNormalisedOrder(15000))
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if !res.PendingApproval {
		t.Fatalf("PendingApproval=false, want true (15000 > 10000)")
	}
	events := pub.snapshot()
	if len(events) != 1 || events[0].Type != eventbus.LargeDropshipOrderPendingApproval {
		t.Fatalf("events = %+v, want pending approval", events)
	}
}

func TestDropshipAgent_HandleOrderNormalisedEvent(t *testing.T) {
	t.Parallel()
	primary := &fakeSupplierClient{name: "1688", supplierOrderID: "1688-evt"}
	agent, pub, _ := newDropshipHarness(t, primary, nil, 50000)
	payload := eventbus.OrderNormalisedPayload{
		Version:         eventbus.OrderNormalisedPayloadVersion,
		TenantID:        "tenant-1",
		OrderID:         "ord-tiktok-evt",
		ExternalOrderID: "evt",
		Channel:         "tiktok",
		BuyerEmail:      "buyer@example.com",
		TotalAUDCents:   20000,
		Currency:        "AUD",
		Items: []eventbus.OrderNormalisedLine{
			{SKU: "sku-A", Quantity: 1, UnitCents: 20000},
		},
		OccurredAt: time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC),
	}
	evt, err := eventbus.NewOrderNormalisedEvent("test", time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC), payload)
	if err != nil {
		t.Fatalf("NewOrderNormalisedEvent: %v", err)
	}
	if err := agent.HandleOrderNormalised(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderNormalised: %v", err)
	}
	if primary.callCount() != 1 {
		t.Fatalf("primary calls = %d, want 1", primary.callCount())
	}
	if events := pub.snapshot(); len(events) != 1 || events[0].Type != eventbus.DropshipOrderPlaced {
		t.Fatalf("events = %+v, want DropshipOrderPlaced", events)
	}
}

func TestDropshipAgent_RejectsMissingDeps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  DropshipAgentConfig
	}{
		{name: "no_tenant", cfg: DropshipAgentConfig{Primary: &fakeSupplierClient{}, Publisher: &recordingDropshipPublisher{}}},
		{name: "no_primary", cfg: DropshipAgentConfig{TenantID: "t", Publisher: &recordingDropshipPublisher{}}},
		{name: "no_publisher", cfg: DropshipAgentConfig{TenantID: "t", Primary: &fakeSupplierClient{}}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewDropshipAgent(nil, tc.cfg)
			if !errors.Is(err, ErrDropshipAgentUnconfigured) {
				t.Fatalf("%s: err=%v, want ErrDropshipAgentUnconfigured", tc.name, err)
			}
		})
	}
}

func TestDropshipAgent_PlaceAfterCloseRejects(t *testing.T) {
	t.Parallel()
	primary := &fakeSupplierClient{name: "1688"}
	agent, _, _ := newDropshipHarness(t, primary, nil, 50000)
	if err := agent.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := agent.Place(context.Background(), sampleNormalisedOrder(20000))
	if !errors.Is(err, ErrDropshipAgentClosed) {
		t.Fatalf("Place after Close: err=%v, want ErrDropshipAgentClosed", err)
	}
}

func TestDropshipAgent_RejectsInvalidOrder(t *testing.T) {
	t.Parallel()
	agent, _, _ := newDropshipHarness(t, &fakeSupplierClient{name: "1688"}, nil, 50000)
	cases := []struct {
		name string
		ord  NormalisedOrder
	}{
		{name: "no_tenant", ord: NormalisedOrder{OrderID: "x", Channel: "tiktok", TotalAUDCents: 100, Items: []NormalisedOrderLine{{SKU: "a", Quantity: 1}}}},
		{name: "no_order_id", ord: NormalisedOrder{TenantID: "t", Channel: "tiktok", TotalAUDCents: 100, Items: []NormalisedOrderLine{{SKU: "a", Quantity: 1}}}},
		{name: "negative_total", ord: NormalisedOrder{TenantID: "t", OrderID: "x", Channel: "tiktok", TotalAUDCents: -1, Items: []NormalisedOrderLine{{SKU: "a", Quantity: 1}}}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := agent.Place(context.Background(), tc.ord)
			if !errors.Is(err, ErrInvalidNormalisedOrder) {
				t.Fatalf("%s: err=%v, want ErrInvalidNormalisedOrder", tc.name, err)
			}
		})
	}
}
