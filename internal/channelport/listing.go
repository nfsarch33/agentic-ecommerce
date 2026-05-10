// Package channelport hosts the v3.9.1 EC-4-4 channel-listing port +
// stub-channel recognition helpers. It deliberately depends on
// nothing else so the existing internal/agent/channel and
// internal/adapter/social packages can both import it without
// creating a cycle.
//
// Cite skill: go-clean-architecture (port + adapter; this package
// owns the typed ListingRequest envelope + the ChannelOrderListing
// port without bringing in any concrete adapter implementation).
package channelport

import (
	"context"
	"strings"
	"time"
)

// StubChannelNames lists the channel names that are
// production-ready stubs today (Instagram + Pinterest). The router
// pivots dispatchOne on this set to emit
// ChannelStatusNotYetImplemented without DLQ-enqueueing.
var StubChannelNames = map[string]struct{}{
	"instagram": {},
	"pinterest": {},
}

// IsStubChannel returns true if the channel name has a stub adapter
// today. Cheap; pure; no allocations on the hot path.
func IsStubChannel(name string) bool {
	_, ok := StubChannelNames[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// ListingRequest is the typed payload the EC-4-4 ChannelOrderListing
// port consumes. Production-ready adapters in v4.1.x will project
// this into the IG Graph API + Pinterest Catalog API request shapes.
// The stubs accept the request unchanged, validate the required
// fields, and return ErrChannelNotImplemented (defined in the
// adapter package).
type ListingRequest struct {
	TenantID      string
	ProductID     string
	Channel       string
	Title         string
	Description   string
	PriceAUDCents int
	ImageURL      string
	OccurredAt    time.Time
}

// ChannelOrderListing is the small port the v3.9.1 EC-4-4 stubs
// implement. Production-ready adapters in v4.1.x will mirror the
// existing TikTokListingAgent / FacebookShop publish paths; the
// port is deliberately narrow (single CreateListing entry) so
// future implementations stay focused.
type ChannelOrderListing interface {
	ChannelName() string
	CreateListing(ctx context.Context, req ListingRequest) error
}
