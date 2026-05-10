package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

func TestV550Metrics_PGPoolMetricsEmit(t *testing.T) {
	reg := metrics.NewRegistry("test-v550")
	m := NewV550Metrics(reg)

	m.SetOpenConnections(25)
	m.SetIdleConnections(10)
	m.RecordPoolWait()
	m.RecordPoolWait()
	m.ObservePoolWaitDuration(0.005)
	m.ObservePoolWaitDuration(0.012)

	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	checks := []string{
		"ec_pg_pool_open_connections",
		"ec_pg_pool_idle_connections",
		"ec_pg_pool_wait_total",
		"ec_pg_pool_wait_duration_seconds",
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestV550Metrics_NilSafe(t *testing.T) {
	var m *V550Metrics
	m.SetOpenConnections(1)
	m.SetIdleConnections(1)
	m.RecordPoolWait()
	m.ObservePoolWaitDuration(0.1)
}

func TestV550Metrics_NilRegistry(t *testing.T) {
	m := &V550Metrics{}
	m.SetOpenConnections(1)
	m.SetIdleConnections(1)
	m.RecordPoolWait()
	m.ObservePoolWaitDuration(0.1)
}
