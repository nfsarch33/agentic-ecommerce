// Package social hosts the v3.3.0 Epic 3 TikTok Shop adapter clients
// (and, in later sprints, Facebook Shop / Instagram / Pinterest).
//
// This file pins the typed sentinels every TikTok Shop call path
// returns. Tests + agent layers branch via errors.Is. Wrapping is
// done with %w everywhere per go-clean-architecture + the existing
// internal/adapter/china pattern.
//
// Resilience pillar (v2.10 baseline):
//   - The adapter clients implement lifecycle.Closer.
//   - HTTP work is request-scoped (no goroutines). Long-running
//     order webhook + inventory sync paths use internal/workerpool.
//   - All errors typed and %w-wrapped via the sentinels here.
//   - Tenant awareness: every public type carries TenantID; metric
//     labels include {tenant_id, endpoint, status}.
//   - Credentials never appear on argv: the client reads
//     ECOMMERCE_TIKTOK_CLIENT_ID, ECOMMERCE_TIKTOK_CLIENT_SECRET, and
//     ECOMMERCE_TIKTOK_WEBHOOK_SECRET via the composition root.
//
// Cite skill: go-clean-architecture (port + adapter; the typed
// sentinels surface in the Client interface so the agent layer
// stays free of HTTP types) and go-security-review (HMAC verify-
// then-parse; constant-time compare; strict secret length floor).
package social

import "errors"

// TikTok Shop typed sentinels. Surface from the client + signing +
// OAuth code paths so callers branch on category without parsing
// strings.
var (
	// ErrTikTokAuthFailed is returned when the OAuth2 PKCE flow
	// rejects the operator-supplied code/refresh token, or when a
	// signed request comes back 401/403 (token expired, scopes
	// missing, app suspended).
	ErrTikTokAuthFailed = errors.New("tiktok: auth failed")

	// ErrTikTokRateLimited is returned when TikTok responds with
	// 429. Wrapped via fmt.Errorf("%w: ...", ErrTikTokRateLimited,
	// ...) so callers can errors.Is and apply exponential backoff
	// matching the v3.1.0 Taobao adapter pattern.
	ErrTikTokRateLimited = errors.New("tiktok: rate limited")

	// ErrTikTokSignatureMismatch is returned when the HMAC signature
	// computed over the canonical form does not match the supplied
	// signature (incoming webhook OR outgoing response signature
	// verification). Constant-time compared via crypto/subtle.
	ErrTikTokSignatureMismatch = errors.New("tiktok: signature mismatch")

	// ErrTikTokInvalidResponse is returned when the API response
	// shape does not match the expected JSON contract (missing
	// required field, unexpected envelope code, malformed body).
	ErrTikTokInvalidResponse = errors.New("tiktok: invalid response")

	// ErrTikTokUnconfigured is returned by the client constructor
	// when a required dependency (client id / secret, webhook
	// secret, base URL) is missing. Production composition root
	// fails fast at boot rather than silently falling through.
	ErrTikTokUnconfigured = errors.New("tiktok: client unconfigured")

	// ErrTikTokClosed is returned by Client methods after Close.
	ErrTikTokClosed = errors.New("tiktok: client closed")

	// ErrTikTokSecretTooShort is returned by NewClient + the webhook
	// verifier when a configured secret is shorter than the v2.10
	// MinSecretBytes floor (mirrors internal/billing.MinWebhookSecretBytes).
	ErrTikTokSecretTooShort = errors.New("tiktok: secret too short")

	// ErrTikTokSignatureMalformed is returned when a webhook header
	// is present but cannot be parsed (missing t=, missing s=).
	ErrTikTokSignatureMalformed = errors.New("tiktok: signature malformed")

	// ErrTikTokEventTooOld is returned when the webhook timestamp is
	// older than the configured tolerance. Replay protection floor.
	ErrTikTokEventTooOld = errors.New("tiktok: webhook event too old")

	// ErrTikTokMissingSignature is returned when the webhook header
	// is missing entirely.
	ErrTikTokMissingSignature = errors.New("tiktok: webhook signature missing")
)
