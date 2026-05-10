// File scope: v3.4.0 EC-4-3 cross-platform channel router sentinels.
//
// The router fans an enriched product event out to one or more
// channel adapters. The sentinels here let callers branch on
// outcome (no match, delivered, DLQ) via errors.Is without parsing
// strings.
package channel

import "errors"

// EC-4-3 typed sentinels.
var (
	// ErrNoMatchingChannel is returned by ChannelRouter.HandleEvent
	// (wrapped) when zero channels in the configured route table
	// matched the inbound payload. The router records a no_match
	// outcome metric and skips the dispatch entirely; the operator
	// can configure a "fallback" channel matcher that always
	// returns true to prevent this case in production.
	ErrNoMatchingChannel = errors.New("router: no matching channel")

	// ErrChannelDelivered is the per-channel success sentinel
	// surfaced on ChannelDispatchResult.Cause so callers can
	// errors.Is(result.Cause, ErrChannelDelivered) when computing
	// per-channel dashboards. It is NOT returned from HandleEvent
	// (a successful dispatch returns nil); it is only the marker
	// inside the typed result envelope.
	ErrChannelDelivered = errors.New("router: channel delivered")

	// ErrChannelDLQ is returned by ChannelRouter.HandleEvent
	// (wrapped) when one or more channels failed to deliver and
	// the failure was enqueued in the DLQ. Callers can branch on
	// this sentinel to escalate (operator alert, EvoMap signal).
	ErrChannelDLQ = errors.New("router: channel dlq")

	// ErrRouterUnconfigured is returned when a required dependency
	// is missing.
	ErrRouterUnconfigured = errors.New("router: unconfigured")

	// ErrRouterClosed is returned by HandleEvent after Close.
	ErrRouterClosed = errors.New("router: closed")

	// ErrChannelNotYetImplemented is the per-channel marker the
	// v3.9.1 EC-4-4 router-side recognition uses to flag a stub
	// adapter call. Returned in ChannelDispatchResult.Cause for
	// stub channels (Instagram + Pinterest) so callers can branch
	// without parsing strings. The router emits the typed
	// ChannelStatusNotYetImplementedEvent rather than enqueueing
	// the call into the DLQ.
	ErrChannelNotYetImplemented = errors.New("router: channel not yet implemented (stub)")
)
