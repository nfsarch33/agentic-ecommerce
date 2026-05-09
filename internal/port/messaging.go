// Package port: messaging adapter contract for v3.6.0 EC-8-3.
//
// Adapters in internal/adapter/social (TikTokShopClient,
// FacebookShopClient) implement OutboundMessageSender so the
// v3.6.0 messaging pipeline can stay channel-agnostic. The
// pipeline itself lives in internal/webhook/messaging.go and
// dispatches by channel name to a registered sender.
package port

import "context"

// OutboundMessageRequest is the unit of work submitted to
// SendMessage. ThreadID identifies the conversation on the
// platform side (TikTok message thread ID; Facebook PSID).
type OutboundMessageRequest struct {
	TenantID string
	ThreadID string
	Text     string
}

// OutboundMessageResponse captures the platform-side message ID so
// the audit log can correlate the reply with the inbound webhook.
type OutboundMessageResponse struct {
	ProviderMessageID string
}

// OutboundMessageSender is implemented by per-channel adapters
// (TikTok, Facebook Messenger) so the EC-8-3 pipeline can route
// outbound replies via a small stable port.
type OutboundMessageSender interface {
	SendMessage(ctx context.Context, req OutboundMessageRequest) (OutboundMessageResponse, error)
}
