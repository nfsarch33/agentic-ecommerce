# EC v8 Pair 9 Self-Improvement Research

> Date: 2026-05-13  
> Branch: `feat/v8-p09-self-improvement`  
> Scope: autoresearch producer-reviewer evidence, EvoLoop/DRL reward artifacts, Agenttrace-driven self-improvement reports, and replay-ready promotion rules.

## Sources Reviewed

- Active roadmap and handoff:
  - `global-kb:backlog/ec-v8-10-pair-roadmap.md`
  - `global-kb:handoff/2026-05-13-ec-v8-p08-oom-observability-handoff.md`
  - `global-kb:global-memories/daily-startup-prompt.md`
- Existing backend self-improvement surfaces:
  - `internal/observability/agentrace/adapter.go`
  - `internal/evomap/agentrace_adapter.go`
  - `internal/evomap/sink.go`
  - `internal/evomap/rollup.go`
  - `internal/coord/reward_signal.go`
  - `docs/operations/agentrace-hooks-setup.md`
  - `docs/operations/v8-p08-oom-observability-qa.md`
- Tooling/runtime evidence:
  - `runx doctor`: GREEN
  - `runx config check-aliases v302`: `38/38` aliases present
  - `runx history audit`: `Sensitive: 0`
  - `rtk verify`: hook integrity PASS
  - `runx cursor-tools resource-probe-once`: `free_pct=38`
  - Mem0 MCP: degraded with socket hang-up; Git KB remains durable truth
  - Context Mode MCP resources/templates: not exposed in this Codex surface

## Current System Facts

1. Agenttrace already has a bounded NDJSON adapter and a KPI transformer.
   Pair 9 should not add new live transports or raw loopback dependencies.
2. EvoMap capsules and rollups are the existing self-improvement ingestion path.
   Pair 9 should extend the additive KPI schema rather than invent a second
   artifact format.
3. `coord.RewardSignal` is already the DRL reward value type. Pair 9 should
   generate compatible reward artifacts from reviewed evidence instead of
   creating another reward struct for training consumers.
4. Pair 8 explicitly carried forward reuse of sanitized resource/evidence
   parsers and bounded observability. Pair 9 should remain pure and replayable:
   no local LLM/VLM, no live HTTP calls, no process command capture.

## Decisions

1. Add a small `internal/selfimprove` package.
   - It will model producer-reviewer evidence, validation, report rendering, and
     reward artifact conversion.
   - It will be pure Go with no filesystem or network dependency.
2. Require evidence-backed promotion.
   - Promoted evidence needs non-empty artifact refs, distinct producer and
     reviewer identifiers, and a bounded reward value.
   - Rejected/rework evidence is still reported but does not become a promote
     reward artifact.
3. Emit additive EvoMap KPIs.
   - New fields track total reviewed evidence, promoted/rejected/rework counts,
     mean reward, and Agenttrace input count.
   - Rollups preserve these fields for EvoLoop/DRL consumers.
4. Keep reports replay-stable.
   - Markdown output is deterministic for a supplied timestamp and evidence
     slice, so QA can replay Agenttrace-derived inputs without relying on a live
     agent session.

## RED Targets

1. `TestValidateEvidenceRequiresProducerReviewerAndArtifacts`
   - Fails until evidence validation rejects missing producer/reviewer,
     same-person producer/reviewer, empty artifacts, and out-of-range rewards.
2. `TestBuildReportSummarisesProducerReviewerEvidence`
   - Fails until a deterministic markdown report captures promoted/rejected
     evidence, Agenttrace counts, artifact refs, and reward mean.
3. `TestRewardArtifactsOnlyPromoteEvidenceBackedDecisions`
   - Fails until only promoted evidence becomes `coord.RewardSignal` artifacts.
4. `TestAggregateSelfImprovementKPIs`
   - Fails until EvoMap capsules and rollups preserve self-improvement KPI
     fields.

## Out of Scope for MVP

- Live LLM reviewer execution.
- Local OmniParser or VLM workloads.
- Mem0 writes while the MCP endpoint is degraded.
- Automatic promotion into production policy. Pair 9 MVP only creates
  replayable evidence and reward artifacts; QA validates replay and promotion
  rules.
