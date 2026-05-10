// File scope: v3.9.1 EC-4-4 -- Pinterest Shop stub adapter.
//
// Sibling of instagram_shop.go; same behaviour contract today, same
// port set, same metric labels (the only thing that changes is the
// channel name). When the v4.1.x production adapters land, both
// stubs swap one-for-one with the real Pinterest Catalog API client.
package social

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/fulfilment"
	"github.com/nfsarch33/agentic-ecommerce/internal/channelport"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

// PinterestChannelName is the canonical channel label.
const PinterestChannelName = "pinterest"

// ErrPinterestStubUnconfigured is returned by the constructor when
// a required dependency is missing.
var ErrPinterestStubUnconfigured = errors.New("pinterest stub: unconfigured")

// PinterestStubAdapter is the v3.9.1 EC-4-4 Pinterest Shop stub.
type PinterestStubAdapter struct {
	tenantID string
	metrics  StubChannelMetrics
}

// NewPinterestStubAdapter constructs the stub adapter. metrics is
// optional (nil = no-op).
func NewPinterestStubAdapter(metrics StubChannelMetrics, tenantID string) (*PinterestStubAdapter, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrPinterestStubUnconfigured)
	}
	return &PinterestStubAdapter{tenantID: tenantID, metrics: metrics}, nil
}

// Name implements channel.ChannelAdapter.
func (a *PinterestStubAdapter) Name() string { return PinterestChannelName }

// ChannelName implements both channel.ChannelOrderListing AND
// fulfilment.ChannelStatusUpdater.
func (a *PinterestStubAdapter) ChannelName() string { return PinterestChannelName }

// Publish implements channel.ChannelAdapter. Stub: returns
// ErrChannelNotImplemented immediately.
func (a *PinterestStubAdapter) Publish(_ context.Context, payload eventbus.ProductEnrichedPayload) error {
	a.recordOp("publish")
	return wrapStubNotImplemented(PinterestChannelName, "publish", payload.ProductID)
}

// UpdateOrderStatus implements fulfilment.ChannelStatusUpdater.
// Stub: returns ErrChannelNotImplemented immediately.
func (a *PinterestStubAdapter) UpdateOrderStatus(_ context.Context, in fulfilment.ChannelStatusUpdate) error {
	a.recordOp("update_order_status")
	return wrapStubNotImplemented(PinterestChannelName, "update_order_status", in.ExternalOrderID)
}

// CreateListing implements channelport.ChannelOrderListing. Stub:
// returns ErrChannelNotImplemented immediately.
func (a *PinterestStubAdapter) CreateListing(_ context.Context, req channelport.ListingRequest) error {
	a.recordOp("create_listing")
	return wrapStubNotImplemented(PinterestChannelName, "create_listing", req.ProductID)
}

// Close implements lifecycle.Closer. No-op.
func (a *PinterestStubAdapter) Close(_ context.Context) error { return nil }

func (a *PinterestStubAdapter) recordOp(op string) {
	if a.metrics == nil {
		return
	}
	a.metrics.RecordStubChannelCall(a.tenantID, PinterestChannelName, op)
}

// Compile-time guards.
var (
	_ channelport.ChannelOrderListing = (*PinterestStubAdapter)(nil)
	_ fulfilment.ChannelStatusUpdater = (*PinterestStubAdapter)(nil)
)
