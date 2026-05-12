# EC v8 Pair 1 Research -- Marketplace Sync Core

**Date**: 2026-05-12  
**Branch**: `feat/v8-p01-marketplace-sync-core`  
**Scope**: shared marketplace sync core only; Shopify and Shopee adapters are follow-on pairs.

## Evidence Reviewed

- Current v7.5.1 handoff: `global-kb:handoff/2026-05-12-ec-v751-release-handoff.md`.
- Current v7 roadmap seed: `global-kb:backlog/ec-v7-15-pair-roadmap.md`.
- Existing marketplace registry: `internal/marketplace/registry.go`.
- Existing channel router DLQ pattern: `internal/agent/channel/router.go`.
- Existing bounded worker/resource discipline: `internal/workerpool/pool.go`, `internal/memwatch/backpressure.go`.
- Existing metric registry and observability spine: `internal/metrics/metrics.go`, `internal/observability/spine/spine.go`.

## Decision

Build a small internal sync core instead of adding Shopify/Shopee behavior directly to WooCommerce, marketplace plugin, or channel-router packages.

Rationale:

- The current `internal/marketplace` package owns plugin lifecycle, not product/order sync execution.
- The current channel router has a useful DLQ pattern, but it is event fan-out oriented and channel-specific.
- Pair 1 needs reusable idempotency, retry-to-DLQ, replay, reconciliation, and metric semantics before provider adapters exist.
- No goroutines are needed in the core. Batch or async execution should be supplied by the composition root through the existing workerpool pattern.

## Constraints

- TDD first: RED tests for idempotency, DLQ retry, replay dedupe, and reconciliation mismatch must fail before implementation.
- No live marketplace calls.
- No secrets, partner IDs, hostnames, or credential paths in docs or tests.
- Mem0 hot recall is degraded in this session: `mcp__mem0__.search_memory` returned socket hang-up. Git KB and local repo docs are source of truth for this sprint.

## Expected Core Shape

- `internal/marketplacesync.Engine`
- `Connector` interface for provider adapters.
- `Ledger` interface for idempotency state.
- `DLQ` interface for failed events.
- `Metrics` interface with three counters:
  - `ec_marketplace_sync_events_total`
  - `ec_marketplace_sync_dlq_total`
  - `ec_marketplace_replay_total`

## Acceptance

- Focused package tests pass under `go test ./internal/marketplacesync -count=1`.
- Race package tests pass.
- The new core remains synchronous and context-aware.
- Every failed event that exhausts attempts produces one DLQ record.
- Replayed already-completed events are skipped without duplicate connector calls.
