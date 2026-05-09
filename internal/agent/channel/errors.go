package channel

import "errors"

// v3.3.0 EC-3-2 channel package sentinels.
var (
	// ErrChannelUnconfigured is returned when a required dependency
	// (Client / Publisher / TenantID) is missing.
	ErrChannelUnconfigured = errors.New("channel: agent unconfigured")

	// ErrChannelClosed is returned by Handle after Close.
	ErrChannelClosed = errors.New("channel: agent closed")

	// ErrChannelEnvelopeInvalid is returned when the dispatched
	// Event payload cannot be decoded into the typed payload struct.
	ErrChannelEnvelopeInvalid = errors.New("channel: envelope invalid")

	// ErrChannelTenantMismatch is returned when the envelope's
	// TenantID does not match the agent's configured TenantID. The
	// agent is single-tenant in v3.3.0; multi-tenant routing is the
	// v3.4.0 EC-4-3 channel router story.
	ErrChannelTenantMismatch = errors.New("channel: tenant mismatch")
)
