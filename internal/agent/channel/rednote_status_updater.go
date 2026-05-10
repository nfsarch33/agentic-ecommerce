// File scope: v3.9.0 carry-forward closure -- wires the v3.8.0
// EC-7-4 fulfilment.ChannelStatusUpdater port for the existing
// RedNote uiauto client.
//
// RedNote (Xiaohongshu) does NOT expose a programmatic API for
// post-purchase order status updates -- the platform's organic
// posting flow ends at the create-note step. The EC-4-1 client
// already documents this constraint. The status updater therefore
// fans the call to the omniparser-bridge as a "status_update" op
// so the bridge can either no-op (current behaviour) or post a
// structured status comment in the future. Failure paths preserve
// the typed sentinels so the propagator's retry-with-backoff helper
// can classify rate limits + auth failures correctly.
package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/social"
	"github.com/nfsarch33/agentic-ecommerce/internal/agent/fulfilment"
)

// RedNoteChannelName is the canonical channel label.
const RedNoteChannelName = "rednote"

// RedNoteStatusBridgePath is the canonical path the omniparser-
// bridge exposes for status updates. Distinct from the post path
// so a single bridge instance can route both.
const RedNoteStatusBridgePath = "/uiauto/rednote/status"

// RedNoteStatusUpdater implements fulfilment.ChannelStatusUpdater
// for the existing RedNote uiauto client. The path is the
// /uiauto/rednote/status alias on the omniparser-bridge.
type RedNoteStatusUpdater struct {
	client   *RedNoteUIAutoClient
	tenantID string
	logger   *slog.Logger
}

// NewRedNoteStatusUpdater wraps an existing RedNoteUIAutoClient.
func NewRedNoteStatusUpdater(logger *slog.Logger, client *RedNoteUIAutoClient, tenantID string) (*RedNoteStatusUpdater, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		return nil, fmt.Errorf("%w: RedNoteUIAutoClient required", ErrRedNoteUnconfigured)
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrRedNoteUnconfigured)
	}
	return &RedNoteStatusUpdater{client: client, tenantID: tenantID, logger: logger}, nil
}

// ChannelName implements fulfilment.ChannelStatusUpdater.
func (u *RedNoteStatusUpdater) ChannelName() string { return RedNoteChannelName }

// UpdateOrderStatus implements fulfilment.ChannelStatusUpdater.
// Posts a status_update op to the omniparser-bridge. Because RedNote
// has no public API, the bridge acks the call as a no-op today;
// future bridge versions can comment on the original organic note.
// Cyclomatic 4.
func (u *RedNoteStatusUpdater) UpdateOrderStatus(ctx context.Context, in fulfilment.ChannelStatusUpdate) error {
	if err := u.client.guard(); err != nil {
		return err
	}
	tenant := requireString(in.TenantID, u.tenantID)
	body, err := json.Marshal(map[string]any{
		"tenant_id":         tenant,
		"external_order_id": in.ExternalOrderID,
		"status":            in.Status,
		"tracking_number":   in.TrackingNumber,
		"delivery_date":     in.DeliveryDate.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return fmt.Errorf("rednote: encode status body: %w", err)
	}
	httpReq, err := u.buildSignedStatusRequest(ctx, body)
	if err != nil {
		return err
	}
	resp, err := u.client.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		u.client.recordOutcome("status_transport_error")
		return fmt.Errorf("%w: %v", ErrRedNoteBridgeUnreachable, err)
	}
	defer resp.Body.Close()
	return u.parseStatusResponse(resp)
}

// buildSignedStatusRequest stamps the X-Bridge-* headers with an
// HMAC over the canonical form, mirroring buildSignedRequest in
// rednote_uiauto_client.go. Cyclomatic 3.
func (u *RedNoteStatusUpdater) buildSignedStatusRequest(ctx context.Context, body []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.client.cfg.BridgeURL+RedNoteStatusBridgePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rednote: build status request: %w", err)
	}
	timestamp := u.client.cfg.Now().Unix()
	signature, err := social.ComputeTikTokSignature(social.TikTokSignRequest{
		Secret:    u.client.cfg.BridgeSecret,
		Timestamp: timestamp,
		Path:      RedNoteStatusBridgePath,
		Body:      body,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRedNoteSignatureBuild, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", u.client.cfg.UserAgent)
	httpReq.Header.Set("X-Bridge-Timestamp", fmt.Sprintf("%d", timestamp))
	httpReq.Header.Set("X-Bridge-Sign", signature)
	httpReq.Header.Set("X-Bridge-Tenant", u.tenantID)
	httpReq.Header.Set("X-Bridge-Platform", RedNoteBridgePlatform)
	return httpReq, nil
}

func (u *RedNoteStatusUpdater) parseStatusResponse(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxRedNoteBridgeBodyBytes))
	if err != nil {
		u.client.recordOutcome("status_read_failed")
		return fmt.Errorf("rednote: read bridge status body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		u.client.recordOutcome("status_bridge_rejected")
		return fmt.Errorf("%w: status=%d body=%s", ErrRedNoteBridgeRejected, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	u.client.recordOutcome("status_ok")
	return nil
}

// Close implements lifecycle.Closer.
func (u *RedNoteStatusUpdater) Close(ctx context.Context) error { return u.client.Close(ctx) }

// Compile-time guard.
var _ fulfilment.ChannelStatusUpdater = (*RedNoteStatusUpdater)(nil)

// errRedNoteStatusBridge is the package-private sentinel the unit
// test fires when the underlying bridge surfaced a transport error.
var errRedNoteStatusBridge = errors.New("rednote: status bridge transport error")
