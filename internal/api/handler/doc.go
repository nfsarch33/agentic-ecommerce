// Package handler is the v3.6.0 home for the analytics + agent
// activity HTTP surfaces (Epic 9 stories EC-9-1 + EC-9-2).
//
// Surfaces:
//
//   - GMVHandler  (EC-9-1): GET /api/v1/analytics/gmv (+ /by-channel,
//     /by-product); reads the gmv_daily_rollup materialized view
//     (migration 0016) via a small GMVRepository port.
//
//   - AgentActivitySSEHandler (EC-9-2): GET /api/v1/agent-activity/
//     stream; subscribes to the in-memory eventbus and dispatches
//     SSE events to the connected client with a 30s heartbeat and
//     per-client backpressure.
//
// Both handlers register themselves as lifecycle.Closer so the
// composition root (cmd/mc-api) can drain them on shutdown.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4 -- 9-sprint streak; v3.6.0 = sprint 10): per-
// handler bodies stay under cyclomatic 6 by splitting filter
// parsing, validation, and write-out into helpers.
package handler
