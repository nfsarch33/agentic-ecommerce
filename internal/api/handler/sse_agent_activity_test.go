// File scope: v3.6.0 EC-9-2 SSE agent-activity stream RED tests.
//
// Cite plan EC-9-2 acceptance:
//   - SSE message latency <1s from event emission to client receipt
//   - 30s heartbeat
//   - Backpressure: per-client queue 100-event buffer; on overflow
//     drop oldest + emit `dropped` event
//   - Tenant isolation enforced
//   - Disconnect cleans up subscription (goleak verifies no leaks)
package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// fakeSubscriber is the SSE-side test double for
// AgentActivitySubscriber.
type fakeSubscriber struct {
	mu       sync.Mutex
	handlers []eventbus.Handler
}

func (f *fakeSubscriber) Subscribe(_ context.Context, _ []eventbus.EventType, _ string, h eventbus.Handler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers = append(f.handlers, h)
	return nil
}

func (f *fakeSubscriber) emit(t *testing.T, evt eventbus.Event) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.handlers {
		_ = h(context.Background(), evt)
	}
}

// recordingSSEMetrics captures every IncActive/Dec/Dispatch call.
type recordingSSEMetrics struct {
	mu         sync.Mutex
	connOpens  int
	connCloses int
	dispatched []string
	activeMax  int32
	current    int32
}

func (m *recordingSSEMetrics) IncActiveConnections(_ string) {
	m.mu.Lock()
	m.connOpens++
	m.current++
	if m.current > m.activeMax {
		m.activeMax = m.current
	}
	m.mu.Unlock()
}

func (m *recordingSSEMetrics) DecActiveConnections(_ string) {
	m.mu.Lock()
	m.connCloses++
	m.current--
	m.mu.Unlock()
}

func (m *recordingSSEMetrics) IncDispatchedEvents(_, et string) {
	m.mu.Lock()
	m.dispatched = append(m.dispatched, et)
	m.mu.Unlock()
}

func newSSEHarness(t *testing.T, subscriber AgentActivitySubscriber, heartbeat time.Duration, buffer int, metrics AgentActivitySSEMetrics) *AgentActivitySSEHandler {
	t.Helper()
	if heartbeat <= 0 {
		heartbeat = DefaultSSEHeartbeatInterval
	}
	if buffer <= 0 {
		buffer = DefaultSSEClientBufferSize
	}
	h, err := NewAgentActivitySSEHandler(nil, AgentActivitySSEHandlerConfig{
		Subscriber:        subscriber,
		HeartbeatInterval: heartbeat,
		BufferSize:        buffer,
		Metrics:           metrics,
		Now:               func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("NewAgentActivitySSEHandler: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	return h
}

// runStreamServer wires the SSE handler behind httptest, returns
// the URL + close function.
func runStreamServer(t *testing.T, h *AgentActivitySSEHandler) (string, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/api/v1/agent-activity/stream", h)
	srv := httptest.NewServer(mux)
	return srv.URL, srv.Close
}

// readUntil reads from r in a goroutine until either `marker` is
// found in the accumulated output or the supplied deadline elapses.
// Returns the accumulated string. The reader goroutine exits when
// the body closes (which happens via closeSrv / cancel in the
// caller).
func readUntil(t *testing.T, r io.Reader, marker string, timeout time.Duration) string {
	t.Helper()
	out := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
				if strings.Contains(sb.String(), marker) {
					out <- sb.String()
					return
				}
			}
			if err != nil {
				out <- sb.String()
				return
			}
		}
	}()
	select {
	case s := <-out:
		return s
	case <-time.After(timeout):
		return "<timeout>"
	}
}

// readForDuration reads from r for the supplied duration and
// returns the accumulated output. Used by the heartbeat + dropped
// tests where there's no single marker to look for.
func readForDuration(t *testing.T, r io.Reader, d time.Duration) string {
	t.Helper()
	out := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			n, err := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if err != nil {
				out <- sb.String()
				return
			}
		}
		out <- sb.String()
	}()
	select {
	case s := <-out:
		return s
	case <-time.After(d + time.Second):
		return "<timeout>"
	}
}

func TestSSEAgentActivity_StreamsEventToConnectedClient(t *testing.T) {
	t.Parallel()
	subscriber := &fakeSubscriber{}
	h := newSSEHarness(t, subscriber, time.Second, 8, nil)
	url, closeSrv := runStreamServer(t, h)
	defer closeSrv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/agent-activity/stream?tenant_id=tenant-A", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}

	// Wait for the subscription to be registered before emitting.
	for i := 0; i < 50; i++ {
		subscriber.mu.Lock()
		got := len(subscriber.handlers)
		subscriber.mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	emitted := time.Now()
	subscriber.emit(t, eventbus.Event{
		Type:      eventbus.PriceChangeApplied,
		TenantID:  "tenant-A",
		Timestamp: time.Now().UTC(),
		Payload:   map[string]any{"product_id": "p1", "delta_pct": 0.05},
	})

	// Read until we get the event line OR 2s elapses. Use a
	// goroutine so the slow Read doesn't block forever; the test's
	// deferred cancel + closeSrv unblocks the read.
	got := readUntil(t, resp.Body, "event: price.change.applied", 2*time.Second)
	if !strings.Contains(got, "event: price.change.applied") {
		t.Fatalf("did not receive event within 2s; got=%q", got)
	}
	if latency := time.Since(emitted); latency > time.Second {
		t.Fatalf("SSE latency = %s, want <= 1s", latency)
	}
	_ = io.Discard // keep import for downstream tests
}

func TestSSEAgentActivity_TenantIsolationEnforced(t *testing.T) {
	t.Parallel()
	subscriber := &fakeSubscriber{}
	h := newSSEHarness(t, subscriber, time.Second, 8, nil)
	url, closeSrv := runStreamServer(t, h)
	defer closeSrv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/agent-activity/stream?tenant_id=tenant-A", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	for i := 0; i < 50; i++ {
		subscriber.mu.Lock()
		got := len(subscriber.handlers)
		subscriber.mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	subscriber.emit(t, eventbus.Event{
		Type:      eventbus.PriceChangeApplied,
		TenantID:  "tenant-B",
		Timestamp: time.Now().UTC(),
		Payload:   map[string]any{"product_id": "leak"},
	})
	subscriber.emit(t, eventbus.Event{
		Type:      eventbus.PriceChangeApplied,
		TenantID:  "tenant-A",
		Timestamp: time.Now().UTC(),
		Payload:   map[string]any{"product_id": "ok"},
	})

	got := readUntil(t, resp.Body, "\"product_id\":\"ok\"", 2*time.Second)
	if !strings.Contains(got, "\"product_id\":\"ok\"") {
		t.Fatalf("missing tenant-A event; got=%q", got)
	}
	if strings.Contains(got, "\"product_id\":\"leak\"") {
		t.Fatalf("tenant isolation breached: tenant-B event leaked through")
	}
}

func TestSSEAgentActivity_HeartbeatSentEvery30s(t *testing.T) {
	t.Parallel()
	subscriber := &fakeSubscriber{}
	// Use a short heartbeat (50ms) so the test runs fast; the
	// production default is 30s.
	h := newSSEHarness(t, subscriber, 50*time.Millisecond, 8, nil)
	url, closeSrv := runStreamServer(t, h)
	defer closeSrv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/agent-activity/stream?tenant_id=tenant-A", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	got := readForDuration(t, resp.Body, 300*time.Millisecond)
	heartbeatCount := strings.Count(got, "event: heartbeat")
	if heartbeatCount < 2 {
		t.Fatalf("heartbeats = %d, want >= 2 (got=%q)", heartbeatCount, got)
	}
}

func TestSSEAgentActivity_DropsOldestOnBufferOverflow(t *testing.T) {
	t.Parallel()
	subscriber := &fakeSubscriber{}
	// Buffer size 2 + slow client (we don't read) → overflow after
	// 2 events.
	h := newSSEHarness(t, subscriber, time.Second, 2, nil)
	url, closeSrv := runStreamServer(t, h)
	defer closeSrv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/agent-activity/stream?tenant_id=tenant-A", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	for i := 0; i < 50; i++ {
		subscriber.mu.Lock()
		got := len(subscriber.handlers)
		subscriber.mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Emit 10 events back-to-back without reading. The handler's
	// dispatch loop needs scheduling time; fast-emit forces
	// overflow.
	for i := 0; i < 10; i++ {
		subscriber.emit(t, eventbus.Event{
			Type:      eventbus.PriceChangeApplied,
			TenantID:  "tenant-A",
			Timestamp: time.Now().UTC(),
			Payload:   map[string]any{"i": i},
		})
	}

	got := readUntil(t, resp.Body, "event: dropped", 1*time.Second)
	if !strings.Contains(got, "event: dropped") {
		t.Fatalf("no dropped notice in stream; got=%q", got)
	}
}

func TestSSEAgentActivity_DisconnectCleansUpSubscription(t *testing.T) {
	t.Parallel()
	subscriber := &fakeSubscriber{}
	h := newSSEHarness(t, subscriber, 50*time.Millisecond, 8, nil)
	url, closeSrv := runStreamServer(t, h)
	defer closeSrv()

	for round := 0; round < 3; round++ {
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/agent-activity/stream?tenant_id=tenant-A", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("round %d Get: %v", round, err)
		}
		buf := make([]byte, 64)
		_, _ = resp.Body.Read(buf) // consume something
		cancel()
		_ = resp.Body.Close()
	}
	// Sleep a touch so the handler goroutine returns; goleak in
	// TestMain catches any leak.
	time.Sleep(100 * time.Millisecond)
}

func TestSSEAgentActivity_RequiresTenant(t *testing.T) {
	t.Parallel()
	subscriber := &fakeSubscriber{}
	h := newSSEHarness(t, subscriber, time.Second, 8, nil)
	url, closeSrv := runStreamServer(t, h)
	defer closeSrv()

	resp, err := http.Get(url + "/api/v1/agent-activity/stream")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (no tenant)", resp.StatusCode)
	}
}

func TestSSEAgentActivity_RejectsClosed(t *testing.T) {
	t.Parallel()
	subscriber := &fakeSubscriber{}
	h := newSSEHarness(t, subscriber, time.Second, 8, nil)
	if err := h.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	url, closeSrv := runStreamServer(t, h)
	defer closeSrv()
	resp, err := http.Get(url + "/api/v1/agent-activity/stream?tenant_id=tenant-A")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestSSEAgentActivity_RejectsNonGet(t *testing.T) {
	t.Parallel()
	subscriber := &fakeSubscriber{}
	h := newSSEHarness(t, subscriber, time.Second, 8, nil)
	url, closeSrv := runStreamServer(t, h)
	defer closeSrv()
	resp, err := http.Post(url+"/api/v1/agent-activity/stream?tenant_id=tenant-A", "application/json", nil)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestNewAgentActivitySSEHandler_RejectsMissingSubscriber(t *testing.T) {
	t.Parallel()
	_, err := NewAgentActivitySSEHandler(nil, AgentActivitySSEHandlerConfig{})
	if err == nil {
		t.Fatalf("err = nil, want ErrSSEHandlerUnconfigured")
	}
	if err.Error() == "" {
		t.Fatalf("err message empty")
	}
}

func TestSSEAgentActivity_EmitsConnectionMetrics(t *testing.T) {
	t.Parallel()
	subscriber := &fakeSubscriber{}
	metrics := &recordingSSEMetrics{}
	h := newSSEHarness(t, subscriber, time.Second, 8, metrics)
	url, closeSrv := runStreamServer(t, h)
	defer closeSrv()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/agent-activity/stream?tenant_id=tenant-A", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	buf := make([]byte, 64)
	_, _ = resp.Body.Read(buf)
	cancel()
	_ = resp.Body.Close()

	// Wait briefly for the dec callback to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		metrics.mu.Lock()
		closed := metrics.connCloses
		metrics.mu.Unlock()
		if closed > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if metrics.connOpens != 1 {
		t.Fatalf("connOpens = %d, want 1", metrics.connOpens)
	}
	if metrics.connCloses != 1 {
		t.Fatalf("connCloses = %d, want 1", metrics.connCloses)
	}
}

func TestClassifyEvent_TableDriven(t *testing.T) {
	t.Parallel()
	cases := map[eventbus.EventType]string{
		eventbus.PriceChangeApplied:                 "pricing_agent",
		eventbus.PriceChangePendingApproval:         "pricing_agent",
		eventbus.SupplierCostChanged:                "supplier_cost_monitor",
		eventbus.OrderNormalised:                    "order_aggregator",
		eventbus.DropshipOrderPlaced:                "dropship_agent",
		eventbus.DropshipOrderRolledBack:            "dropship_agent",
		eventbus.LargeDropshipOrderPendingApproval:  "dropship_agent",
		eventbus.CustomerMessageReceived:            "customer_service",
		eventbus.CustomerMessageReplied:             "customer_service",
		eventbus.CustomerMessageEscalatedToOperator: "customer_service",
	}
	for et, wantAgent := range cases {
		got, _, _ := classifyEvent(et)
		if got != wantAgent {
			t.Errorf("classifyEvent(%s) agent = %s, want %s", et, got, wantAgent)
		}
	}
}

func TestSubscribedEventTypes_NoDuplicates(t *testing.T) {
	t.Parallel()
	seen := map[eventbus.EventType]bool{}
	for _, et := range SubscribedEventTypes() {
		if seen[et] {
			t.Fatalf("duplicate type %s", et)
		}
		seen[et] = true
	}
}

// TestSSEAgentActivity_HighFrequencyEventsNoLeak emits a burst and
// asserts no goroutine leak via goleak (TestMain).
func TestSSEAgentActivity_HighFrequencyEventsNoLeak(t *testing.T) {
	t.Parallel()
	subscriber := &fakeSubscriber{}
	h := newSSEHarness(t, subscriber, 50*time.Millisecond, 16, nil)
	url, closeSrv := runStreamServer(t, h)
	defer closeSrv()

	var wg sync.WaitGroup
	wg.Add(1)
	connected := atomic.Bool{}
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/agent-activity/stream?tenant_id=tenant-A", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("Get: %v", err)
			return
		}
		connected.Store(true)
		defer resp.Body.Close()
		buf := make([]byte, 1024)
		for {
			if _, err := resp.Body.Read(buf); err != nil {
				return
			}
		}
	}()
	for !connected.Load() {
		time.Sleep(5 * time.Millisecond)
	}
	for i := 0; i < 50; i++ {
		subscriber.emit(t, eventbus.Event{
			Type:      eventbus.PriceChangeApplied,
			TenantID:  "tenant-A",
			Timestamp: time.Now().UTC(),
			Payload:   map[string]any{"i": i},
		})
	}
	wg.Wait()
}

func TestResolveTenantIDForSSE(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		header    string
		query     string
		want      string
		wantError bool
	}{
		{"header_only", "tenant-A", "", "tenant-A", false},
		{"query_only", "", "tenant-B", "tenant-B", false},
		{"header_wins", "tenant-A", "tenant-B", "tenant-A", false},
		{"missing", "", "", "", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/?tenant_id=%s", tc.query), nil)
			if tc.header != "" {
				r.Header.Set("X-Tenant-Id", tc.header)
			}
			got, err := resolveTenantIDForSSE(r, "X-Tenant-Id")
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got = %s, want %s", got, tc.want)
			}
		})
	}
}
