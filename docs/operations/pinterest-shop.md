# Pinterest Shop adapter

> Last verified: 2026-05-12

## State

Pinterest now uses the production adapter in
`internal/adapter/social/pinterest_shop.go`. The earlier v3.9.1 stub has been
replaced by a Pinterest API v5 backed adapter with injectable `BaseURL` and
`HTTPClient` for deterministic tests.

The production adapter:

- Implements `channelport.ChannelOrderListing` (`ChannelName()` +
  `CreateListing(ctx, ListingRequest)`).
- Implements `fulfilment.ChannelStatusUpdater` (`ChannelName()` +
  `UpdateOrderStatus(ctx, ChannelStatusUpdate)`).
- Implements product pin publish, listing creation, conversion tracking, and
  webhook signature verification.
- Defaults to `DefaultPinterestBaseURL` only when callers do not inject a test
  base URL.
- Is covered by `internal/adapter/social/pinterest_shop_test.go` with
  `httptest.Server`.

The channel router can now treat `pinterest` as a real adapter when configured.
Live Pinterest API execution remains operator-gated; default CI must use
injected local fixtures only.

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
