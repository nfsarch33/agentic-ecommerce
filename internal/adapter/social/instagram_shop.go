// File scope: v3.9.1 EC-4-4 -- Instagram Shop stub adapter.
//
// Production-ready facade for the v4.1.x Instagram Shop integration.
// Implements the same trio of ports the channel router + propagator
// expect (channel.ChannelAdapter, channel.ChannelOrderListing,
// fulfilment.ChannelStatusUpdater) so cmd/* binaries can register
// the stub today and swap the real adapter in v4.1.x without
// changing the composition root.
//
// Behaviour: every operation returns ErrChannelNotImplemented within
// 10ms (no external I/O, no allocations beyond the wrapped error).
//
// Cite skill: go-clean-architecture (port + adapter; the stubs
// satisfy the existing fulfilment.ChannelStatusUpdater + new
// channel.ChannelOrderListing ports without taking on any stack
// dependency that future production wiring will not need).
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

// InstagramChannelName is the canonical channel label.
const InstagramChannelName = "instagram"

// ErrInstagramStubUnconfigured is returned by the constructor when
// a required dependency is missing.
var ErrInstagramStubUnconfigured = errors.New("instagram stub: unconfigured")

// InstagramStubAdapter is the v3.9.1 EC-4-4 Instagram Shop stub.
// Mirrors the TikTok adapter shape so the v4.1.x production wire
// stays a drop-in replacement.
type InstagramStubAdapter struct {
	tenantID string
	metrics  StubChannelMetrics
}

// NewInstagramStubAdapter constructs the stub adapter. metrics is
// optional (nil = no-op).
func NewInstagramStubAdapter(metrics StubChannelMetrics, tenantID string) (*InstagramStubAdapter, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrInstagramStubUnconfigured)
	}
	return &InstagramStubAdapter{tenantID: tenantID, metrics: metrics}, nil
}

// Name implements channel.ChannelAdapter.
func (a *InstagramStubAdapter) Name() string { return InstagramChannelName }

// ChannelName implements both channel.ChannelOrderListing AND
// fulfilment.ChannelStatusUpdater.
func (a *InstagramStubAdapter) ChannelName() string { return InstagramChannelName }

// Publish implements channel.ChannelAdapter. Stub: returns
// ErrChannelNotImplemented immediately.
func (a *InstagramStubAdapter) Publish(_ context.Context, payload eventbus.ProductEnrichedPayload) error {
	a.recordOp("publish")
	return wrapStubNotImplemented(InstagramChannelName, "publish", payload.ProductID)
}

// UpdateOrderStatus implements fulfilment.ChannelStatusUpdater.
// Stub: returns ErrChannelNotImplemented immediately.
func (a *InstagramStubAdapter) UpdateOrderStatus(_ context.Context, in fulfilment.ChannelStatusUpdate) error {
	a.recordOp("update_order_status")
	return wrapStubNotImplemented(InstagramChannelName, "update_order_status", in.ExternalOrderID)
}

// CreateListing implements channelport.ChannelOrderListing. Stub:
// returns ErrChannelNotImplemented immediately.
func (a *InstagramStubAdapter) CreateListing(_ context.Context, req channelport.ListingRequest) error {
	a.recordOp("create_listing")
	return wrapStubNotImplemented(InstagramChannelName, "create_listing", req.ProductID)
}

// Close implements lifecycle.Closer. No-op.
func (a *InstagramStubAdapter) Close(_ context.Context) error { return nil }

func (a *InstagramStubAdapter) recordOp(op string) {
	if a.metrics == nil {
		return
	}
	a.metrics.RecordStubChannelCall(a.tenantID, InstagramChannelName, op)
}

// Compile-time guards: ensure the stub satisfies the v3.8.0
// propagator + v3.9.1 listing port. The router-side
// channel.ChannelAdapter compile guard lives in cmd/* composition
// roots to avoid an import cycle (the router's package imports
// social for ComputeTikTokSignature; social would otherwise
// import the router back).
var (
	_ channelport.ChannelOrderListing = (*InstagramStubAdapter)(nil)
	_ fulfilment.ChannelStatusUpdater = (*InstagramStubAdapter)(nil)
)

// wrapStubNotImplemented produces a typed error wrapping
// ErrChannelNotImplemented with channel + op + correlation_id so
// callers can branch via errors.Is without parsing the message.
func wrapStubNotImplemented(channelName, op, correlationID string) error {
	if correlationID == "" {
		return fmt.Errorf("%w: channel=%s op=%s", ErrChannelNotImplemented, channelName, op)
	}
	return fmt.Errorf("%w: channel=%s op=%s correlation_id=%s", ErrChannelNotImplemented, channelName, op, correlationID)
}
