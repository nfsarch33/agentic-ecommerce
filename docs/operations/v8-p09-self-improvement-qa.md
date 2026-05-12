# EC v8 Pair 9 Self-Improvement QA

> Date: 2026-05-13  
> Branch: `qa/v8-p09-self-improvement`  
> Scope: replay Agenttrace-derived evidence, validate reward logs, and prove
> only evidence-backed improvements are promoted.

## Summary

Pair 9 QA adds replay coverage for the self-improvement evidence contract. The
new replay helper consumes sanitized NDJSON evidence, keeps valid rows, records
malformed or invalid rows with line numbers, and emits promoted reward artifacts
as streaming NDJSON for EvoLoop/DRL consumers.

The QA path remains pure and deterministic: no live Agenttrace loopback, Mem0,
LLM, VLM, OmniParser, filesystem, or network dependency is required.

## Added QA Coverage

- `DecodeEvidenceReplay`:
  - accepts valid evidence rows
  - skips empty lines
  - records malformed JSON with line numbers
  - records validation failures with line numbers
  - rejects promotions without artifact refs
- `EncodeRewardArtifactsNDJSON`:
  - writes one reward artifact per line
  - preserves `coord.RewardSignal` policy and artifact refs
  - remains stream-reader friendly for EvoLoop/DRL replay jobs

## TDD Evidence

RED:

```text
runx worktree run --repo ecommerce --branch qa/v8-p09-self-improvement -- \
  zsh -lc 'GOSUMDB=sum.golang.org /opt/homebrew/bin/rtk go test ./internal/selfimprove \
  -run "TestDecodeEvidenceReplay|TestEncodeRewardArtifacts" -count=1'

selfimprove: DecodeEvidenceReplay, ReplayResult, and
EncodeRewardArtifactsNDJSON undefined.
```

GREEN:

```text
runx worktree run --repo ecommerce --branch qa/v8-p09-self-improvement -- \
  zsh -lc 'GOSUMDB=sum.golang.org /opt/homebrew/bin/rtk go test ./internal/selfimprove \
  -run "TestDecodeEvidenceReplay|TestEncodeRewardArtifacts" -count=1'

3 tests passed in 1 package.
```

## Validation

| Gate | Result | Evidence |
| --- | --- | --- |
| `git diff --check` | PASS | no whitespace findings |
| `runx cursor-tools docsync check --repo .` | PASS | documentation drift check OK |
| `runx shell-leak-scan --root . --include-docs` | PASS | 180 files scanned, no findings |
| Focused replay tests | PASS | 3 tests in `internal/selfimprove` |
| Focused package tests | PASS | 116 tests across 5 packages |
| Focused package race tests | PASS | 116 tests across 5 packages |
| Full backend race suite | PASS | 4337 tests across 114 packages |
| `make coverage-check` | PASS | race coverage `84.8% >= 83%` |
| `make govulncheck-scan` | PASS | no vulnerabilities found |
| `make build` | PASS | 8 binaries built |
| `make compose-config-prod` | PASS | compose production config rendered |
| `make tf-fmt-check` | PASS | Terraform formatting clean |
| `make tf-validate` | PASS | AWS/GCP modules and roots valid |
| `make tf-plan-contract` | PASS | AWS ECS and GCP Cloud Run plan contracts generated |
| branch-local `sentrux gate .` | PASS | Quality `6041 -> 6042`, Coupling `0.04 -> 0.04`, Cycles `1`, God files `0` |
| `runx cursor-tools resource-probe-once` | PASS | post-gate `free_pct=45` |
| Sentrux desktop process check | PASS | transient PID disappeared by `ps`; no lingering desktop process confirmed |

## Carry-Forwards

- Pair 10 final hardening should include the Pair 9 self-improvement report and
  reward artifact schema in the final v8 release checklist.
- Mem0 remained degraded during Pair 9 MVP; do not depend on hot memory for
  replay evidence until the endpoint is healthy again.
