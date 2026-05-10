// File scope: v3.9.1 EC-9-4 channel content analytics handler RED
// tests + bench acceptance test (p95 <300ms over 30-day window).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stubChannelContentRepo struct {
	mu          sync.Mutex
	rows        []ChannelContentRow
	top         []ChannelContentTopPerformer
	err         error
	calls       atomic.Int32
	lastChannel string
	lastTenant  string
}

func (r *stubChannelContentRepo) ByChannel(_ context.Context, filter ChannelContentFilter) ([]ChannelContentRow, error) {
	r.calls.Add(1)
	r.mu.Lock()
	r.lastChannel = filter.Channel
	r.lastTenant = filter.TenantID
	r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	if filter.Channel == "" {
		return r.rows, nil
	}
	out := make([]ChannelContentRow, 0, len(r.rows))
	for _, row := range r.rows {
		if row.Channel == filter.Channel {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *stubChannelContentRepo) TopPerformers(_ context.Context, filter ChannelContentFilter) ([]ChannelContentTopPerformer, error) {
	r.calls.Add(1)
	r.mu.Lock()
	r.lastTenant = filter.TenantID
	r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	out := make([]ChannelContentTopPerformer, len(r.top))
	copy(out, r.top)
	return out, nil
}

func newChannelContentHarness(t *testing.T, repo *stubChannelContentRepo) *ChannelContentHandler {
	t.Helper()
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	h, err := NewChannelContentHandler(nil, ChannelContentHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return clk },
	})
	if err != nil {
		t.Fatalf("NewChannelContentHandler: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	return h
}

func TestChannelContent_AggregatesPerChannel(t *testing.T) {
	t.Parallel()
	repo := &stubChannelContentRepo{
		rows: []ChannelContentRow{
			{Day: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Channel: "tiktok", ContentType: "video", PostCount: 5, TotalEngagement: 1000, ConversionCount: 10, GMVAttributionCents: 50000},
			{Day: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), Channel: "tiktok", ContentType: "video", PostCount: 4, TotalEngagement: 800, ConversionCount: 8, GMVAttributionCents: 40000},
			{Day: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Channel: "rednote", ContentType: "post", PostCount: 3, TotalEngagement: 600, ConversionCount: 5, GMVAttributionCents: 25000},
		},
	}
	h := newChannelContentHarness(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content?tenant_id=tenant-1&channel=tiktok&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		TenantID string              `json:"tenant_id"`
		Rows     []ChannelContentRow `json:"rows"`
		Summary  map[string]any      `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.TenantID != "tenant-1" {
		t.Fatalf("tenant_id=%q want=tenant-1", out.TenantID)
	}
	if len(out.Rows) != 2 {
		t.Fatalf("rows=%d want=2 (tiktok-only)", len(out.Rows))
	}
	for _, row := range out.Rows {
		if row.Channel != "tiktok" {
			t.Fatalf("row channel=%q want=tiktok", row.Channel)
		}
	}
	if got := out.Summary["post_count"]; got != float64(9) {
		t.Fatalf("summary.post_count=%v want=9", got)
	}
}

func TestChannelContent_TopPerformersOrderingCorrect(t *testing.T) {
	t.Parallel()
	repo := &stubChannelContentRepo{
		top: []ChannelContentTopPerformer{
			{Channel: "tiktok", ContentType: "video", PostCount: 5, TotalEngagement: 100, ConversionRate: 0.05, GMVAttribution: 50000},
			{Channel: "rednote", ContentType: "post", PostCount: 4, TotalEngagement: 500, ConversionRate: 0.10, GMVAttribution: 80000},
			{Channel: "facebook", ContentType: "video", PostCount: 3, TotalEngagement: 200, ConversionRate: 0.03, GMVAttribution: 25000},
			{Channel: "instagram", ContentType: "reel", PostCount: 2, TotalEngagement: 500, ConversionRate: 0.07, GMVAttribution: 60000},
		},
	}
	h := newChannelContentHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content/top?tenant_id=tenant-1&period=30d&limit=3", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		TopPerformers []ChannelContentTopPerformer `json:"top_performers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.TopPerformers) != 3 {
		t.Fatalf("got %d top performers, want=3", len(out.TopPerformers))
	}
	// First place should be rednote (engagement=500, post_count=4).
	if out.TopPerformers[0].Channel != "rednote" {
		t.Fatalf("first=%q want=rednote", out.TopPerformers[0].Channel)
	}
	// Engagement-tie deterministic: instagram (engagement=500, post_count=2) places second.
	if out.TopPerformers[1].Channel != "instagram" {
		t.Fatalf("second=%q want=instagram", out.TopPerformers[1].Channel)
	}
	// Engagements descending overall (allow ties; assert via direct
	// pairwise check).
	for i := 0; i < len(out.TopPerformers)-1; i++ {
		if out.TopPerformers[i].TotalEngagement < out.TopPerformers[i+1].TotalEngagement {
			t.Fatalf("top performers not sorted descending at index %d: %+v", i, out.TopPerformers)
		}
	}
}

func TestChannelContent_TenantIsolationEnforced(t *testing.T) {
	t.Parallel()
	repo := &stubChannelContentRepo{}
	h := newChannelContentHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content?period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when tenant missing, got %d body=%s", rec.Code, rec.Body.String())
	}

	// With X-Tenant-Id header (set by JWT middleware in production)
	// the handler MUST prefer the header over the query string so a
	// caller cannot impersonate another tenant.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content?tenant_id=tenant-evil&period=30d", nil)
	req2.Header.Set("X-Tenant-Id", "tenant-bob")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 with header tenant, got %d body=%s", rec2.Code, rec2.Body.String())
	}
	if got := repo.lastTenant; got != "tenant-bob" {
		t.Fatalf("repo saw tenant=%q want=tenant-bob (header wins over query)", got)
	}
}

func TestChannelContent_PerformanceP95Under300ms(t *testing.T) {
	t.Parallel()
	// Build 30 days of fixtures across 4 channels so the handler
	// exercises the full materialised-view shape; each row matches
	// the rollup contract in 0024.
	rows := make([]ChannelContentRow, 0, 4*30)
	channels := []string{"tiktok", "rednote", "facebook", "instagram"}
	for d := 0; d < 30; d++ {
		day := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(d) * 24 * time.Hour)
		for _, c := range channels {
			rows = append(rows, ChannelContentRow{
				Day:                 day,
				Channel:             c,
				ContentType:         "video",
				PostCount:           10,
				TotalEngagement:     float64(100 * (d + 1)),
				ConversionCount:     5,
				ConversionRate:      0.05,
				GMVAttributionCents: int64(1000 * (d + 1)),
			})
		}
	}
	repo := &stubChannelContentRepo{rows: rows}
	h := newChannelContentHarness(t, repo)

	const samples = 200
	durations := make([]time.Duration, samples)
	for i := 0; i < samples; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content?tenant_id=tenant-1&period=30d", nil)
		rec := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(rec, req)
		durations[i] = time.Since(start)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[int(0.95*float64(samples))-1]
	if p95 > 300*time.Millisecond {
		t.Fatalf("p95=%s exceeds budget of 300ms", p95)
	}
}

func TestChannelContent_RepositoryErrorReturns500(t *testing.T) {
	t.Parallel()
	repo := &stubChannelContentRepo{err: errors.New("db unreachable")}
	h := newChannelContentHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChannelContent_InvalidChannelFilterReturns400(t *testing.T) {
	t.Parallel()
	repo := &stubChannelContentRepo{}
	h := newChannelContentHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content?tenant_id=tenant-1&channel=myspace&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid channel, got %d", rec.Code)
	}
	if !errors.Is(asError(rec.Body.Bytes()), ErrInvalidChannelFilter) && !contains(rec.Body.String(), "invalid channel filter") {
		// The handler returns a JSON error body; we accept either
		// a wrapped error string or the typed sentinel marker.
		t.Fatalf("expected ErrInvalidChannelFilter marker; body=%s", rec.Body.String())
	}
}

func TestChannelContent_InvalidPeriodReturns400(t *testing.T) {
	t.Parallel()
	repo := &stubChannelContentRepo{}
	h := newChannelContentHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content?tenant_id=tenant-1&period=42d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChannelContent_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := newChannelContentHarness(t, &stubChannelContentRepo{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/channel-content?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestChannelContent_UnknownRouteReturns404(t *testing.T) {
	t.Parallel()
	h := newChannelContentHarness(t, &stubChannelContentRepo{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content/unknown?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestChannelContent_ClosedReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()
	h := newChannelContentHarness(t, &stubChannelContentRepo{})
	if err := h.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// asError extracts a typed sentinel from a JSON {error: ...} body
// so tests can branch via errors.Is. Returns nil if no marker found.
func asError(body []byte) error {
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil
	}
	if contains(env.Error, "invalid channel filter") {
		return ErrInvalidChannelFilter
	}
	return nil
}

func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && stringIndex(s, substr) >= 0
}

func stringIndex(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
