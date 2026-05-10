// File scope: v3.9.1 EC-9-4 -- additional channel content handler
// coverage tests (parseChannelContentWindow + sortTopPerformers
// edge cases, NewChannelContentHandler error paths).
package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewChannelContentHandler_RequiresRepository(t *testing.T) {
	t.Parallel()
	if _, err := NewChannelContentHandler(nil, ChannelContentHandlerConfig{}); !errors.Is(err, ErrChannelContentHandlerUnconfigured) {
		t.Fatalf("expected ErrChannelContentHandlerUnconfigured, got %v", err)
	}
}

func TestParseChannelContentWindow_WithFromTo(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	q := map[string][]string{
		"from": {"2026-04-01"},
		"to":   {"2026-04-30"},
	}
	from, to, err := parseChannelContentWindow(q, now)
	if err != nil {
		t.Fatalf("parseChannelContentWindow err=%v", err)
	}
	if from.IsZero() || to.IsZero() {
		t.Fatalf("from/to not parsed: from=%v to=%v", from, to)
	}
}

func TestParseChannelContentWindow_FromToInverted(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	q := map[string][]string{
		"from": {"2026-04-30"},
		"to":   {"2026-04-01"},
	}
	if _, _, err := parseChannelContentWindow(q, now); err == nil {
		t.Fatal("expected error for inverted range")
	}
}

func TestParseChannelContentWindow_FromToWindowTooLarge(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	q := map[string][]string{
		"from": {"2024-01-01"},
		"to":   {"2026-05-10"},
	}
	if _, _, err := parseChannelContentWindow(q, now); err == nil {
		t.Fatal("expected error for window > MaxChannelContentWindowDays")
	}
}

func TestParseChannelContentWindow_OnlyFromOrTo(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	if _, _, err := parseChannelContentWindow(map[string][]string{"from": {"2026-04-01"}}, now); err == nil {
		t.Fatal("expected error for only from")
	}
	if _, _, err := parseChannelContentWindow(map[string][]string{"to": {"2026-04-01"}}, now); err == nil {
		t.Fatal("expected error for only to")
	}
}

func TestParseChannelContentWindow_BadFromOrTo(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	if _, _, err := parseChannelContentWindow(map[string][]string{"from": {"not-a-date"}, "to": {"2026-04-30"}}, now); err == nil {
		t.Fatal("expected error for bad from")
	}
	if _, _, err := parseChannelContentWindow(map[string][]string{"from": {"2026-04-01"}, "to": {"not-a-date"}}, now); err == nil {
		t.Fatal("expected error for bad to")
	}
}

func TestSortTopPerformers_CapsAtLimit(t *testing.T) {
	t.Parallel()
	rows := []ChannelContentTopPerformer{
		{Channel: "tiktok", TotalEngagement: 100},
		{Channel: "facebook", TotalEngagement: 200},
		{Channel: "rednote", TotalEngagement: 50},
	}
	out := sortTopPerformers(rows, 1)
	if len(out) != 1 {
		t.Fatalf("limit not applied: got %d", len(out))
	}
	if out[0].Channel != "facebook" {
		t.Fatalf("first=%q want=facebook", out[0].Channel)
	}
}

func TestSortTopPerformers_LimitZeroReturnsAll(t *testing.T) {
	t.Parallel()
	rows := []ChannelContentTopPerformer{
		{Channel: "tiktok", TotalEngagement: 100},
		{Channel: "facebook", TotalEngagement: 200},
	}
	out := sortTopPerformers(rows, 0)
	if len(out) != 2 {
		t.Fatalf("expected all 2 rows, got %d", len(out))
	}
}

func TestChannelContentHandler_NoMetricsConfig(t *testing.T) {
	t.Parallel()
	repo := &stubChannelContentRepo{}
	h, err := NewChannelContentHandler(nil, ChannelContentHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewChannelContentHandler: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no metrics, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChannelContentHandler_TopRepositoryError(t *testing.T) {
	t.Parallel()
	h := newChannelContentHarness(t, &stubChannelContentRepo{err: errors.New("boom")})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content/top?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestChannelContentHandler_TopWithInvalidLimit(t *testing.T) {
	t.Parallel()
	h := newChannelContentHarness(t, &stubChannelContentRepo{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channel-content/top?tenant_id=tenant-1&period=30d&limit=abc", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid limit, got %d", rec.Code)
	}
}

func TestChannelContentHandler_TopLimitCappedAtMax(t *testing.T) {
	t.Parallel()
	h := newChannelContentHarness(t, &stubChannelContentRepo{})
	url := "/api/v1/analytics/channel-content/top?tenant_id=tenant-1&period=30d&limit=" + intToStrLocal(MaxTopPerformersLimit+50)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with capped limit, got %d", rec.Code)
	}
}

// intToStrLocal mirrors the strconv.Itoa contract without pulling
// strconv into this file (the test suite already uses a local
// formatter for the onboarding tests).
func intToStrLocal(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
