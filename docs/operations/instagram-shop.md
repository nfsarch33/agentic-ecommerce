# Instagram Shop adapter (v3.9.1 EC-4-4 stub)

## State

The v3.9.1 sprint ships a **production-ready stub** of the Instagram
Shop adapter. The stub satisfies every port the existing channel
router and status propagator depend on so cmd/* binaries can register
the adapter today and swap a real Graph API client in v4.1.x without
touching the composition root.

The stub:

- Implements `channelport.ChannelOrderListing` (`ChannelName()` +
  `CreateListing(ctx, ListingRequest)`).
- Implements `fulfilment.ChannelStatusUpdater` (`ChannelName()` +
  `UpdateOrderStatus(ctx, ChannelStatusUpdate)`).
- Returns `social.ErrChannelNotImplemented` (typed sentinel; wraps
  channel + op + correlation id) for every operation.
- Returns within 10ms with no external I/O (verified by the unit
  test `TestInstagramStub_UpdateOrderStatusReturnsNotImplemented`).

The channel router (v3.4.0 `internal/agent/channel`) recognises the
`instagram` channel name via `channelport.IsStubChannel`. Dispatches
to the stub adapter therefore surface as
`ChannelStatusNotYetImplementedEvent` rather than enqueueing into the
DLQ; metric label `outcome=not_yet_implemented` keeps the dashboard
honest.

## Integration plan for v4.1.x

1. Replace `social.NewInstagramStubAdapter` with a real
   `social.NewInstagramShopClient` that wraps the Meta Graph API
   v21 `commerce/products` + `commerce/orders` endpoints. Reuse the
   v3.4.0 Facebook Shop OAuth + signing flows.
2. Wire the IG webhook receiver into `internal/api/webhook/` for
   inbound message + order events. Mirror the v3.3.0 EC-3-3 TikTok
   webhook HMAC-verify-then-parse pattern.
3. Drop `instagram` from `channelport.StubChannelNames`. The
   router's stub-recognition path will then no-op for IG and the
   dispatch metrics flip to `outcome=delivered`.
4. Update `internal/observability/v391_metrics.go` cardinality
   estimate to remove the `op=publish/update_order_status/create_listing`
   stub series and add IG-specific API call counters.

## Operational guard rails

- **Tenant isolation**: every stub call requires a non-empty
  `tenant_id`; constructor returns `ErrInstagramStubUnconfigured`
  otherwise.
- **No external I/O**: until v4.1.x lands, the stub MUST NOT issue
  any HTTP calls. The compile-time guards in `instagram_shop.go`
  deliberately exclude `*http.Client`.
- **Goleak**: `internal/adapter/social/leak_test.go` covers the
  package; the stub's no-op `Close` is verified.
