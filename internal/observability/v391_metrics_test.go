// File scope: v3.9.1 V391Metrics facade RED tests.
package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/alert"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

func TestV391Metrics_RecordWizardStepCompleted(t *testing.T) {
	t.Parallel()
	registry := metrics.NewRegistry("test")
	v := NewV391Metrics(registry)
	v.RecordWizardStepCompleted("tenant-1", 1)
	v.RecordWizardStepCompleted("tenant-1", 2)
	v.RecordWizardStepCompleted("tenant-2", 1)
	rec := metricsText(t, registry)
	if !strings.Contains(rec, "ec_onboarding_wizard_steps_completed_total") {
		t.Fatalf("counter not exposed: %s", rec)
	}
	if !strings.Contains(rec, `step="1"`) {
		t.Fatalf("step label missing: %s", rec)
	}
}

func TestV391Metrics_RecordOperatorAlert(t *testing.T) {
	t.Parallel()
	registry := metrics.NewRegistry("test")
	v := NewV391Metrics(registry)
	v.RecordOperatorAlert("tenant-1", alert.TypeLargeRefund, alert.StatusPending)
	v.RecordOperatorAlert("tenant-1", alert.TypeLargeRefund, alert.StatusResolved)
	v.RecordOperatorAlert("tenant-1", alert.TypePriceChange, alert.StatusAcknowledged)
	rec := metricsText(t, registry)
	for _, want := range []string{
		`alert_type="large_refund_pending_approval"`,
		`alert_type="price_change_pending_approval"`,
		`status="resolved"`,
		`status="acknowledged"`,
	} {
		if !strings.Contains(rec, want) {
			t.Fatalf("expected %q in metrics output: %s", want, rec)
		}
	}
}

func TestV391Metrics_RecordStubChannelCall(t *testing.T) {
	t.Parallel()
	registry := metrics.NewRegistry("test")
	v := NewV391Metrics(registry)
	v.RecordStubChannelCall("tenant-1", "instagram", "publish")
	v.RecordStubChannelCall("tenant-1", "pinterest", "create_listing")
	rec := metricsText(t, registry)
	if !strings.Contains(rec, `channel="instagram"`) {
		t.Fatalf("instagram label missing: %s", rec)
	}
	if !strings.Contains(rec, `op="create_listing"`) {
		t.Fatalf("create_listing op missing: %s", rec)
	}
}

func TestV391Metrics_ObserveDurations(t *testing.T) {
	t.Parallel()
	registry := metrics.NewRegistry("test")
	v := NewV391Metrics(registry)
	v.ObserveWizardCompletionDuration(1.23)
	v.ObserveChannelContentQueryDuration(0.05)
	v.ObserveOperatorAlertResolutionDuration(45.0)
	rec := metricsText(t, registry)
	for _, want := range []string{
		"ec_onboarding_wizard_completion_duration_seconds",
		"ec_channel_content_query_duration_seconds",
		"ec_operator_alerts_resolution_duration_seconds",
	} {
		if !strings.Contains(rec, want) {
			t.Fatalf("histogram %q missing: %s", want, rec)
		}
	}
}

func TestV391Metrics_NilSafety(t *testing.T) {
	t.Parallel()
	var v *V391Metrics
	v.RecordWizardStepCompleted("tenant", 1)
	v.ObserveWizardCompletionDuration(1)
	v.RecordOperatorAlert("tenant", alert.TypeLargeRefund, alert.StatusPending)
	v.ObserveChannelContentQueryDuration(1)
	v.ObserveOperatorAlertResolutionDuration(1)
	v.RecordStubChannelCall("t", "instagram", "publish")
	// Pass: no panic.
}

// metricsText writes the registry through its Prometheus handler
// and returns the body as a string for substring assertions.
func metricsText(t *testing.T, r *metrics.Registry) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}
