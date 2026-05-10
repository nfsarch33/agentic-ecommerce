// Package alert defines shared alert type enumerations used by
// both the API handler layer and the observability layer. Extracted
// from internal/api/handler to break the observability → handler
// import cycle detected by sentrux (v4.10.0).
package alert

// AlertType enumerates operator-actionable event sources.
type AlertType string

const (
	TypeLargeRefund       AlertType = "large_refund_pending_approval"
	TypeLargeDropship     AlertType = "large_dropship_pending_approval"
	TypePriceChange       AlertType = "price_change_pending_approval"
	TypeCAPTCHADetected   AlertType = "captcha_detected"
	TypeOmniUnavailable   AlertType = "omniparser_unavailable"
	TypeRateLimitDrain    AlertType = "rate_limit_drain"
	TypeChannelStatusFail AlertType = "channel_status_update_failed"
	TypeLargeMargin       AlertType = "large_margin_alert"
)

// AlertStatus enumerates the lifecycle states.
type AlertStatus string

const (
	StatusPending      AlertStatus = "pending"
	StatusAcknowledged AlertStatus = "acknowledged"
	StatusResolved     AlertStatus = "resolved"
	StatusExpired      AlertStatus = "expired"
)

// AlertSeverity enumerates severity levels.
type AlertSeverity string

const (
	SeverityInfo     AlertSeverity = "info"
	SeverityWarning  AlertSeverity = "warning"
	SeverityCritical AlertSeverity = "critical"
)
