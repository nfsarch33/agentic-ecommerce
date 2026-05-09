// Package channel hosts the v3.3.0 Epic 3 + v3.4.0 Epic 4 channel
// agents that subscribe to ProductEnrichedEvent and publish to a
// specific external storefront (TikTok Shop, Facebook Shop, RedNote,
// etc).
//
// The TikTok agent is the v3.3.0 EC-3-2 entry point. It depends on
// the social.Client port (EC-3-1 implementation) so the composition
// root chooses the concrete adapter at startup. Saga rollback uses
// the existing eventbus + the TikTok client's idempotent
// CreateProduct + DeleteProduct primitives -- there is no external
// Temporal worker needed because the publish path is single-step
// from the agent's perspective.
//
// Resilience pillar (v2.10 baseline):
//
//   - Implements lifecycle.Closer.
//   - Subscribes via the existing eventbus.Consumer port (the agent
//     does not own the goroutine; the consumer dispatches).
//   - Workerpool is supplied by the composition root and used for
//     any concurrent fan-out (none today; reserved for v3.4 router).
//   - Errors typed and %w-wrapped via package sentinels.
//   - Tenant awareness: every event handled is gated on the typed
//     ProductEnrichedPayload TenantID; metric labels carry it.
//
// Cite skill: go-clean-architecture (port + adapter -- the agent
// depends on social.Client, not on the concrete TikTokShopClient).
package channel
