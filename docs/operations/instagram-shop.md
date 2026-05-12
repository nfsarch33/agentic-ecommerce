# Instagram Shop adapter

> Last verified: 2026-05-12

## State

Instagram now uses the production adapter in
`internal/adapter/social/instagram_shop.go`. The earlier v3.9.1 stub has been
replaced by a Graph API backed adapter with injectable `BaseURL` and
`HTTPClient` for deterministic tests.

The production adapter:

- Implements `channelport.ChannelOrderListing` (`ChannelName()` +
  `CreateListing(ctx, ListingRequest)`).
- Implements `fulfilment.ChannelStatusUpdater` (`ChannelName()` +
  `UpdateOrderStatus(ctx, ChannelStatusUpdate)`).
- Implements product publish, listing creation, order status updates,
  recent-order fetch, and webhook signature verification.
- Defaults to `DefaultInstagramBaseURL` only when callers do not inject a
  test base URL.
- Is covered by `internal/adapter/social/instagram_shop_test.go` with
  `httptest.Server`.

The channel router can now treat `instagram` as a real adapter when configured.
Live Graph API execution remains operator-gated; default CI must use injected
local fixtures only.

## Mock / Live Boundary

- Mock path: inject `BaseURL` + `HTTPClient` and use `httptest.Server`.
- Live path: requires app credentials and account approval, and is
  operator-gated per `docs/operations/social-sandbox-readiness.md`.
- Secrets: never commit app secrets or access tokens; never place them on argv.

## Operational guard rails

- **Tenant isolation**: constructor requires a non-empty `tenant_id`.
- **Credential checks**: constructor requires app ID, sufficiently long app
  secret, and access token.
- **HTTP boundary**: production calls use the configured `BaseURL`; tests must
  inject local servers.
- **Goleak**: `internal/adapter/social/leak_test.go` covers the
  package; `Close` marks the adapter closed.
