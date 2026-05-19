// File scope: v3.9.0 carry-forward closure -- wires the v3.8.0
// EC-7-4 fulfilment.ChannelStatusUpdater port for the existing
// TikTok Shop adapter so cmd/* binaries can plug it directly into
// fulfilment.StatusPropagator.
//
// The wrapper holds the HTTP client and fans every UpdateOrderStatus
// call out to /api/orders/{external_order_id}/shipments using the
// existing signed roundTrip. Failure paths preserve the typed
// sentinels so the propagator's retry-with-backoff helper can
// classify rate limits + auth failures correctly.
package social

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/agent/fulfilment"
)

// TikTokChannelName is the canonical channel label.
const TikTokChannelName = "tiktok"

// TikTokStatusUpdater implements fulfilment.ChannelStatusUpdater
// for the existing TikTok Shop client.
type TikTokStatusUpdater struct {
	client   *TikTokShopClient
	tenantID string
	logger   *slog.Logger
	now      func() time.Time
}

// NewTikTokStatusUpdater wraps an existing TikTokShopClient.
func NewTikTokStatusUpdater(logger *slog.Logger, client *TikTokShopClient, tenantID string) (*TikTokStatusUpdater, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		return nil, fmt.Errorf("%w: TikTokShopClient required", ErrTikTokUnconfigured)
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrTikTokUnconfigured)
	}
	return &TikTokStatusUpdater{
		client:   client,
		tenantID: tenantID,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
	}, nil
}

// ChannelName implements fulfilment.ChannelStatusUpdater.
func (u *TikTokStatusUpdater) ChannelName() string { return TikTokChannelName }

// UpdateOrderStatus implements fulfilment.ChannelStatusUpdater. The
// underlying call signs + posts to TikTok's order shipment endpoint
// using the canonical roundTrip path (HMAC + token).
func (u *TikTokStatusUpdater) UpdateOrderStatus(ctx context.Context, in fulfilment.ChannelStatusUpdate) error {
	if err := u.client.guard(); err != nil {
		return err
	}
	if strings.TrimSpace(in.ExternalOrderID) == "" {
		return fmt.Errorf("%w: ExternalOrderID required", ErrTikTokUnconfigured)
	}
	body, err := json.Marshal(map[string]any{
		"external_order_id": in.ExternalOrderID,
		"status":            in.Status,
		"tracking_number":   in.TrackingNumber,
		"delivery_date":     in.DeliveryDate.UTC().Format(time.RFC3339Nano),
		"tenant_id":         requireTenantID(in.TenantID, u.tenantID),
	})
	if err != nil {
		return fmt.Errorf("%w: encode status payload: %v", ErrTikTokInvalidResponse, err)
	}
	_, err = u.client.roundTrip(ctx, requestEnvelope{
		Method:   http.MethodPost,
		Path:     "/api/orders/" + url.PathEscape(in.ExternalOrderID) + "/shipments",
		Body:     body,
		TenantID: requireTenantID(in.TenantID, u.tenantID),
		Endpoint: "orders.shipments",
	})
	if err != nil {
		u.logger.Warn("tiktok.status_updater.update_failed", "tenant_id", in.TenantID, "external_order_id", in.ExternalOrderID, "error", err)
		return err
	}
	return nil
}

// Close implements lifecycle.Closer.
func (u *TikTokStatusUpdater) Close(ctx context.Context) error { return u.client.Close(ctx) }

// Compile-time guard: ensure TikTokStatusUpdater satisfies the
// fulfilment.ChannelStatusUpdater contract. The unused variable
// pattern is the canonical Go check (cite: go-clean-architecture
// "verify interfaces with type assertions").
var _ fulfilment.ChannelStatusUpdater = (*TikTokStatusUpdater)(nil)

// errTikTokStatusUnreachable is the package-private sentinel the
// unit test fires when the underlying client surfaced a transport
// error after exhausting retries.
var errTikTokStatusUnreachable = errors.New("tiktok: status updater unreachable")
