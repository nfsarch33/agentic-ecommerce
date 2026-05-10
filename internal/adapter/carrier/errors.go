// Package carrier wires AusPost + DHL shipping carrier clients for the
// v3.8.0 EC-7-3 shipping label generator. Each client is a thin HTTP
// adapter that exposes Quote (price + ETA) + CreateLabel (PDF +
// tracking number) so the fulfilment.ShippingLabelGenerator can run
// the cheapest-within-SLA selection without leaking carrier-specific
// detail upstream.
//
// Reuse evidence:
//   - HTTP client + base URL via env var pattern from v3.3.0 EC-3-1
//     internal/adapter/social/tiktok_shop_client.go.
//   - HMAC stdlib pattern (no third-party deps) from v3.3.0 EC-3-3
//     internal/webhook/tiktok_order.go.
//   - OAuth2 client_credentials follows the v3.4.0 EC-4-2
//     internal/adapter/social/facebook_shop_oauth.go shape; the
//     v3.8.0 DHL client reuses the same Token caching contract via
//     a small port so production composition wires the existing
//     credentials adapter without a second copy.
//   - Typed sentinel + %w wrapping per the package convention.
//   - VCR-style cassette tests use httptest.Server (mirrors the
//     existing internal/adapter/social/testdata/cassettes pattern;
//     no live calls, no third-party VCR library needed for v3.8.0).
package carrier

import "errors"

// EC-7-3 typed sentinels.
var (
	// ErrCarrierUnavailable signals the carrier API is unreachable
	// (network / 5xx / HMAC reject). Triggers fallback to the second
	// carrier in the cheapest-within-SLA selector.
	ErrCarrierUnavailable = errors.New("carrier: unavailable")

	// ErrLabelGenerationFailed signals a deterministic carrier
	// rejection (4xx body / malformed response). Does NOT trigger
	// the fallback path -- a deterministic failure is the same on
	// every carrier.
	ErrLabelGenerationFailed = errors.New("carrier: label generation failed")

	// ErrInvalidShippingAddress signals the input address failed
	// the carrier's pre-validation (missing postcode, unsupported
	// country, etc.).
	ErrInvalidShippingAddress = errors.New("carrier: invalid shipping address")

	// ErrSLANotMet signals every carrier's quoted ETA is outside
	// the requested SLA window. Surfaces directly to the operator
	// so they can manually intervene.
	ErrSLANotMet = errors.New("carrier: no carrier meets requested SLA")

	// ErrCarrierClientUnconfigured is returned by the client
	// constructors when the BaseURL is empty.
	ErrCarrierClientUnconfigured = errors.New("carrier: client unconfigured")
)
