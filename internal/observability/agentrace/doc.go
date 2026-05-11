// Package agentrace is the v6.2.0 production wiring for the
// pre/post-tool Agentrace pipeline (closes ADR-032 CF #5 +
// lessons-learned CF-11).
//
// The package owns three small responsibilities:
//
//  1. Forward `mc_api_*` Prometheus metric snapshots + tool-call
//     events into Agentrace NDJSON for the EvoLoop schema.
//  2. Buffer events behind a bounded ring + a single writer goroutine
//     (no raw `go func()`; backpressure when the ring is saturated).
//  3. Emit through a runx-aliased writer (filesystem path or external
//     command). Raw IPs / Tailscale addresses MUST NOT appear in code,
//     argv, or test fixtures (see no-shell-leak.mdc).
//
// The reader-side (KPI extraction from NDJSON for capsules) lives in
// `internal/evomap/agentrace_adapter.go`. This package only writes.
//
// Decomposition discipline (HARD GATE: complex_fn must stay <= 5):
//
//   - Adapter.Emit               (cyclomatic 4)
//   - Adapter.Close              (cyclomatic 3)
//   - Adapter.flush              (cyclomatic 4)
//   - Adapter.writerLoop         (cyclomatic 4)
//   - newRingBuffer / push / pop (each cyclomatic <= 3)
//
// Reuse evidence:
//   - Bounded ring + single writer pattern: internal/workerpool/pool.go
//   - NDJSON reader contract: internal/evomap/agentrace_adapter.go
//   - context.WithTimeout discipline: internal/api/handler/* v6.1.x
package agentrace
