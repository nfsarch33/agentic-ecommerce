# EC v8 Pair 9 Self-Improvement QA Research

> Date: 2026-05-13  
> Branch: `qa/v8-p09-self-improvement`  
> Scope: replay Agenttrace-derived evidence, validate reward logs, and prove
> only evidence-backed producer-reviewer decisions can be promoted.

## Sources Reviewed

- Pair 9 MVP:
  - `reports/research/ec-v8-p09-self-improvement-research.md`
  - `docs/operations/v8-p09-self-improvement.md`
  - `internal/selfimprove/evidence.go`
  - `internal/evomap/self_improvement_test.go`
- Existing replay and observability patterns:
  - `internal/observability/agentrace/adapter.go`
  - `internal/evomap/agentrace_adapter.go`
  - `internal/workflow/selftest/replay_harness.go`
  - `docs/operations/agentrace-hooks-setup.md`
  - `docs/operations/v8-p08-oom-observability-qa.md`

## Decisions

1. Replay should consume sanitized NDJSON, not live Agenttrace services.
   - QA must not require local Agentrace loopback, Mem0, LLM, VLM, or
     OmniParser availability.
2. Replay should be tolerant of malformed lines but strict about promotion.
   - Bad JSON lines and invalid evidence are reported with line numbers.
   - Valid rejected/rework evidence remains available for reports.
   - Only valid promoted evidence produces DRL reward artifacts.
3. Reward logs should be line-delimited JSON.
   - EvoLoop/DRL jobs can replay them with streaming readers and without
     parsing prose reports.
4. Keep replay APIs pure.
   - `io.Reader` and `io.Writer` surfaces keep tests deterministic and avoid
     filesystem/network coupling.

## RED Targets

1. `TestDecodeEvidenceReplayKeepsValidRowsAndRecordsRejects`
   - Fails until replay accepts valid evidence, records malformed/invalid rows,
     and does not abort the whole run.
2. `TestEncodeRewardArtifactsNDJSONRoundTrip`
   - Fails until promoted reward artifacts can be emitted as replayable NDJSON.
3. `TestDecodeEvidenceReplayPromotionRequiresArtifacts`
   - Fails until promotion without artifact refs is rejected during replay.
