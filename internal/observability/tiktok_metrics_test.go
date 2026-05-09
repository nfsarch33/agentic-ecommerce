package observability

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

func TestTikTokMetrics_NilSafe(t *testing.T) {
	t.Parallel()
	var m *TikTokMetrics
	m.RecordAPICall("t", "endpoint", "ok", 0.1)
	m.RecordListing("t", "published")
	m.RecordWebhook("t", "order", "ok")
	m.RecordSignatureFailure("t", "missing")
	m.RecordInventorySync("t", "wc_to_tiktok", "ok")
	// Should not panic.
}

func TestTikTokMetrics_EmitsRegistrySeries(t *testing.T) {
	t.Parallel()
	r := metrics.NewRegistry("test-binary")
	m := NewTikTokMetrics(r)
	m.RecordAPICall("tenant-1", "products.list", "ok", 0.05)
	m.RecordListing("tenant-1", "published")
	m.RecordWebhook("tenant-1", "order", "ok")
	m.RecordSignatureFailure("tenant-1", "missing")
	m.RecordInventorySync("tenant-1", "wc_to_tiktok", "ok")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	r.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	cases := []string{
		"ec_tiktok_api_calls_total",
		"ec_tiktok_api_duration_seconds",
		"ec_tiktok_listing_attempts_total",
		"ec_tiktok_webhook_received_total",
		"ec_tiktok_signature_failures_total",
		"ec_tiktok_inventory_sync_total",
	}
	for _, name := range cases {
		if !strings.Contains(body, name) {
			t.Errorf("/metrics missing %s", name)
		}
	}
}

func TestTikTokMetrics_NoCollisionWithEnrichment(t *testing.T) {
	t.Parallel()
	r := metrics.NewRegistry("test-binary")
	m := NewTikTokMetrics(r)
	m.RecordAPICall("t", "x", "ok", 0)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	r.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "research_scraper_scrape_total") {
		t.Fatalf("namespace collision present")
	}
}

func TestTikTokMetrics_ZeroDurationSkipsHistogram(t *testing.T) {
	t.Parallel()
	r := metrics.NewRegistry("test-binary")
	m := NewTikTokMetrics(r)
	m.RecordAPICall("t", "x", "ok", 0)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	r.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "ec_tiktok_api_duration_seconds_count") {
		t.Fatalf("histogram should be empty when only zero-duration observed")
	}
}

func TestTikTokMetrics_RegistryAccessor(t *testing.T) {
	t.Parallel()
	r := metrics.NewRegistry("test-binary")
	m := NewTikTokMetrics(r)
	if m.Registry() != r {
		t.Fatalf("Registry accessor mismatch")
	}
}
