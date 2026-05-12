package marketplacesync

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

func TestRegistryMetricsEmitsBoundedLabels(t *testing.T) {
	t.Parallel()

	registry := metrics.NewRegistry("mc-api")
	adapter := NewRegistryMetrics(registry)
	event := ProductEvent{
		TenantID:   "tenant-a",
		Provider:   "shopify",
		EntityType: EntityProduct,
		EntityID:   "sku-1",
		ExternalID: "remote-1",
		Operation:  OperationUpsert,
		Version:    "v1",
	}

	adapter.RecordSyncEvent(event, StatusApplied)
	adapter.RecordDLQ(DLQRecord{Event: event, Attempts: 2, Reason: "transient tcp reset for sku-1"})
	adapter.RecordReplay(DLQRecord{Event: event, Attempts: 2, Reason: "transient tcp reset for sku-1"}, StatusDuplicate)

	rec := httptest.NewRecorder()
	registry.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`ec_marketplace_sync_events_total{binary="mc-api",entity_type="product",provider="shopify",status="applied"} 1`,
		`ec_marketplace_sync_dlq_total{binary="mc-api",entity_type="product",provider="shopify",reason="transient"} 1`,
		`ec_marketplace_replay_total{binary="mc-api",entity_type="product",provider="shopify",status="duplicate"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "sku-1") || strings.Contains(body, "tcp reset") {
		t.Fatalf("metrics body contains unbounded error content\n%s", body)
	}
}
