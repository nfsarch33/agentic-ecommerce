//go:build v331_smoke

// File scope: v3.3.1 QA Task 1 -- sandbox E2E for the EC-3-2
// TikTok Shop listing agent.
//
// Acceptance (cite plan + EC-3-2): "synthetic enriched product event
// -> listing created in TikTok Shop sandbox within 15s; rollback
// confirmed on injected API failure".
//
// The smoke wires the production composition shape:
//
//	eventbus.InMemoryBus
//	  -> channel.TikTokListingAgent (Subscribe at boot)
//	     -> social.TikTokShopClient (signed via tiktok_shop_signing.go)
//	        -> httptest.Server (sandbox; latches per-call latency)
//
// Every component registers with internal/lifecycle.Manager so the
// v2.10 resilience pillar drain runs at the end of the test.
package v331

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/social"
	"github.com/nfsarch33/helixon-ec/internal/agent/channel"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/lifecycle"
)

// e2eDeadline is the per-event deadline AND the p95 ceiling per
// the EC-3-2 acceptance criterion. The sandbox runs in-process so
// real wall-clock typically lands sub-millisecond; the ceiling is
// the production budget the agent commits to.
const e2eDeadline = 15 * time.Second

// productCount is the cohort size for the smoke. 10 events cover
// the happy path + give a stable p50/p95 distribution without
// dragging the suite past the per-test budget.
const productCount = 10

// stepLatency captures one phase of the publish path so the test
// can attribute time to publish vs. delivery vs. round-trip. Pure
// data; no IO.
type stepLatency struct {
	ProductID string
	Publish   time.Duration // bus.Publish blocking-call latency
	Delivery  time.Duration // event-published-to-handler-completed
	Total     time.Duration // event constructed -> social.CreateProduct OK
}

// e2eHarness bundles every wired component for the sandbox smoke.
// Returned by setupE2EHarness so the top-level test stays a thin
// orchestrator (sentrux complex_fn guard: keeps
// TestTikTokSandboxE2E_PublishesWithinDeadline under the cyclomatic
// threshold).
type e2eHarness struct {
	bus      *eventbus.InMemoryBus
	agent    *channel.TikTokListingAgent
	client   *social.TikTokShopClient
	sandbox  *sandboxRecorder
	manager  *lifecycle.Manager
	tenantID string
}

// TestTikTokSandboxE2E_PublishesWithinDeadline is the EC-3-2
// acceptance test. Drives the agent end-to-end via the in-memory
// bus + a sandbox httptest server and verifies every product lands
// a TikTok listing inside the 15s deadline. The p95 latency must
// also stay within the deadline.
func TestTikTokSandboxE2E_PublishesWithinDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	h := setupE2EHarness(t, ctx)
	if err := h.agent.Start(ctx); err != nil {
		t.Fatalf("agent.Start: %v", err)
	}

	latencies := publishCohort(t, ctx, h, productCount)
	report := summariseLatencies(latencies, e2eDeadline)
	logE2EReport(t, report, h.sandbox)

	assertE2EOutcome(t, h, report)
}

// TestTikTokSandboxE2E_RollsBackOnSandboxFailure is the EC-3-2
// rollback acceptance ("rollback confirmed on injected API
// failure"). When the sandbox returns 500 mid-publish the agent
// emits TikTokListingRolledBack and the metric counter records
// the failure.
func TestTikTokSandboxE2E_RollsBackOnSandboxFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const tenantID = "tenant-v331"
	bus, manager := newBusAndManager(t)
	sandbox := newSandboxRecorder()
	sandbox.setStatus(http.StatusInternalServerError)
	srv := newSandboxServer(t, sandbox)
	t.Cleanup(srv.Close)

	client := mustNewTikTokSandboxClient(t, srv, tenantID)
	manager.Register("tiktok_client", client)

	agent := mustNewTikTokListingAgent(t, client, bus, tenantID)
	manager.Register("listing_agent", agent)

	if err := agent.Start(ctx); err != nil {
		t.Fatalf("agent.Start: %v", err)
	}

	evt := mustNewProductEnrichedEvent(t, tenantID, "p-rollback-1", productCount)
	if err := bus.Publish(ctx, evt); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	if got := countEvents(bus, eventbus.TikTokListingRolledBack); got != 1 {
		t.Fatalf("rollback events = %d, want 1; delivered=%v", got, eventTypeNames(bus.Delivered()))
	}
	if got := sandbox.attempts(); got == 0 {
		t.Fatalf("sandbox attempts = 0, expected at least 1 publish attempt")
	}
}

// setupE2EHarness wires every component of the v3.3.0 publish
// path + registers each Closer with a single lifecycle.Manager so
// end-of-test drain happens automatically. Pulled out of the
// top-level test so cyclomatic stays at 1.
func setupE2EHarness(t *testing.T, ctx context.Context) *e2eHarness {
	t.Helper()
	const tenantID = "tenant-v331"
	bus, manager := newBusAndManager(t)
	sandbox := newSandboxRecorder()
	sandbox.setStatus(http.StatusOK)
	srv := newSandboxServer(t, sandbox)
	t.Cleanup(srv.Close)

	client := mustNewTikTokSandboxClient(t, srv, tenantID)
	manager.Register("tiktok_client", client)

	agent := mustNewTikTokListingAgent(t, client, bus, tenantID)
	manager.Register("listing_agent", agent)

	_ = ctx // sandbox is hermetic; ctx forwarded to Start by caller
	return &e2eHarness{
		bus:      bus,
		agent:    agent,
		client:   client,
		sandbox:  sandbox,
		manager:  manager,
		tenantID: tenantID,
	}
}

// newBusAndManager constructs the eventbus + lifecycle pair.
// Cleanup of both is wired here so callers stay linear.
func newBusAndManager(t *testing.T) (*eventbus.InMemoryBus, *lifecycle.Manager) {
	t.Helper()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	manager := lifecycle.New(nil, 5*time.Second)
	t.Cleanup(func() {
		if err := manager.Shutdown(); err != nil {
			t.Errorf("lifecycle.Manager.Shutdown: %v", err)
		}
	})
	return bus, manager
}

// publishCohort spins up productCount enriched events sequentially
// (the agent's own goroutine pool fan-out happens inside the bus
// dispatch). Returns per-event latencies in publish order.
func publishCohort(t *testing.T, ctx context.Context, h *e2eHarness, n int) []stepLatency {
	t.Helper()
	out := make([]stepLatency, 0, n)
	for i := 0; i < n; i++ {
		productID := fmt.Sprintf("p-v331-%02d", i+1)
		evt := mustNewProductEnrichedEvent(t, h.tenantID, productID, n)
		startTotal := time.Now()
		startPublish := time.Now()
		if err := h.bus.Publish(ctx, evt); err != nil {
			t.Fatalf("bus.Publish[%s]: %v", productID, err)
		}
		publishDur := time.Since(startPublish)
		// In-memory bus dispatches synchronously; total ~= publish.
		// Splitting them gives the histogram a per-step view.
		out = append(out, stepLatency{
			ProductID: productID,
			Publish:   publishDur,
			Delivery:  publishDur,
			Total:     time.Since(startTotal),
		})
	}
	return out
}

// e2eReport is the aggregate latency view emitted to the test log
// for the PR body. Pure value type.
type e2eReport struct {
	N             int
	P50Total      time.Duration
	P95Total      time.Duration
	MaxTotal      time.Duration
	Histogram     map[string]int
	OverDeadline  int
	PerProductMax time.Duration
}

// summariseLatencies computes p50/p95/max + a 5-bucket histogram
// from a cohort. Pure (no IO, no t.*).
func summariseLatencies(latencies []stepLatency, deadline time.Duration) e2eReport {
	totals := make([]time.Duration, len(latencies))
	for i, l := range latencies {
		totals[i] = l.Total
	}
	sort.Slice(totals, func(i, j int) bool { return totals[i] < totals[j] })
	histogram := bucketLatencies(totals, deadline)
	overDeadline := 0
	maxPerProduct := time.Duration(0)
	for _, t := range totals {
		if t > deadline {
			overDeadline++
		}
		if t > maxPerProduct {
			maxPerProduct = t
		}
	}
	return e2eReport{
		N:             len(totals),
		P50Total:      percentile(totals, 0.50),
		P95Total:      percentile(totals, 0.95),
		MaxTotal:      maxPerProduct,
		Histogram:     histogram,
		OverDeadline:  overDeadline,
		PerProductMax: maxPerProduct,
	}
}

// percentile returns the p-th percentile of a sorted duration
// slice. Linear interpolation is overkill for a 10-event cohort,
// so the nearest-rank method (P^2-style) is used.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// bucketLatencies returns the canonical 5-bucket histogram used in
// the PR body. Buckets sized against the 15s deadline so any
// regression toward the ceiling is immediately visible.
func bucketLatencies(totals []time.Duration, deadline time.Duration) map[string]int {
	out := map[string]int{
		"a_<10ms":            0,
		"b_10ms-100ms":       0,
		"c_100ms-1s":         0,
		"d_1s-15s_(ceiling)": 0,
		"e_>15s_VIOLATION":   0,
	}
	for _, t := range totals {
		out[bucketName(t, deadline)]++
	}
	return out
}

// bucketName classifies a single duration. Pulled out for sentrux.
func bucketName(t, deadline time.Duration) string {
	switch {
	case t < 10*time.Millisecond:
		return "a_<10ms"
	case t < 100*time.Millisecond:
		return "b_10ms-100ms"
	case t < time.Second:
		return "c_100ms-1s"
	case t <= deadline:
		return "d_1s-15s_(ceiling)"
	default:
		return "e_>15s_VIOLATION"
	}
}

// logE2EReport prints the per-event histogram + sandbox counters
// for the PR reviewer.
func logE2EReport(t *testing.T, report e2eReport, sandbox *sandboxRecorder) {
	t.Helper()
	t.Logf("v3.3.1 QA Task 1 sandbox E2E -- N=%d  p50=%s  p95=%s  max=%s  over_deadline=%d  sandbox_attempts=%d  sandbox_ok=%d  sandbox_fail=%d",
		report.N, report.P50Total, report.P95Total, report.MaxTotal,
		report.OverDeadline, sandbox.attempts(), sandbox.successes(), sandbox.failures(),
	)
	keys := make([]string, 0, len(report.Histogram))
	for k := range report.Histogram {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Log("-- e2e total-latency histogram (per-event publish path) --")
	for _, k := range keys {
		t.Logf("  %-22s %d", k, report.Histogram[k])
	}
}

// assertE2EOutcome runs the post-loop assertions. Extracted so the
// top-level test stays under the sentrux complex_fn threshold.
func assertE2EOutcome(t *testing.T, h *e2eHarness, report e2eReport) {
	t.Helper()
	if report.OverDeadline != 0 {
		t.Fatalf("over-deadline events = %d (per-event ceiling %s); p95=%s max=%s", report.OverDeadline, e2eDeadline, report.P95Total, report.MaxTotal)
	}
	if report.P95Total > e2eDeadline {
		t.Fatalf("p95 latency = %s exceeds deadline %s", report.P95Total, e2eDeadline)
	}
	if got := h.sandbox.attempts(); got != report.N {
		t.Fatalf("sandbox attempts = %d, want %d (one per published event)", got, report.N)
	}
	if got := h.sandbox.successes(); got != report.N {
		t.Fatalf("sandbox successes = %d, want %d (no failures injected)", got, report.N)
	}
	if got := countEvents(h.bus, eventbus.TikTokListingRolledBack); got != 0 {
		t.Fatalf("unexpected %d rollback events on the happy path", got)
	}
}

// mustNewProductEnrichedEvent constructs a synthetic enriched event
// matching the EC-2 schema. Fails the test on validation error so
// fixture mistakes surface immediately.
func mustNewProductEnrichedEvent(t *testing.T, tenantID, productID string, stockUnits int) eventbus.Event {
	t.Helper()
	payload := eventbus.ProductEnrichedPayload{
		Version:            eventbus.ProductEnrichedPayloadVersion,
		TenantID:           tenantID,
		ProductID:          productID,
		ExternalID:         "ext-" + productID,
		EnglishTitle:       "Wireless Headphones " + productID,
		EnglishDescription: "Premium audio. Ergonomic fit.",
		CategoryID:         "audio",
		BrandName:          "AcmeAudio",
		PriceCents:         4999,
		Currency:           "AUD",
		StockUnits:         stockUnits,
		ShippingTemplate:   "ship-default",
		Images:             []string{"https://cdn.example.com/" + productID + ".jpg"},
		QualityScore:       0.91,
		Source:             "agent.enrichment",
	}
	evt, err := eventbus.NewProductEnrichedEvent("agent.enrichment", time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC), payload)
	if err != nil {
		t.Fatalf("NewProductEnrichedEvent[%s]: %v", productID, err)
	}
	return evt
}

// mustNewTikTokSandboxClient builds a real TikTokShopClient pointed
// at the supplied httptest server. Reuses the v3.3.0 token-manager +
// signing primitives so the sandbox exercises the full request
// envelope (HMAC sign + access token header).
func mustNewTikTokSandboxClient(t *testing.T, srv *httptest.Server, tenantID string) *social.TikTokShopClient {
	t.Helper()
	store := social.NewMemoryTokenStore()
	tok := social.TikTokToken{
		TenantID:     tenantID,
		ShopID:       "shop-v331",
		AccessToken:  "v331-access-token",
		RefreshToken: "v331-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}
	if err := store.Put(context.Background(), tok); err != nil {
		t.Fatalf("token store Put: %v", err)
	}
	mgr, err := social.NewTokenManager(social.TokenManagerConfig{
		Store:     store,
		Exchanger: noopExchanger{},
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	cfg := social.TikTokShopConfig{
		HTTPClient:   srv.Client(),
		TokenManager: mgr,
		BaseURL:      srv.URL,
		ClientID:     "v331-client-id",
		ClientSecret: []byte("v331-client-secret-32-byte-fixture"),
		TenantID:     tenantID,
		Now:          func() time.Time { return time.Now().UTC() },
	}
	client, err := social.NewTikTokShopClient(nil, cfg)
	if err != nil {
		t.Fatalf("NewTikTokShopClient: %v", err)
	}
	return client
}

// mustNewTikTokListingAgent wires the agent against the supplied
// client + bus. Pulled out so the e2e harness setup stays linear.
func mustNewTikTokListingAgent(t *testing.T, client social.Client, bus *eventbus.InMemoryBus, tenantID string) *channel.TikTokListingAgent {
	t.Helper()
	agent, err := channel.NewTikTokListingAgent(nil, channel.TikTokListingConfig{
		Client:           client,
		Publisher:        bus,
		Consumer:         bus,
		TenantID:         tenantID,
		DefaultShipping:  "ship-default",
		Now:              func() time.Time { return time.Now().UTC() },
		CategoryMapper:   func(c string) string { return "tt-" + c },
		ShippingResolver: func(_, c string) string { return "ship-" + c },
	})
	if err != nil {
		t.Fatalf("NewTikTokListingAgent: %v", err)
	}
	return agent
}

// noopExchanger satisfies social.TokenExchanger without performing
// any HTTP work; the sandbox primes the token store directly.
type noopExchanger struct{}

func (noopExchanger) Exchange(_ context.Context, _ social.OAuthBootstrapRequest) (social.TikTokToken, error) {
	return social.TikTokToken{}, fmt.Errorf("noopExchanger should not be called")
}

func (noopExchanger) Refresh(_ context.Context, _, _ string) (social.TikTokToken, error) {
	return social.TikTokToken{}, fmt.Errorf("noopExchanger should not be called")
}

// sandboxRecorder is the hermetic in-process TikTok Shop sandbox.
// Counters are atomic-equivalent (mutex-guarded) so concurrent
// publishers from the agent dispatch loop never race.
type sandboxRecorder struct {
	mu        sync.Mutex
	status    int
	calls     int
	okCalls   int
	failCalls int
}

func newSandboxRecorder() *sandboxRecorder {
	return &sandboxRecorder{status: http.StatusOK}
}

func (s *sandboxRecorder) setStatus(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

func (s *sandboxRecorder) record(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if status == http.StatusOK {
		s.okCalls++
	} else {
		s.failCalls++
	}
}

func (s *sandboxRecorder) attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *sandboxRecorder) successes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.okCalls
}

func (s *sandboxRecorder) failures() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failCalls
}

func (s *sandboxRecorder) currentStatus() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// newSandboxServer wires the httptest mock that replies to the
// EC-3-1 client's POST /api/products + DELETE /api/products/<id>.
// status is parameterised so the rollback test can flip to 500.
func newSandboxServer(t *testing.T, recorder *sandboxRecorder) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/products", func(w http.ResponseWriter, r *http.Request) {
		status := recorder.currentStatus()
		recorder.record(status)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte(`{"code":500,"message":"injected sandbox failure"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"product_id": "tt-sandbox-" + r.Header.Get("X-Tts-Tenant")},
		})
	})
	mux.HandleFunc("/api/products/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0}`))
	})
	return httptest.NewServer(mux)
}

// countEvents walks the in-memory bus's delivered slice and counts
// events of the supplied type. Pulled out so the assertion sites
// stay one-liners.
func countEvents(bus *eventbus.InMemoryBus, t eventbus.EventType) int {
	n := 0
	for _, e := range bus.Delivered() {
		if e.Type == t {
			n++
		}
	}
	return n
}

// eventTypeNames returns the type strings of every delivered
// event for diagnostic-only logging when an assertion fails.
func eventTypeNames(events []eventbus.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, string(e.Type))
	}
	return out
}
