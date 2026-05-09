// File scope: v3.6.0 EC-8-3 Facebook Messenger Send API extension.
//
// Adds the SendMessage method to FacebookShopClient so it satisfies
// port.OutboundMessageSender for the v3.6.0 customer service
// pipeline. The implementation reuses the existing roundTrip
// infrastructure -- signing (appsecret_proof), auth, metrics --
// so behaviour matches every other Graph API call.
package social

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// SendMessage delivers a customer service reply to a Facebook
// Messenger thread (the buyer's PSID). Wires through the existing
// Graph API roundTrip so signing + auth + metrics behave the same
// as every other Graph call.
func (c *FacebookShopClient) SendMessage(ctx context.Context, req port.OutboundMessageRequest) (port.OutboundMessageResponse, error) {
	if err := c.guard(); err != nil {
		return port.OutboundMessageResponse{}, err
	}
	if req.ThreadID == "" {
		return port.OutboundMessageResponse{}, fmt.Errorf("%w: ThreadID (PSID) required", ErrFacebookUnconfigured)
	}
	if req.Text == "" {
		return port.OutboundMessageResponse{}, fmt.Errorf("%w: Text required", ErrFacebookUnconfigured)
	}
	body, err := json.Marshal(map[string]any{
		"recipient": map[string]string{"id": req.ThreadID},
		"message":   map[string]string{"text": req.Text},
	})
	if err != nil {
		return port.OutboundMessageResponse{}, fmt.Errorf("%w: encode message payload: %v", ErrFacebookInvalidResponse, err)
	}
	resp, err := c.roundTrip(ctx, facebookEnvelope{
		Method:   http.MethodPost,
		Path:     "/me/messages",
		Body:     body,
		TenantID: requireTenantID(req.TenantID, c.cfg.TenantID),
		Endpoint: "messages.send",
	})
	if err != nil {
		return port.OutboundMessageResponse{}, err
	}
	var sent facebookMessageSentResponse
	if len(resp) > 0 {
		if err := json.Unmarshal(resp, &sent); err != nil {
			return port.OutboundMessageResponse{}, fmt.Errorf("%w: decode body: %v", ErrFacebookInvalidResponse, err)
		}
	}
	return port.OutboundMessageResponse{ProviderMessageID: sent.MessageID}, nil
}

// facebookMessageSentResponse is the trimmed wire shape of the
// Messenger Send API response.
type facebookMessageSentResponse struct {
	MessageID string `json:"message_id"`
}
