# EC v8 Pair 9 Self-Improvement MVP

> Date: 2026-05-13  
> Branch: `feat/v8-p09-self-improvement`  
> Scope: autoresearch producer-reviewer evidence, Agenttrace-driven reports,
> EvoMap/EvoLoop KPI fields, and DRL reward artifacts.

## Summary

Pair 9 MVP adds a pure `internal/selfimprove` package for replayable
producer-reviewer evidence. It converts promoted, evidence-backed decisions
into existing `coord.RewardSignal` artifacts and adds additive EvoMap and
Prometheus fields for self-improvement and Agenttrace evidence counts.

The implementation intentionally does not run live LLM/VLM/OmniParser work and
does not depend on Mem0 availability. Mem0 remained degraded during the sprint
with socket hang-up responses, so Git KB and repo artifacts are the durable
evidence source.

## Added Behavior

- `ValidateEvidence` rejects:
  - missing producer/reviewer identifiers
  - identical producer and reviewer
  - empty artifact refs
  - unknown decisions
  - reward values outside `[-1, 1]`
- `BuildReport` renders deterministic markdown reports with:
  - reviewed/promoted/rejected/rework counts
  - reward mean
  - producer-reviewer chain
  - Agenttrace tool call, bottleneck, error, and parallelism evidence
  - artifact references
- `RewardArtifacts` emits `coord.RewardSignal` only for promoted evidence.
- `EvoMapKPIs` converts evidence into additive EvoMap KPI fields.
- EvoMap rollups now preserve:
  - total self-improvement evidence
  - promoted/rejected/rework counts
  - mean self-improvement reward
  - total Agenttrace evidence inputs
- Prometheus metrics added:
  - `ec_self_improvement_evidence_total{decision}`
  - `ec_self_improvement_reward`
  - `ec_agentrace_evidence_total{source}`

## TDD Evidence

RED:

```text
runx worktree run --repo ecommerce --branch feat/v8-p09-self-improvement -- \
  zsh -lc 'GOSUMDB=sum.golang.org /opt/homebrew/bin/rtk go test ./internal/selfimprove ./internal/evomap \
  -run "TestValidateEvidence|TestBuildReport|TestRewardArtifacts|TestAggregateSelfImprovement|TestRenderCapsuleMarkdownIncludesSelfImprovement" -count=1'

selfimprove: Evidence, DecisionPromote, AgentraceSummary, ValidateEvidence,
BuildReport, RewardArtifacts undefined.
evomap: SelfImprovement* and AgentraceEvidence fields undefined.
```

Second RED:

```text
runx worktree run --repo ecommerce --branch feat/v8-p09-self-improvement -- \
  zsh -lc 'GOSUMDB=sum.golang.org /opt/homebrew/bin/rtk go test ./internal/selfimprove ./internal/metrics \
  -run "TestEvoMapKPIsSummariseEvidence|TestV8SelfImprovementMetricsRegisteredAndExposed" -count=1'

selfimprove: EvoMapKPIs undefined.
metrics: SelfImprovementEvidenceTotal, SelfImprovementReward, and
AgentraceEvidenceTotal undefined.
```

GREEN:

```text
runx worktree run --repo ecommerce --branch feat/v8-p09-self-improvement -- \
  zsh -lc 'GOSUMDB=sum.golang.org /opt/homebrew/bin/rtk go test ./internal/selfimprove ./internal/evomap ./internal/metrics \
  -run "TestValidateEvidence|TestBuildReport|TestRewardArtifacts|TestEvoMapKPIs|TestAggregateSelfImprovement|TestRenderCapsuleMarkdownIncludesSelfImprovement|TestV8SelfImprovementMetricsRegisteredAndExposed" -count=1'

13 tests passed in 3 packages.
```

Focused package validation:

```text
runx worktree run --repo ecommerce --branch feat/v8-p09-self-improvement -- \
  zsh -lc 'GOSUMDB=sum.golang.org /opt/homebrew/bin/rtk go test ./internal/selfimprove ./internal/evomap ./internal/metrics ./internal/coord ./internal/observability/agentrace -count=1'

113 tests passed in 5 packages.
```

## Validation

| Gate | Result | Evidence |
| --- | --- | --- |
| `git diff --check` | PASS | no whitespace findings |
| `runx cursor-tools docsync check --repo .` | PASS | documentation drift check OK |
| `runx shell-leak-scan --root . --include-docs` | PASS | 178 files scanned, no findings |
| Focused tests | PASS | 13 tests across 3 packages |
| Focused package tests | PASS | 113 tests across 5 packages |
| Focused race tests | PASS | 113 tests across 5 packages |
| Full backend race suite | PASS | 4334 tests across 114 packages |
| `make coverage-check` | PASS | race coverage `84.8% >= 83%` |
| `make govulncheck-scan` | PASS | no vulnerabilities found |
| `make build` | PASS | 8 binaries built |
| `make compose-config-prod` | PASS | compose production config rendered |
| `make tf-fmt-check` | PASS | Terraform formatting clean |
| `make tf-validate` | PASS | AWS/GCP modules and roots valid |
| `make tf-plan-contract` | PASS | AWS ECS and GCP Cloud Run plan contracts generated |
| branch-local `sentrux gate .` | PASS | Quality `6041 -> 6042`, Coupling `0.04 -> 0.05`, Cycles `1`, God files `0` |
| `runx cursor-tools resource-probe-once` | PASS | post-gate `free_pct=40` |
| Sentrux desktop process check | PASS | transient PID was gone by `ps`; no lingering desktop process confirmed |

## Notes

- Branch-local Go commands required `GOSUMDB=sum.golang.org` because the
  ambient shell had `GOSUMDB=off`, which prevented verification of the pinned
  Go toolchain before tests could compile.
- Context Mode MCP resources/templates were not exposed in this Codex app
  surface during this run.
- Heavy model execution remains out of scope for the MacBook and must stay on
  approved remote runx aliases.
