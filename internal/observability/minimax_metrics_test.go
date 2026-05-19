package observability_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/metrics"
	"github.com/nfsarch33/helixon-ec/internal/observability"
)

func TestMinimaxMetricsIncrementOnRequest(t *testing.T) {
	t.Parallel()

	reg := metrics.NewRegistry("test")
	m := observability.NewMinimaxMetrics(reg)
	m.RecordRequest("1", "success", 0.5)
	m.RecordRequest("1", "success", 0.3)
	m.RecordRequest("2", "rate_limited", 1.0)

	body := scrapeMetrics(t, reg)
	if !strings.Contains(body, "ec_minimax_requests_total") {
		t.Fatal("expected ec_minimax_requests_total in /metrics output")
	}
	if !strings.Contains(body, `key_alias="1"`) {
		t.Fatal("expected key_alias=1 label in /metrics output")
	}
}

func TestMinimaxMetricsFailoverEventLogged(t *testing.T) {
	t.Parallel()

	reg := metrics.NewRegistry("test")
	m := observability.NewMinimaxMetrics(reg)
	m.RecordFailover("1", "2")

	body := scrapeMetrics(t, reg)
	if !strings.Contains(body, "ec_minimax_failover_events_total") {
		t.Fatal("expected ec_minimax_failover_events_total in /metrics output")
	}
	if !strings.Contains(body, `from_key="1"`) {
		t.Fatal("expected from_key=1 in /metrics output")
	}
}

func TestMinimaxMetricsCooldownTimerTracks(t *testing.T) {
	t.Parallel()

	reg := metrics.NewRegistry("test")
	m := observability.NewMinimaxMetrics(reg)
	m.SetCooldownRemaining("1", 3500.0)

	body := scrapeMetrics(t, reg)
	if !strings.Contains(body, "ec_minimax_key_cooldown_remaining_seconds") {
		t.Fatal("expected cooldown metric in /metrics output")
	}
	if !strings.Contains(body, "3500") {
		t.Fatal("expected cooldown value 3500 in /metrics output")
	}
}

func TestMinimaxMetricsActiveKeyUpdates(t *testing.T) {
	t.Parallel()

	reg := metrics.NewRegistry("test")
	m := observability.NewMinimaxMetrics(reg)
	m.SetActiveKey("2")

	body := scrapeMetrics(t, reg)
	if !strings.Contains(body, "ec_minimax_active_key") {
		t.Fatal("expected ec_minimax_active_key in /metrics output")
	}
}

func scrapeMetrics(t *testing.T, reg *metrics.Registry) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reg.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d", w.Code)
	}
	return w.Body.String()
}
