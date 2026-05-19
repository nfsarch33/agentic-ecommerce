// File scope: v3.9.0 EC-6-4 + EC-6-5 + EC-5-2 + EC-5-4 + EC-5-5 +
// carry-forward observability spine.
//
// V390Metrics is the typed facade around the existing
// internal/metrics.Registry that the seven v3.9.0 surfaces emit
// counters / gauges / histograms through:
//
//   - EC-6-4 pricing.CompetitorScraperMetrics
//     (ec_competitor_prices_observed_total).
//   - EC-6-5 handler.MarginHandlerMetrics
//     (ec_margin_dashboard_request_duration_seconds).
//   - EC-5-2 content.CalendarMetrics
//     (ec_content_calendar_entries_total +
//     ec_content_calendar_publishing_duration_seconds).
//   - EC-5-4 content.HashtagAgentMetrics
//     (ec_hashtag_caption_generations_total).
//   - EC-5-5 content.PerformanceLoopMetrics
//     (ec_content_ema_score + ec_content_ema_updates_total).
//   - Carry-forward closure: ChannelStatusUpdater outcome counter
//     (ec_channel_status_updates_total).
//
// Single struct satisfies all seven ports so cmd/* binaries wire
// one instance and pass it everywhere. Mirrors the V340 / V350 /
// V360 / V380 facades exactly.
package observability

import (
	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

// V390Metrics is the v3.9.0 typed facade.
type V390Metrics struct {
	registry *metrics.Registry
}

// NewV390Metrics binds to the supplied registry.
func NewV390Metrics(registry *metrics.Registry) *V390Metrics {
	return &V390Metrics{registry: registry}
}

// RecordCompetitorObservation implements
// pricing.CompetitorScraperMetrics. undercut is "true" / "false".
func (m *V390Metrics) RecordCompetitorObservation(tenantID, channel, undercut string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.CompetitorPricesObservedTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"channel":   channel,
		"undercut":  undercut,
	})
}

// ObserveMarginDashboardDuration implements handler.MarginHandlerMetrics.
func (m *V390Metrics) ObserveMarginDashboardDuration(durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.MarginDashboardRequestDuration.Observe(durationSec, nil)
}

// RecordCalendarEntry implements content.CalendarMetrics.
func (m *V390Metrics) RecordCalendarEntry(tenantID, channel, status string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ContentCalendarEntriesTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"channel":   channel,
		"status":    status,
	})
}

// ObserveCalendarPublishingDuration implements content.CalendarMetrics.
func (m *V390Metrics) ObserveCalendarPublishingDuration(tenantID, channel string, durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ContentCalendarPublishingDuration.Observe(durationSec, metrics.Labels{
		"tenant_id": tenantID,
		"channel":   channel,
	})
}

// RecordHashtagGeneration implements content.HashtagAgentMetrics.
func (m *V390Metrics) RecordHashtagGeneration(tenantID, channel, outcome string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.HashtagCaptionGenerationsTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"channel":   channel,
		"outcome":   outcome,
	})
}

// SetContentEMAScore implements content.PerformanceLoopMetrics.
func (m *V390Metrics) SetContentEMAScore(tenantID, channel, contentType string, score float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ContentEMAScore.Set(score, metrics.Labels{
		"tenant_id":    tenantID,
		"channel":      channel,
		"content_type": contentType,
	})
}

// RecordContentEMAUpdate implements content.PerformanceLoopMetrics.
func (m *V390Metrics) RecordContentEMAUpdate(tenantID, channel string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ContentEMAUpdatesTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"channel":   channel,
	})
}

// ObserveContentEMAUpdateDuration implements content.PerformanceLoopMetrics.
// Pivots through ec_content_calendar_publishing_duration_seconds with a
// dedicated channel="ema" label so the dashboard can spot the EMA-write
// hot path next to the publishing histogram. Cardinality stays bounded.
func (m *V390Metrics) ObserveContentEMAUpdateDuration(durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ContentCalendarPublishingDuration.Observe(durationSec, metrics.Labels{
		"tenant_id": "_ema",
		"channel":   "ema",
	})
}

// RecordChannelStatusUpdate is the carry-forward closure counter
// for ChannelStatusUpdater outcomes. cmd/* wires this through
// fulfilment.StatusPropagatorMetrics in v3.9.0.
//
// outcome is ok|failed.
func (m *V390Metrics) RecordChannelStatusUpdate(tenantID, channel, outcome string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.ChannelStatusUpdatesTotal.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"channel":   channel,
		"outcome":   outcome,
	})
}

// Registry returns the underlying registry.
func (m *V390Metrics) Registry() *metrics.Registry { return m.registry }
