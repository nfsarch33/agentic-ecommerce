package webhook

import "errors"

// v3.3.0 EC-3-3 webhook package sentinels.
var (
	// ErrWebhookUnconfigured is returned by the constructor when a
	// required dependency is missing.
	ErrWebhookUnconfigured = errors.New("webhook: handler unconfigured")

	// ErrWebhookClosed is returned after Close.
	ErrWebhookClosed = errors.New("webhook: handler closed")

	// ErrWebhookPayloadInvalid is returned when the body cannot be
	// decoded into the expected wire shape.
	ErrWebhookPayloadInvalid = errors.New("webhook: payload invalid")
)
