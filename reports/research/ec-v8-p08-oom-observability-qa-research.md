# EC v8 Pair 8 OOM Observability QA Research

> Date: 2026-05-12  
> Branch: `qa/v8-p08-oom-observability`  
> Scope: sanitized resource-probe review, EvoMap rollup evidence, leak/race checks, and Sentrux cleanup verification for Pair 8.

## Sources Reviewed

- Pair 8 MVP artifacts:
  - `reports/research/ec-v8-p08-oom-observability-research.md`
  - `docs/operations/v8-p08-oom-observability.md`
  - `internal/runtimeobs/resource_guard.go`
  - `internal/evomap/rollup.go`
- Operational resource surfaces:
  - `cursor-tools resource-probe-once`
  - `runx workspace doctor`
  - macOS `memory_pressure`
  - branch-local `sentrux gate .` inside `runx worktree run`
- Existing QA patterns:
  - `docs/operations/v731-resource-aware-orchestration-qa.md`
  - `docs/operations/v8-p04-image-editing-qa.md`
  - `internal/observability/spine/spine_qa_test.go`

## Decisions

1. Keep process inspection outside backend runtime.
   - Backend accepts sanitized counters only.
   - Raw process command lines, argv, shell snippets, paths, and host details
     are rejected by the resource-probe parser.
2. Distinguish Sentrux desktop from Sentrux MCP.
   - Desktop app count is the OOM-risk signal.
   - `sentrux --mcp` can remain active and should not trigger desktop cleanup
     alerts by itself.
3. QA should validate EvoMap rollup field survival.
   - Pair 8 MVP writes resource guard fields to capsules.
   - QA must prove daily rollups and dashboard snapshots preserve them.
4. Keep soak evidence bounded.
   - Use focused race/leak tests and generated sample capsules instead of
     intentionally driving the MacBook into memory pressure.

## RED Targets

1. `TestLoadProcessSnapshotFromResourceProbeUsesLatestSanitizedSample`
   - Fails until runtimeobs can parse sanitized resource-probe NDJSON and return
     the latest Sentrux desktop/MCP counts.
2. `TestLoadProcessSnapshotFromResourceProbeRejectsRawProcessCommand`
   - Fails until unsafe raw process fields are rejected before any value enters
     app logs, Agenttrace, or EvoMap.
3. `TestRenderCapsuleMarkdownIncludesResourceGuardFields`
   - Locks the Pair 8 rollup fields into rendered EvoMap markdown evidence.

## QA Carry-Forward

- Add runx/cursor-tools branch-local Sentrux wrapper support so future agents do
  not need to invoke `sentrux gate .` inside a worktree command.
- Add a fleet automation that records Sentrux desktop count after every sprint
  closeout and sends only major threshold alerts.
