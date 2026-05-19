// File scope: v3.9.0 carry-forward closure -- wires the v3.8.0
// EC-7-4 fulfilment.ChannelStatusUpdater port for the existing
// WooCommerce REST client.
//
// WC accepts a PUT /wp-json/wc/v3/orders/{id} with status="completed"
// + tracking metadata; this updater exposes that capability behind
// the EC-7-4 contract.
package woocommerce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/nfsarch33/helixon-ec/internal/agent/fulfilment"
)

// WooCommerceChannelName is the canonical channel label.
const WooCommerceChannelName = "woocommerce"

// ErrWooCommerceStatusUpdateFailed is the typed sentinel.
var ErrWooCommerceStatusUpdateFailed = errors.New("woocommerce: status update failed")

// StatusUpdater implements fulfilment.ChannelStatusUpdater for
// the existing WooCommerce Client.
type StatusUpdater struct {
	client   Client
	tenantID string
	logger   *slog.Logger
}

// NewStatusUpdater wraps an existing WooCommerce Client.
func NewStatusUpdater(logger *slog.Logger, client Client, tenantID string) (*StatusUpdater, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrWooCommerceStatusUpdateFailed)
	}
	if client.baseURL == "" {
		return nil, fmt.Errorf("%w: WooCommerce client requires base URL", ErrWooCommerceStatusUpdateFailed)
	}
	return &StatusUpdater{client: client, tenantID: tenantID, logger: logger}, nil
}

// ChannelName implements fulfilment.ChannelStatusUpdater.
func (u *StatusUpdater) ChannelName() string { return WooCommerceChannelName }

// UpdateOrderStatus implements fulfilment.ChannelStatusUpdater.
// Posts a PUT to /orders/{external_order_id} with the WooCommerce-
// canonical status mapping (delivered -> completed; in_transit ->
// processing; exception -> on-hold). Cyclomatic 5.
func (u *StatusUpdater) UpdateOrderStatus(ctx context.Context, in fulfilment.ChannelStatusUpdate) error {
	if strings.TrimSpace(in.ExternalOrderID) == "" {
		return fmt.Errorf("%w: ExternalOrderID required", ErrWooCommerceStatusUpdateFailed)
	}
	body, err := json.Marshal(map[string]any{
		"status": mapShipmentStatusToWoo(in.Status),
		"meta_data": []map[string]any{
			{"key": "_tracking_number", "value": in.TrackingNumber},
			{"key": "_delivery_date", "value": in.DeliveryDate.Format("2006-01-02")},
			{"key": "_tenant_id", "value": stringOrFallback(in.TenantID, u.tenantID)},
		},
	})
	if err != nil {
		return fmt.Errorf("%w: marshal: %v", ErrWooCommerceStatusUpdateFailed, err)
	}
	endpoint, err := u.client.endpoint("/orders/"+url.PathEscape(in.ExternalOrderID), nil)
	if err != nil {
		return fmt.Errorf("%w: endpoint: %v", ErrWooCommerceStatusUpdateFailed, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrWooCommerceStatusUpdateFailed, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := u.client.httpClient.Do(req)
	if err != nil {
		u.logger.Warn("woocommerce.status_updater.transport", "tenant_id", in.TenantID, "external_order_id", in.ExternalOrderID, "error", err)
		return fmt.Errorf("%w: transport: %v", ErrWooCommerceStatusUpdateFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%w: status %d body=%s", ErrWooCommerceStatusUpdateFailed, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// Close implements lifecycle.Closer.
func (u *StatusUpdater) Close(_ context.Context) error { return nil }

// mapShipmentStatusToWoo maps the v3.8.0 ShipmentStatusUpdatedPayload
// status taxonomy to the WooCommerce status enum.
func mapShipmentStatusToWoo(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "delivered":
		return "completed"
	case "shipped", "in_transit":
		return "processing"
	case "exception":
		return "on-hold"
	default:
		return status
	}
}

func stringOrFallback(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

// Compile-time guard.
var _ fulfilment.ChannelStatusUpdater = (*StatusUpdater)(nil)
