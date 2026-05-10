// File scope: v3.9.1 EC-4-4 -- channel router stub-recognition RED
// tests + listing port helpers.
package channel

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/channelport"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// stubAdapter is a test double that simulates the EC-4-4 stub
// channels by always returning a wrapped ErrChannelNotYetImplemented
// from Publish. The Instagram + Pinterest production stubs in
// internal/adapter/social wrap social.ErrChannelNotImplemented; the
// router-side recognition layer accepts BOTH the typed sentinel AND
// the channel name (IsStubChannel) so either path succeeds.
type stubAdapter struct {
	name string
	mu   sync.Mutex
	last eventbus.ProductEnrichedPayload
	hits int
}

func newStubAdapter(name string) *stubAdapter { return &stubAdapter{name: name} }

func (s *stubAdapter) Name() string { return s.name }

func (s *stubAdapter) Publish(_ context.Context, p eventbus.ProductEnrichedPayload) error {
	s.mu.Lock()
	s.hits++
	s.last = p
	s.mu.Unlock()
	return ErrChannelNotYetImplemented
}

func (s *stubAdapter) Close(_ context.Context) error { return nil }

func TestIsStubChannel_PostV460Promotion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want bool
	}{
		{"instagram", false},
		{"pinterest", false},
		{"INSTAGRAM", false},
		{" pinterest ", false},
		{"tiktok", false},
		{"facebook", false},
		{"rednote", false},
		{"", false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := channelport.IsStubChannel(c.name); got != c.want {
				t.Fatalf("IsStubChannel(%q)=%v want=%v", c.name, got, c.want)
			}
		})
	}
}

func TestChannelRouter_RecognizesIGAndPinterestAsStubs(t *testing.T) {
	t.Parallel()
	ig := newStubAdapter("instagram")
	pin := newStubAdapter("pinterest")
	channels := []ChannelDescriptor{
		{Adapter: ig, Matcher: MatchAlways},
		{Adapter: pin, Matcher: MatchAlways},
	}
	pool := workerpool.New(nil, workerpool.Config{Name: "stub-test", MinWorkers: 2, MaxWorkers: 2, QueueDepth: 4})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	bus := eventbus.NewInMemoryBus()
	metrics := &recordingRouterMetrics{}
	dlq := NewInMemoryDLQ()
	router, err := NewChannelRouter(nil, ChannelRouterConfig{
		TenantID:        "tenant-v391",
		Channels:        channels,
		Pool:            pool,
		Publisher:       bus,
		Consumer:        bus,
		DLQ:             dlq,
		Metrics:         metrics,
		Now:             func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
		DispatchTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewChannelRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close(context.Background()) })

	// Capture stub-not-yet-implemented events from the bus so we
	// can assert the router emitted them rather than enqueueing
	// the dispatch into the DLQ.
	var capturedMu sync.Mutex
	var captured []eventbus.Event
	if err := bus.Subscribe(context.Background(), []eventbus.EventType{eventbus.ChannelStatusNotYetImplemented}, "stub.observer", func(_ context.Context, evt eventbus.Event) error {
		capturedMu.Lock()
		captured = append(captured, evt)
		capturedMu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	enriched, err := eventbus.NewProductEnrichedEvent("test", time.Now().UTC(), eventbus.ProductEnrichedPayload{
		Version:      eventbus.ProductEnrichedPayloadVersion,
		TenantID:     "tenant-v391",
		ProductID:    "sku-v391",
		EnglishTitle: "Sample stub product",
		PriceCents:   4990,
		Currency:     "AUD",
	})
	if err != nil {
		t.Fatalf("NewProductEnrichedEvent: %v", err)
	}
	if err := router.HandleEvent(context.Background(), enriched); err != nil {
		t.Fatalf("HandleEvent err=%v want=nil (stub channels must not error)", err)
	}
	// Force-drain async event delivery so the subscriber observes
	// the publish before we assert.
	time.Sleep(20 * time.Millisecond)

	if records := dlq.Records(); len(records) != 0 {
		t.Fatalf("DLQ should be empty for stub channels, got %d records", len(records))
	}
	gotIG := metrics.dispatchOutcome("instagram")
	gotPin := metrics.dispatchOutcome("pinterest")
	if gotIG != "not_yet_implemented" {
		t.Fatalf("instagram dispatch outcome=%q want=not_yet_implemented (all=%v)", gotIG, metrics.dispatch)
	}
	if gotPin != "not_yet_implemented" {
		t.Fatalf("pinterest dispatch outcome=%q want=not_yet_implemented (all=%v)", gotPin, metrics.dispatch)
	}

	capturedMu.Lock()
	defer capturedMu.Unlock()
	if len(captured) < 2 {
		t.Fatalf("expected 2 ChannelStatusNotYetImplemented events, got %d", len(captured))
	}
	seen := map[string]bool{}
	for _, evt := range captured {
		if got, _ := evt.Payload["channel"].(string); got != "" {
			seen[got] = true
		}
	}
	if !seen["instagram"] || !seen["pinterest"] {
		t.Fatalf("expected events for both instagram + pinterest; got %v", seen)
	}
}

// dispatchOutcome returns the most recent outcome recorded for the
// supplied channel (test helper used by the stub-recognition test).
func (r *recordingRouterMetrics) dispatchOutcome(channel string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.dispatch) - 1; i >= 0; i-- {
		if strings.HasPrefix(r.dispatch[i], channel+"=") {
			return strings.TrimPrefix(r.dispatch[i], channel+"=")
		}
	}
	return ""
}
