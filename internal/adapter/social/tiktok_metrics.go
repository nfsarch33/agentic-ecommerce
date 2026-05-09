package social

// TikTokMetricsHook is the small port the EC-3-1 client uses to
// emit Prometheus counters / histograms without coupling to the
// internal/metrics.Registry. The observability spine in
// internal/observability/tiktok_metrics.go implements this
// interface.
//
// Every method is nil-safe via the *TikTokShopClient.recordMetric
// guard so cmd/* binaries that disable metrics in dev / unit tests
// can pass nil without extra branching.
type TikTokMetricsHook interface {
	RecordAPICall(tenantID, endpoint, status string, durationSeconds float64)
	RecordListing(tenantID, outcome string)
	RecordWebhook(tenantID, eventType, status string)
	RecordInventorySync(tenantID, direction, status string)
	RecordSignatureFailure(tenantID, reason string)
}
