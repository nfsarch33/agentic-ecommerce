// File scope: v3.4.0 EC-4-2 Facebook Shop typed sentinels.
//
// META Commerce Manager (Graph API v21) requires its own error
// category set so callers can branch via errors.Is without parsing
// strings. The shape mirrors the v3.3.0 EC-3-1 TikTok sentinel
// pattern in errors.go: one var block, one wrapped %w everywhere.
//
// Resilience pillar (v2.10 baseline):
//   - Errors typed and %w-wrapped via the sentinels here.
//   - Tenant awareness: every public type (see facebook_shop_models.go)
//     carries TenantID; metric labels include {tenant_id, endpoint, status}.
//   - Credentials never appear on argv: the client reads
//     ECOMMERCE_FACEBOOK_APP_ID, ECOMMERCE_FACEBOOK_APP_SECRET, and
//     ECOMMERCE_FACEBOOK_PAGE_TOKEN via the composition root.
//
// Cite skill: go-clean-architecture (typed sentinels surface in the
// FacebookClient interface so the agent layer stays free of HTTP types).
package social

import "errors"

// Facebook Shop typed sentinels. Surface from the client + signing +
// OAuth code paths so callers branch on category without parsing
// strings.
var (
	// ErrFacebookAuthFailed is returned when the Graph API rejects
	// the long-lived page token (401/403) -- token expired, scopes
	// missing, or the app is in development mode for a non-tester.
	ErrFacebookAuthFailed = errors.New("facebook: auth failed")

	// ErrFacebookRateLimited is returned when META responds with
	// 429 or surfaces an x-app-usage / x-business-use-case-usage
	// throttle marker. Wrapped via fmt.Errorf("%w: ...") so callers
	// can errors.Is and apply exponential backoff (mirrors the
	// v3.1.0 Taobao adapter pattern).
	ErrFacebookRateLimited = errors.New("facebook: rate limited")

	// ErrFacebookSignatureMismatch is returned when the inbound
	// X-Hub-Signature-256 header does not match the recomputed
	// HMAC-SHA256 over the raw payload. Constant-time compared via
	// crypto/subtle.
	ErrFacebookSignatureMismatch = errors.New("facebook: signature mismatch")

	// ErrFacebookGraphAPIError is returned when the Graph API
	// returns a structured error envelope (the typical {error:
	// {message, type, code}} shape) outside the categories above.
	// The error.code + error.type are surfaced via fmt.Errorf so
	// dashboards can pivot on them.
	ErrFacebookGraphAPIError = errors.New("facebook: graph api error")

	// ErrFacebookUnconfigured is returned by the client constructor
	// when a required dependency (App ID, App Secret, Page Token,
	// Catalogue ID) is missing.
	ErrFacebookUnconfigured = errors.New("facebook: client unconfigured")

	// ErrFacebookClosed is returned by Client methods after Close.
	ErrFacebookClosed = errors.New("facebook: client closed")

	// ErrFacebookSecretTooShort is returned by the constructor +
	// signing path when the configured app secret is shorter than
	// MinFacebookSecretBytes.
	ErrFacebookSecretTooShort = errors.New("facebook: secret too short")

	// ErrFacebookBatchTooLarge is returned when a single Graph API
	// batch request exceeds the META 50/batch contract for the
	// /<catalog_id> endpoint OR the v3.4.0 100-product per-call
	// acceptance criterion (we chunk under the hood).
	ErrFacebookBatchTooLarge = errors.New("facebook: batch too large")

	// ErrFacebookInvalidResponse is returned when the API response
	// shape does not match the expected JSON contract.
	ErrFacebookInvalidResponse = errors.New("facebook: invalid response")
)
