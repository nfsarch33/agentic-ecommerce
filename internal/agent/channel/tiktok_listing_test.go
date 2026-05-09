package channel

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/social"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

// fakeSocialClient is the test double for social.Client. Keeps
// per-call counters + injectable error returns so the saga + happy
// paths are deterministic.
type fakeSocialClient struct {
	mu                sync.Mutex
	createCalls       int
	deleteCalls       int
	createReturnID    string
	createErr         error
	deleteErr         error
	lastCreatePayload social.TikTokProductPayload
	lastDeleteID      string
}

func (f *fakeSocialClient) ListProducts(_ context.Context, _ social.TikTokListProductsRequest) (social.TikTokProductPage, error) {
	return social.TikTokProductPage{}, nil
}

func (f *fakeSocialClient) CreateProduct(_ context.Context, p social.TikTokProductPayload) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.lastCreatePayload = p
	return f.createReturnID, f.createErr
}

func (f *fakeSocialClient) UpdateProduct(_ context.Context, _ string, _ social.TikTokProductPayload) error {
	return nil
}

func (f *fakeSocialClient) DeleteProduct(_ context.Context, remoteID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls++
	f.lastDeleteID = remoteID
	return f.deleteErr
}

func (f *fakeSocialClient) SyncInventory(_ context.Context, _ social.TikTokInventoryUpdate) error {
	return nil
}

func (f *fakeSocialClient) Close(_ context.Context) error { return nil }

type recordingMetrics struct {
	mu   sync.Mutex
	last []string
}

func (r *recordingMetrics) RecordListing(_, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last = append(r.last, outcome)
}

func (r *recordingMetrics) outcomes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.last))
	copy(out, r.last)
	return out
}

func newAgentWithFakes(t *testing.T, fc *fakeSocialClient, bus *eventbus.InMemoryBus, metrics TikTokListingMetrics) *TikTokListingAgent {
	t.Helper()
	agent, err := NewTikTokListingAgent(nil, TikTokListingConfig{
		Client:           fc,
		Publisher:        bus,
		Consumer:         bus,
		TenantID:         "tenant-1",
		DefaultShipping:  "ship-default",
		Metrics:          metrics,
		Now:              func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
		CategoryMapper:   func(c string) string { return "tt-" + c },
		ShippingResolver: func(_, c string) string { return "ship-" + c },
	})
	if err != nil {
		t.Fatalf("NewTikTokListingAgent: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close(context.Background()) })
	return agent
}

func enrichedEvent(t *testing.T, productID string, opts ...func(*eventbus.ProductEnrichedPayload)) eventbus.Event {
	t.Helper()
	payload := eventbus.ProductEnrichedPayload{
		TenantID:           "tenant-1",
		ProductID:          productID,
		ExternalID:         "ext-" + productID,
		EnglishTitle:       "Wireless Headphones",
		EnglishDescription: "Quality audio.",
		CategoryID:         "audio",
		PriceCents:         4999,
		Currency:           "AUD",
		StockUnits:         20,
		Images:             []string{"https://cdn.example.com/img1.jpg"},
		QualityScore:       0.85,
		Source:             "agent.enrichment",
	}
	for _, opt := range opts {
		opt(&payload)
	}
	evt, err := eventbus.NewProductEnrichedEvent("agent.enrichment", time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC), payload)
	if err != nil {
		t.Fatalf("NewProductEnrichedEvent: %v", err)
	}
	return evt
}

// TestTikTokListingAgent_PublishesEnrichedProductToShop is the EC-3-2
// RED acceptance test. Drives the agent end to end via the in-memory
// bus and verifies the social.Client.CreateProduct call observes the
// adapted payload.
func TestTikTokListingAgent_PublishesEnrichedProductToShop(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	fc := &fakeSocialClient{createReturnID: "tt-publish-1"}
	metrics := &recordingMetrics{}
	agent := newAgentWithFakes(t, fc, bus, metrics)
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	evt := enrichedEvent(t, "p-1")
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got := fc.createCalls; got != 1 {
		t.Fatalf("createCalls = %d", got)
	}
	if got := fc.lastCreatePayload.Title; got != "Wireless Headphones" {
		t.Fatalf("title = %q", got)
	}
	if got := fc.lastCreatePayload.CategoryID; got != "tt-audio" {
		t.Fatalf("category = %q (want mapped tt-audio)", got)
	}
	if got := fc.lastCreatePayload.ShippingTemplate; got != "ship-audio" {
		t.Fatalf("shipping = %q (want resolver output ship-audio)", got)
	}
	if got := metrics.outcomes(); len(got) != 1 || got[0] != "published" {
		t.Fatalf("outcomes = %v", got)
	}
}

// TestTikTokListingAgent_RollsBackOnAPIFailure asserts the saga
// emits the compensating delete + TikTokListingRolledBack event.
func TestTikTokListingAgent_RollsBackOnAPIFailure(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	fc := &fakeSocialClient{
		createReturnID: "tt-rollback-1",
		createErr:      social.ErrTikTokInvalidResponse,
	}
	metrics := &recordingMetrics{}
	agent := newAgentWithFakes(t, fc, bus, metrics)
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	evt := enrichedEvent(t, "p-rollback")
	_ = bus.Publish(context.Background(), evt)

	if got := fc.deleteCalls; got != 1 {
		t.Fatalf("deleteCalls = %d, want compensating action", got)
	}
	delivered := bus.Delivered()
	rolledBack := false
	for _, e := range delivered {
		if e.Type == eventbus.TikTokListingRolledBack {
			rolledBack = true
			if reason, ok := e.Payload["reason"].(string); !ok || reason == "" {
				t.Fatalf("rollback missing reason: %v", e.Payload)
			}
		}
	}
	if !rolledBack {
		t.Fatalf("expected TikTokListingRolledBack event")
	}
	got := metrics.outcomes()
	if len(got) < 2 || got[0] != "publish_failed" || got[1] != "rolled_back" {
		t.Fatalf("outcomes = %v", got)
	}
}

func TestTikTokListingAgent_RollbackHandlesMissingRemoteID(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	fc := &fakeSocialClient{createErr: errors.New("network exploded")}
	agent := newAgentWithFakes(t, fc, bus, &recordingMetrics{})
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = bus.Publish(context.Background(), enrichedEvent(t, "p-no-remote"))
	if fc.deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, expected 0 when remoteID empty", fc.deleteCalls)
	}
	if !containsType(bus.Delivered(), eventbus.TikTokListingRolledBack) {
		t.Fatalf("rollback event missing")
	}
}

func TestTikTokListingAgent_TenantMismatchReturnsSentinel(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	fc := &fakeSocialClient{createReturnID: "x"}
	agent := newAgentWithFakes(t, fc, bus, nil)
	evt := enrichedEvent(t, "p-mis", func(p *eventbus.ProductEnrichedPayload) { p.TenantID = "tenant-other" })
	err := agent.HandleEvent(context.Background(), evt)
	if !errors.Is(err, ErrChannelTenantMismatch) {
		t.Fatalf("err = %v", err)
	}
	if fc.createCalls != 0 {
		t.Fatalf("createCalls = %d", fc.createCalls)
	}
}

func TestTikTokListingAgent_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	agent := newAgentWithFakes(t, &fakeSocialClient{createReturnID: "x"}, bus, nil)
	_ = agent.Close(context.Background())
	err := agent.HandleEvent(context.Background(), enrichedEvent(t, "p"))
	if !errors.Is(err, ErrChannelClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewTikTokListingAgent_ConfigValidation(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	fc := &fakeSocialClient{}
	cases := map[string]TikTokListingConfig{
		"missing client":    {Publisher: bus, TenantID: "t"},
		"missing publisher": {Client: fc, TenantID: "t"},
		"missing tenant":    {Client: fc, Publisher: bus},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewTikTokListingAgent(nil, cfg)
			if !errors.Is(err, ErrChannelUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestTikTokListingAgent_StartRequiresConsumer(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	fc := &fakeSocialClient{createReturnID: "x"}
	agent, err := NewTikTokListingAgent(nil, TikTokListingConfig{
		Client:    fc,
		Publisher: bus,
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewTikTokListingAgent: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close(context.Background()) })
	if err := agent.Start(context.Background()); !errors.Is(err, ErrChannelUnconfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeEnriched_RejectsWrongType(t *testing.T) {
	t.Parallel()
	evt := eventbus.Event{Type: eventbus.OrderPlaced, Payload: map[string]any{"version": 1}}
	_, err := decodeEnriched(evt)
	if !errors.Is(err, ErrChannelEnvelopeInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodePayloadMap_HelperFieldExtraction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   map[string]any
		want eventbus.ProductEnrichedPayload
	}{
		{
			name: "all populated",
			in: map[string]any{
				"version":       1,
				"tenant_id":     "t1",
				"product_id":    "p1",
				"english_title": "Title",
				"price_cents":   100,
				"currency":      "AUD",
				"images":        []any{"a", "b"},
				"quality_score": 0.91,
			},
			want: eventbus.ProductEnrichedPayload{
				Version: 1, TenantID: "t1", ProductID: "p1",
				EnglishTitle: "Title", PriceCents: 100, Currency: "AUD",
				Images: []string{"a", "b"}, QualityScore: 0.91,
			},
		},
		{
			name: "string slice",
			in: map[string]any{
				"version":       1,
				"tenant_id":     "t1",
				"product_id":    "p1",
				"english_title": "X",
				"price_cents":   5,
				"images":        []string{"only"},
			},
			want: eventbus.ProductEnrichedPayload{Version: 1, TenantID: "t1", ProductID: "p1", EnglishTitle: "X", PriceCents: 5, Images: []string{"only"}},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodePayloadMap(tc.in)
			if err != nil {
				t.Fatalf("decodePayloadMap: %v", err)
			}
			if got.TenantID != tc.want.TenantID || got.ProductID != tc.want.ProductID || got.EnglishTitle != tc.want.EnglishTitle {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
			if len(got.Images) != len(tc.want.Images) {
				t.Fatalf("images = %v want %v", got.Images, tc.want.Images)
			}
		})
	}
}

func TestStringField_FallbackBehaviours(t *testing.T) {
	t.Parallel()
	if v := stringField(nil, "k"); v != "" {
		t.Fatalf("stringField nil = %q", v)
	}
	if v := intField(nil, "k"); v != 0 {
		t.Fatalf("intField nil = %d", v)
	}
	if v := floatField(nil, "k"); v != 0 {
		t.Fatalf("floatField nil = %g", v)
	}
	if got := stringSliceField(nil, "k"); got != nil {
		t.Fatalf("stringSliceField nil = %v", got)
	}
	if got := stringSliceField(map[string]any{"k": []any{"a"}}, "k"); len(got) != 1 || got[0] != "a" {
		t.Fatalf("stringSliceField []any = %v", got)
	}
	if got := stringSliceField(map[string]any{"k": []string{"x"}}, "k"); len(got) != 1 {
		t.Fatalf("stringSliceField []string = %v", got)
	}
	if got := stringSliceField(map[string]any{"k": "wrong"}, "k"); got != nil {
		t.Fatalf("stringSliceField wrong type = %v", got)
	}
}

func TestErrIs_HelperWraps(t *testing.T) {
	t.Parallel()
	err := strings.Builder{}
	_ = err
	if !errIs(ErrChannelClosed, ErrChannelClosed) {
		t.Fatal("errIs identity")
	}
}

func TestRecordListing_NilMetricsNoOp(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	agent := newAgentWithFakes(t, &fakeSocialClient{createReturnID: "x"}, bus, nil)
	// Force nil metrics path explicitly to assert no panic.
	agent.cfg.Metrics = nil
	if err := bus.Publish(context.Background(), enrichedEvent(t, "p")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func containsType(events []eventbus.Event, t eventbus.EventType) bool {
	for _, e := range events {
		if e.Type == t {
			return true
		}
	}
	return false
}

// Ensure atomic counters are not yet leaked elsewhere; reserved for
// the v3.4 router story when the channel handler fan-outs.
var _ atomic.Int64
