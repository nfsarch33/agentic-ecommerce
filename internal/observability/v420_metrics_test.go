package observability_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/metrics"
	"github.com/nfsarch33/helixon-ec/internal/observability"
)

func TestV420MetricsPaymentCharges(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry("test")
	m := observability.NewV420Metrics(reg)

	m.RecordCharge("tenant-a", "stripe", "succeeded")
	m.RecordCharge("tenant-a", "alipay", "failed")
	m.RecordCharge("tenant-b", "wechat", "succeeded")
	m.ObserveChargeDuration("stripe", 0.5)
	m.RecordRefund("tenant-a", "stripe", "succeeded")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reg.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	for _, want := range []string{
		"ec_payment_charges_total",
		"ec_payment_charge_duration_seconds",
		"ec_payment_refunds_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q", want)
		}
	}
}

func TestV420MetricsNilSafe(t *testing.T) {
	t.Parallel()
	var m *observability.V420Metrics
	m.RecordCharge("t", "stripe", "ok")
	m.ObserveChargeDuration("stripe", 1.0)
	m.RecordRefund("t", "stripe", "ok")
}
