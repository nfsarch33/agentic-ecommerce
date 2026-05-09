package observability

import (
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

func TestV360Metrics_RecordsAllSurfaces(t *testing.T) {
	t.Parallel()
	registry := metrics.NewRegistry("v360-test")
	v := NewV360Metrics(registry)
	v.RecordClassification("tenant-A", "refund_request", "negative", "en")
	v.RecordFAQResponse("tenant-A", "auto_replied")
	v.RecordMessageWebhook("tenant-A", "tiktok", "replied")
	v.ObserveGMVRequestDurationSeconds(0.025)
	v.IncActiveConnections("tenant-A")
	v.IncActiveConnections("tenant-A")
	v.IncDispatchedEvents("tenant-A", "price.change.applied")
	v.DecActiveConnections("tenant-A")

	// Render the /metrics output and assert every metric appears.
	rec := newRecordingResponseWriter()
	registry.Handler().ServeHTTP(rec, mustGetReq(t))
	body := rec.body.String()
	for _, fragment := range []string{
		"ec_enquiry_classifications_total",
		"ec_faq_responses_total",
		"ec_message_webhook_received_total",
		"ec_gmv_rollup_request_duration_seconds",
		"ec_sse_active_connections",
		"ec_sse_events_dispatched_total",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("missing %s in /metrics output", fragment)
		}
	}
	// Exercise the no-op guards.
	var nilFacade *V360Metrics
	nilFacade.RecordClassification("t", "i", "s", "l")
	nilFacade.RecordFAQResponse("t", "o")
	nilFacade.RecordMessageWebhook("t", "c", "s")
	nilFacade.ObserveGMVRequestDurationSeconds(0.1)
	nilFacade.IncActiveConnections("t")
	nilFacade.DecActiveConnections("t")
	nilFacade.IncDispatchedEvents("t", "x")
	if v.Registry() != registry {
		t.Fatalf("Registry() returned a different instance")
	}
}

func TestSSECounter_NeverGoesNegative(t *testing.T) {
	t.Parallel()
	c := newSSECounter()
	c.adjust("t", -1)
	if got := c.get("t"); got != 0 {
		t.Fatalf("got = %v, want 0", got)
	}
	c.adjust("t", 3)
	c.adjust("t", -2)
	if got := c.get("t"); got != 1 {
		t.Fatalf("got = %v, want 1", got)
	}
}
