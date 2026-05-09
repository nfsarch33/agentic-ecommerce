package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRegistryEmitsAllMetrics(t *testing.T) {
	t.Parallel()
	r := NewRegistry("mc-api")
	r.HTTPRequests.Inc(Labels{"tenant_id": "t1", "route": "/api/v1/products", "method": "GET", "status": "200"})
	r.HTTPDuration.Observe(0.05, Labels{"route": "/api/v1/products", "method": "GET"})
	r.WorkflowRuns.Inc(Labels{"workflow": "OnboardTenant", "status": "success"})
	r.WorkflowDuration.Observe(1.2, Labels{"workflow": "OnboardTenant"})
	r.WorkerpoolQueued.Set(3, Labels{"pool": "agent"})
	r.WorkerpoolSaturation.Inc(Labels{"pool": "agent"})
	r.OOMAlarms.Inc(Labels{})
	r.GoroutineCount.Set(42, Labels{})
	r.HeapBytes.Set(1024, Labels{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"ec_http_requests_total",
		"ec_http_duration_seconds",
		"ec_workflow_runs_total",
		"ec_workflow_duration_seconds",
		"ec_workerpool_queued",
		"ec_workerpool_saturation_total",
		"ec_oom_alarms_total",
		"ec_goroutine_count",
		"ec_heap_bytes",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q\n%s", want, body)
		}
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type=%q", got)
	}
}

func TestCounterRejectsNegative(t *testing.T) {
	t.Parallel()
	r := NewRegistry("mc-api")
	r.HTTPRequests.Add(-3, Labels{"tenant_id": "t1", "route": "/x", "method": "GET", "status": "200"})
	body := scrape(t, r)
	if !strings.Contains(body, `ec_http_requests_total{binary="mc-api",method="GET",route="/x",status="200",tenant_id="t1"} 0`) {
		t.Errorf("expected zero counter, got\n%s", body)
	}
}

func TestHandlerRejectsNonGet(t *testing.T) {
	t.Parallel()
	r := NewRegistry("mc-api")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	r.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Code=%d, want 405", rec.Code)
	}
}

func TestLabelCardinalityBoundedByConfig(t *testing.T) {
	t.Parallel()
	r := NewRegistry("mc-api", WithMaxSeries(2))
	r.HTTPRequests.Inc(Labels{"tenant_id": "t1", "route": "/r1", "method": "GET", "status": "200"})
	r.HTTPRequests.Inc(Labels{"tenant_id": "t2", "route": "/r2", "method": "GET", "status": "200"})
	r.HTTPRequests.Inc(Labels{"tenant_id": "t3", "route": "/r3", "method": "GET", "status": "200"})
	body := scrape(t, r)
	// MaxSeries cap is per-counter; expect drop counter visible.
	if !strings.Contains(body, "ec_metrics_series_dropped_total") {
		t.Errorf("expected dropped counter, got\n%s", body)
	}
}

// TestChannelHealthMetricsRender is the v3.4.1 EC-4-5 metrics-side
// gate. The handler MUST emit the five new ec_channel_health_*
// series so the Prometheus alert rule + Grafana panel can scrape
// them.
func TestChannelHealthMetricsRender(t *testing.T) {
	t.Parallel()
	r := NewRegistry("mc-api")
	r.ChannelHealthState.Set(2, Labels{"tenant_id": "t1", "channel": "tiktok"})
	r.ChannelHealthFailureRate.Set(0.06, Labels{"tenant_id": "t1", "channel": "tiktok"})
	r.ChannelHealthConsecutiveFailures.Set(4, Labels{"tenant_id": "t1", "channel": "tiktok"})
	r.ChannelHealthAlertsTotal.Inc(Labels{"tenant_id": "t1", "channel": "tiktok", "state": "unhealthy"})
	r.ChannelHealthRecoveriesTotal.Inc(Labels{"tenant_id": "t1", "channel": "tiktok"})
	body := scrape(t, r)
	for _, want := range []string{
		"ec_channel_health_state",
		"ec_channel_health_failure_rate",
		"ec_channel_health_consecutive_failures",
		"ec_channel_health_alerts_total",
		"ec_channel_health_recoveries_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q\n%s", want, body)
		}
	}
}

func TestHistogramSumAndCount(t *testing.T) {
	t.Parallel()
	r := NewRegistry("mc-api")
	r.HTTPDuration.Observe(0.05, Labels{"route": "/x", "method": "GET"})
	r.HTTPDuration.Observe(0.5, Labels{"route": "/x", "method": "GET"})
	r.HTTPDuration.Observe(2.0, Labels{"route": "/x", "method": "GET"})
	body := scrape(t, r)
	if !strings.Contains(body, "ec_http_duration_seconds_count{") {
		t.Errorf("missing _count, got\n%s", body)
	}
	if !strings.Contains(body, "ec_http_duration_seconds_sum{") {
		t.Errorf("missing _sum, got\n%s", body)
	}
}

func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}
