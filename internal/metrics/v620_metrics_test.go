package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestV620ResilienceMetricsExposed locks in that the new memwatch v3
// + circuit breaker metrics surface on /metrics so dashboards can
// pivot on them without further cmd/* wiring changes.
func TestV620ResilienceMetricsExposed(t *testing.T) {
	t.Parallel()
	r := NewRegistry("mc-api")
	r.WorkerpoolActive.Set(3, Labels{"pool": "agent"})
	r.WorkerpoolRejected.Inc(Labels{"pool": "agent"})
	r.BreakerOpenTotal.Inc(Labels{"name": "stripe"})
	r.BreakerHalfOpenTotal.Inc(Labels{"name": "stripe"})
	r.CoordConflictsTotal.Inc(Labels{
		"tenant_id":  "tenant-1",
		"agent_a":    "PricingAgent",
		"agent_b":    "FulfilmentAgent",
		"resolution": "last_write_wins",
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		"ec_workerpool_active",
		"ec_workerpool_rejected_total",
		"ec_breaker_open_total",
		"ec_breaker_half_open_total",
		"ec_coord_conflicts_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing %q\n--- body ---\n%s", want, body)
		}
	}
}
