// File scope: v3.9.0 carry-forward closure -- wires the v3.8.0
// EC-7-4 fulfilment.ChannelStatusUpdater port for the existing
// Facebook Shop adapter.
//
// Wraps PushOrderStatus from the existing FacebookShopClient. The
// Facebook Graph API exposes /<order_id>/shipments which already
// matches the EC-7-4 contract.
package social

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nfsarch33/helixon-ec/internal/agent/fulfilment"
)

// FacebookChannelName is the canonical channel label.
const FacebookChannelName = "facebook"

// FacebookStatusUpdater implements fulfilment.ChannelStatusUpdater
// for the existing Facebook Shop client.
type FacebookStatusUpdater struct {
	client   *FacebookShopClient
	tenantID string
	logger   *slog.Logger
}

// NewFacebookStatusUpdater wraps an existing FacebookShopClient.
func NewFacebookStatusUpdater(logger *slog.Logger, client *FacebookShopClient, tenantID string) (*FacebookStatusUpdater, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		return nil, fmt.Errorf("%w: FacebookShopClient required", ErrFacebookUnconfigured)
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrFacebookUnconfigured)
	}
	return &FacebookStatusUpdater{client: client, tenantID: tenantID, logger: logger}, nil
}

// ChannelName implements fulfilment.ChannelStatusUpdater.
func (u *FacebookStatusUpdater) ChannelName() string { return FacebookChannelName }

// UpdateOrderStatus implements fulfilment.ChannelStatusUpdater.
// Delegates to PushOrderStatus on the underlying Graph API client.
func (u *FacebookStatusUpdater) UpdateOrderStatus(ctx context.Context, in fulfilment.ChannelStatusUpdate) error {
	if strings.TrimSpace(in.ExternalOrderID) == "" {
		return fmt.Errorf("%w: ExternalOrderID required", ErrFacebookUnconfigured)
	}
	push := FacebookOrderStatusPush{
		TenantID:       requireTenantID(in.TenantID, u.tenantID),
		OrderID:        in.ExternalOrderID,
		Status:         in.Status,
		TrackingNumber: in.TrackingNumber,
	}
	if err := u.client.PushOrderStatus(ctx, push); err != nil {
		u.logger.Warn("facebook.status_updater.push_failed", "tenant_id", in.TenantID, "external_order_id", in.ExternalOrderID, "error", err)
		return err
	}
	return nil
}

// Close implements lifecycle.Closer.
func (u *FacebookStatusUpdater) Close(ctx context.Context) error { return u.client.Close(ctx) }

// Compile-time guard.
var _ fulfilment.ChannelStatusUpdater = (*FacebookStatusUpdater)(nil)
