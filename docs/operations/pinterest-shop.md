# Pinterest Shop adapter (v3.9.1 EC-4-4 stub)

## State

The v3.9.1 sprint ships a **production-ready stub** of the Pinterest
Shop adapter. Like the Instagram stub, the Pinterest stub satisfies
every port the existing channel router and status propagator depend
on so cmd/* binaries can register the adapter today and swap a real
Pinterest Catalog API client in v4.1.x without touching the
composition root.

The stub:

- Implements `channelport.ChannelOrderListing` (`ChannelName()` +
  `CreateListing(ctx, ListingRequest)`).
- Implements `fulfilment.ChannelStatusUpdater` (`ChannelName()` +
  `UpdateOrderStatus(ctx, ChannelStatusUpdate)`).
- Returns `social.ErrChannelNotImplemented` (typed sentinel; wraps
  channel + op + correlation id) for every operation.
- Returns within 10ms with no external I/O (verified by the unit
  test `TestPinterestStub_UpdateOrderStatusReturnsNotImplemented`).

The channel router (v3.4.0 `internal/agent/channel`) recognises the
`pinterest` channel name via `channelport.IsStubChannel`. Dispatches
to the stub adapter therefore surface as
`ChannelStatusNotYetImplementedEvent` rather than enqueueing into the
DLQ; metric label `outcome=not_yet_implemented` keeps the dashboard
honest.

## Integration plan for v4.1.x

1. Replace `social.NewPinterestStubAdapter` with a real
   `social.NewPinterestShopClient` that wraps the Pinterest Catalog
   API v5 product endpoints. Reuse the v3.4.0 Facebook Shop OAuth +
   signing flows where possible.
2. Wire Pinterest pin status updates into `internal/agent/fulfilment`.
   Pinterest's order webhook delivery is staged behind the
   business-account approval gate; the v4.1.x sprint plan covers
   the application + sandbox enablement steps.
3. Drop `pinterest` from `channelport.StubChannelNames`. The
   router's stub-recognition path will then no-op for Pinterest and
   the dispatch metrics flip to `outcome=delivered`.
4. Update `internal/observability/v391_metrics.go` cardinality
   estimate to remove the per-op stub series and add Pinterest API
   call counters.

## Operational guard rails

- **Tenant isolation**: every stub call requires a non-empty
  `tenant_id`; constructor returns `ErrPinterestStubUnconfigured`
  otherwise.
- **No external I/O**: until v4.1.x lands, the stub MUST NOT issue
  any HTTP calls.
- **Goleak**: `internal/adapter/social/leak_test.go` covers the
  package; the stub's no-op `Close` is verified.
