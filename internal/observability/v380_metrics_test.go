// File scope: v3.8.0/v3.8.1 EC-7 + EC-9-3 V380Metrics adapter
// tests. Asserts every typed facade method writes to the right
// underlying registry surface so a future refactor cannot silently
// stop emitting a metric.
package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/metrics"
	"github.com/stretchr/testify/require"
)

func newV380Harness(t *testing.T) (*metrics.Registry, *V380Metrics) {
	t.Helper()
	r := metrics.NewRegistry("v380-test")
	m := NewV380Metrics(r)
	require.NotNil(t, m)
	require.Same(t, r, m.Registry())
	return r, m
}

// scrape returns the /metrics body so tests can assert exposition
// shape without coupling to internal counter representation.
func scrape(t *testing.T, r *metrics.Registry) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func TestV380Metrics_RecordShippingLabelEmitsCounter(t *testing.T) {
	t.Parallel()
	r, m := newV380Harness(t)
	m.RecordShippingLabel("tenant-A", "auspost", "generated")
	body := scrape(t, r)
	require.Contains(t, body, "ec_shipping_labels_generated_total")
	require.Contains(t, body, `tenant_id="tenant-A"`)
	require.Contains(t, body, `carrier="auspost"`)
	require.Contains(t, body, `status="generated"`)
}

func TestV380Metrics_ObserveShippingLabelCostEmitsHistogram(t *testing.T) {
	t.Parallel()
	r, m := newV380Harness(t)
	m.ObserveShippingLabelCost("tenant-A", "dhl", 4599)
	body := scrape(t, r)
	require.Contains(t, body, "ec_shipping_label_cost_aud_cents")
	require.Contains(t, body, "ec_shipping_label_cost_aud_cents_sum")
	require.Contains(t, body, `carrier="dhl"`)
}

func TestV380Metrics_ObserveStatusPropagationDuration(t *testing.T) {
	t.Parallel()
	r, m := newV380Harness(t)
	m.ObserveStatusPropagationDuration("tiktok", 0.123)
	body := scrape(t, r)
	require.Contains(t, body, "ec_status_propagation_duration_seconds")
	require.Contains(t, body, `channel="tiktok"`)
}

func TestV380Metrics_RecordChannelUpdateNoOp(t *testing.T) {
	t.Parallel()
	r, m := newV380Harness(t)
	// RecordChannelUpdate is intentionally a no-op for v3.8.1
	// (cardinality dedup with the v3.4.1 channel health series).
	// The contract is that the call does not panic + does not emit
	// a duplicate counter.
	m.RecordChannelUpdate("tenant-A", "tiktok", "ok")
	body := scrape(t, r)
	require.NotContains(t, body, "ec_status_propagation_channel_updates_total", "RecordChannelUpdate must not emit a third counter")
}

func TestV380Metrics_ObserveROIQueryDurationSeconds(t *testing.T) {
	t.Parallel()
	r, m := newV380Harness(t)
	m.ObserveROIQueryDurationSeconds(0.073)
	m.RecordROIQuery("heatmap", 0.142)
	body := scrape(t, r)
	require.Contains(t, body, "ec_roi_query_duration_seconds")
	require.Contains(t, body, `route="all"`)
	require.Contains(t, body, `route="heatmap"`)
}

func TestV380Metrics_RecordReturnsSagaTransitionEmitsCounter(t *testing.T) {
	t.Parallel()
	r, m := newV380Harness(t)
	m.RecordReturnsSagaTransition("tenant-A", "completed", true)
	m.RecordReturnsSagaTransition("tenant-A", "rolled_back", false)
	body := scrape(t, r)
	require.Contains(t, body, "ec_returns_saga_state_transitions_total")
	require.Contains(t, body, `state="completed"`)
	require.Contains(t, body, `auto_approved="true"`)
	require.Contains(t, body, `state="rolled_back"`)
	require.Contains(t, body, `auto_approved="false"`)
}

func TestV380Metrics_ObserveReturnsRefundAmount(t *testing.T) {
	t.Parallel()
	r, m := newV380Harness(t)
	m.ObserveReturnsRefundAmount("tenant-A", 4500)
	body := scrape(t, r)
	require.Contains(t, body, "ec_returns_refund_amount_aud_cents")
}

func TestV380Metrics_RecordReplayAssertion(t *testing.T) {
	t.Parallel()
	r, m := newV380Harness(t)
	m.RecordReplayAssertion("returns_saga", "pass")
	m.RecordReplayAssertion("returns_saga", "non_determinism")
	body := scrape(t, r)
	require.Contains(t, body, "ec_workflow_replay_assertions_total")
	require.Contains(t, body, `outcome="pass"`)
	require.Contains(t, body, `outcome="non_determinism"`)
}

func TestV380Metrics_NilSafe(t *testing.T) {
	t.Parallel()
	// Nil receiver + nil registry must be tolerated so cmd/* binaries
	// can opt out of metrics emission without per-call guards. This
	// matches V340/V350/V360 and is part of the V*Metrics contract.
	var m *V380Metrics
	require.NotPanics(t, func() {
		m.RecordShippingLabel("t", "c", "s")
		m.ObserveShippingLabelCost("t", "c", 1)
		m.ObserveStatusPropagationDuration("c", 0.1)
		m.RecordChannelUpdate("t", "c", "s")
		m.ObserveROIQueryDurationSeconds(0.1)
		m.RecordROIQuery("r", 0.1)
		m.RecordReturnsSagaTransition("t", "s", true)
		m.ObserveReturnsRefundAmount("t", 1)
		m.RecordReplayAssertion("w", "o")
	})
	// A V380Metrics with a nil registry must also be tolerated.
	withNil := &V380Metrics{registry: nil}
	require.NotPanics(t, func() {
		withNil.RecordShippingLabel("t", "c", "s")
		withNil.ObserveShippingLabelCost("t", "c", 1)
		withNil.ObserveStatusPropagationDuration("c", 0.1)
		withNil.RecordChannelUpdate("t", "c", "s")
		withNil.ObserveROIQueryDurationSeconds(0.1)
		withNil.RecordROIQuery("r", 0.1)
		withNil.RecordReturnsSagaTransition("t", "s", false)
		withNil.ObserveReturnsRefundAmount("t", 1)
		withNil.RecordReplayAssertion("w", "o")
	})
}

func TestV380Metrics_HandlerEmitsAll7Series(t *testing.T) {
	t.Parallel()
	r, m := newV380Harness(t)
	m.RecordShippingLabel("t", "auspost", "generated")
	m.ObserveShippingLabelCost("t", "auspost", 1099)
	m.ObserveStatusPropagationDuration("tiktok", 0.05)
	m.RecordReturnsSagaTransition("t", "completed", true)
	m.ObserveReturnsRefundAmount("t", 4500)
	m.RecordROIQuery("heatmap", 0.05)
	m.RecordReplayAssertion("returns_saga", "pass")
	body := scrape(t, r)
	wantSeries := []string{
		"ec_shipping_labels_generated_total",
		"ec_shipping_label_cost_aud_cents",
		"ec_status_propagation_duration_seconds",
		"ec_returns_saga_state_transitions_total",
		"ec_returns_refund_amount_aud_cents",
		"ec_roi_query_duration_seconds",
		"ec_workflow_replay_assertions_total",
	}
	for _, name := range wantSeries {
		require.True(t, strings.Contains(body, name), "metric %s missing from /metrics", name)
	}
}
