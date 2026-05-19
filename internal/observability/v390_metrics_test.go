package observability

import (
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

func newV390Harness(t *testing.T) (*metrics.Registry, *V390Metrics) {
	t.Helper()
	r := metrics.NewRegistry("v390-test")
	m := NewV390Metrics(r)
	if m == nil {
		t.Fatalf("NewV390Metrics returned nil")
	}
	return r, m
}

func TestV390Metrics_RecordsCompetitorObservation(t *testing.T) {
	t.Parallel()
	reg, m := newV390Harness(t)
	m.RecordCompetitorObservation("tenant-1", "tiktok", "true")
	out := scrape(t, reg)
	if !strings.Contains(out, "ec_competitor_prices_observed_total") || !strings.Contains(out, "tenant-1") {
		t.Fatalf("expected observation in scrape: %s", out)
	}
}

func TestV390Metrics_ObservesMarginDashboardDuration(t *testing.T) {
	t.Parallel()
	reg, m := newV390Harness(t)
	m.ObserveMarginDashboardDuration(0.05)
	out := scrape(t, reg)
	if !strings.Contains(out, "ec_margin_dashboard_request_duration_seconds_bucket") {
		t.Fatalf("expected histogram in scrape: %s", out)
	}
}

func TestV390Metrics_RecordsCalendarEntry(t *testing.T) {
	t.Parallel()
	reg, m := newV390Harness(t)
	m.RecordCalendarEntry("tenant-1", "tiktok", "scheduled")
	m.ObserveCalendarPublishingDuration("tenant-1", "tiktok", 1.2)
	out := scrape(t, reg)
	if !strings.Contains(out, "ec_content_calendar_entries_total") {
		t.Fatalf("missing calendar counter: %s", out)
	}
	if !strings.Contains(out, "ec_content_calendar_publishing_duration_seconds") {
		t.Fatalf("missing calendar duration histogram: %s", out)
	}
}

func TestV390Metrics_RecordsHashtagGeneration(t *testing.T) {
	t.Parallel()
	reg, m := newV390Harness(t)
	m.RecordHashtagGeneration("tenant-1", "tiktok", "llm")
	out := scrape(t, reg)
	if !strings.Contains(out, "ec_hashtag_caption_generations_total") {
		t.Fatalf("missing hashtag generation counter: %s", out)
	}
}

func TestV390Metrics_SetsContentEMAScore(t *testing.T) {
	t.Parallel()
	reg, m := newV390Harness(t)
	m.SetContentEMAScore("tenant-1", "tiktok", "video", 75.5)
	m.RecordContentEMAUpdate("tenant-1", "tiktok")
	out := scrape(t, reg)
	if !strings.Contains(out, "ec_content_ema_score") {
		t.Fatalf("missing ema score gauge: %s", out)
	}
	if !strings.Contains(out, "ec_content_ema_updates_total") {
		t.Fatalf("missing ema updates counter: %s", out)
	}
}

func TestV390Metrics_RecordsChannelStatusUpdate(t *testing.T) {
	t.Parallel()
	reg, m := newV390Harness(t)
	m.RecordChannelStatusUpdate("tenant-1", "tiktok", "ok")
	m.RecordChannelStatusUpdate("tenant-1", "tiktok", "failed")
	out := scrape(t, reg)
	if !strings.Contains(out, "ec_channel_status_updates_total") {
		t.Fatalf("missing channel status updates: %s", out)
	}
	if !strings.Contains(out, `outcome="ok"`) || !strings.Contains(out, `outcome="failed"`) {
		t.Fatalf("expected ok+failed labels in scrape: %s", out)
	}
}

func TestV390Metrics_NilSafe(t *testing.T) {
	t.Parallel()
	var m *V390Metrics
	// All methods MUST be nil-safe (the v340/v350/v360/v380 facades
	// share this contract).
	m.RecordCompetitorObservation("a", "b", "c")
	m.ObserveMarginDashboardDuration(0)
	m.RecordCalendarEntry("a", "b", "c")
	m.ObserveCalendarPublishingDuration("a", "b", 0)
	m.RecordHashtagGeneration("a", "b", "c")
	m.SetContentEMAScore("a", "b", "c", 0)
	m.RecordContentEMAUpdate("a", "b")
	m.ObserveContentEMAUpdateDuration(0)
	m.RecordChannelStatusUpdate("a", "b", "c")
}

func TestV390Metrics_RegistryAccessor(t *testing.T) {
	t.Parallel()
	reg, m := newV390Harness(t)
	if m.Registry() != reg {
		t.Fatalf("expected same registry pointer")
	}
}
