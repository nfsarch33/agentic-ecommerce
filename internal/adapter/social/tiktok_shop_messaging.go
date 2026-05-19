// File scope: v3.6.0 EC-8-3 TikTok Shop messaging extension.
//
// Adds the SendMessage method to TikTokShopClient so it satisfies
// port.OutboundMessageSender for the v3.6.0 customer service
// pipeline. The implementation reuses the existing roundTrip
// infrastructure (signing, auth, metrics) -- no new HTTP plumbing.
package social

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// SendMessage delivers a customer service reply to a TikTok Shop
// message thread. Wires through roundTrip so signing + auth +
// metrics behave the same as every other API call.
func (c *TikTokShopClient) SendMessage(ctx context.Context, req port.OutboundMessageRequest) (port.OutboundMessageResponse, error) {
	if err := c.guard(); err != nil {
		return port.OutboundMessageResponse{}, err
	}
	if req.ThreadID == "" {
		return port.OutboundMessageResponse{}, fmt.Errorf("%w: ThreadID required", ErrTikTokUnconfigured)
	}
	if req.Text == "" {
		return port.OutboundMessageResponse{}, fmt.Errorf("%w: Text required", ErrTikTokUnconfigured)
	}
	body, err := json.Marshal(map[string]any{
		"thread_id": req.ThreadID,
		"text":      req.Text,
	})
	if err != nil {
		return port.OutboundMessageResponse{}, fmt.Errorf("%w: encode message payload: %v", ErrTikTokInvalidResponse, err)
	}
	resp, err := c.roundTrip(ctx, requestEnvelope{
		Method:   http.MethodPost,
		Path:     "/api/messages/send",
		Body:     body,
		TenantID: requireTenantID(req.TenantID, c.cfg.TenantID),
		Endpoint: "messages.send",
	})
	if err != nil {
		return port.OutboundMessageResponse{}, err
	}
	var sent tiktokMessageSentResponse
	if len(resp) > 0 {
		if err := decodeJSON(resp, &sent); err != nil {
			return port.OutboundMessageResponse{}, err
		}
	}
	return port.OutboundMessageResponse{ProviderMessageID: sent.MessageID}, nil
}

// tiktokMessageSentResponse is the trimmed wire shape of the
// TikTok messaging API response.
type tiktokMessageSentResponse struct {
	MessageID string `json:"message_id"`
}
