// File scope: v3.3.0 EC-3 TikTok Shop observability spine.
//
// TikTokMetrics is the typed facade around the existing
// internal/metrics.Registry that the four EC-3 surfaces emit
// counters / histograms through:
//
//   - EC-3-1 social.TikTokShopClient signs every request and emits
//     ec_tiktok_api_calls_total + ec_tiktok_api_duration_seconds.
//   - EC-3-2 channel.TikTokListingAgent emits
//     ec_tiktok_listing_attempts_total per outcome.
//   - EC-3-3 webhook.TikTokOrderHandler emits
//     ec_tiktok_webhook_received_total + ec_tiktok_signature_failures_total.
//   - EC-3-4 sync.TikTokInventorySync emits
//     ec_tiktok_inventory_sync_total per direction + status.
//
// Design notes:
//
//   - Single struct satisfies social.TikTokMetricsHook,
//     channel.TikTokListingMetrics, webhook.TikTokWebhookMetrics,
//     and sync.InventorySyncMetrics so cmd/* binaries wire one
//     instance and pass it everywhere.
//   - Nil-safe receivers: every Record* method on *TikTokMetrics
//     is a no-op when the receiver is nil. Mirrors the EC-2-5
//     EnrichmentMetrics pattern.
//   - Cardinality budget annotated in metrics.Registry per series.
package observability

import (
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

// TikTokMetrics is the EC-3 typed facade.
type TikTokMetrics struct {
	registry *metrics.Registry
}

// NewTikTokMetrics binds to the supplied registry.
func NewTikTokMetrics(registry *metrics.Registry) *TikTokMetrics {
	return &TikTokMetrics{registry: registry}
}

// RecordAPICall implements social.TikTokMetricsHook.RecordAPICall.
// Emits ec_tiktok_api_calls_total + ec_tiktok_api_duration_seconds.
func (m *TikTokMetrics) RecordAPICall(tenantID, endpoint, status string, durationSeconds float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.TikTokAPICallsTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"endpoint":  endpoint,
		"status":    status,
	})
	if durationSeconds > 0 {
		m.registry.TikTokAPIDurationSeconds.Observe(durationSeconds, metrics.Labels{"endpoint": endpoint})
	}
}

// RecordListing implements channel.TikTokListingMetrics.RecordListing
// AND social.TikTokMetricsHook.RecordListing.
func (m *TikTokMetrics) RecordListing(tenantID, outcome string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.TikTokListingAttemptsTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"outcome":   outcome,
	})
}

// RecordWebhook implements webhook.TikTokWebhookMetrics.RecordWebhook.
func (m *TikTokMetrics) RecordWebhook(tenantID, eventType, status string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.TikTokWebhookReceivedTotal.Inc(metrics.Labels{
		"tenant_id":  tenantID,
		"event_type": eventType,
		"status":     status,
	})
}

// RecordSignatureFailure implements webhook.TikTokWebhookMetrics +
// social.TikTokMetricsHook signature failure recording.
func (m *TikTokMetrics) RecordSignatureFailure(tenantID, reason string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.TikTokSignatureFailuresTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"reason":    reason,
	})
}

// RecordInventorySync implements sync.InventorySyncMetrics.RecordInventorySync.
func (m *TikTokMetrics) RecordInventorySync(tenantID, direction, status string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.TikTokInventorySyncTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"direction": direction,
		"status":    status,
	})
}

// Registry returns the underlying registry.
func (m *TikTokMetrics) Registry() *metrics.Registry { return m.registry }
