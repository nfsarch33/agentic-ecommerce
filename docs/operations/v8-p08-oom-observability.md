# EC v8 Pair 8 OOM Observability MVP

> Date: 2026-05-12  
> Branch: `feat/v8-p08-oom-observability`  
> Scope: resource guard alerts, Sentrux desktop process visibility, adaptive workerpool resize metrics, and Agenttrace/EvoMap wiring.

## Summary

Pair 8 MVP adds an additive OOM observability layer on top of the existing
memwatch, workerpool, metrics, Agenttrace, and EvoMap surfaces. It does not
change product request routing or marketplace/media behavior.

## Added Contracts

- `internal/runtimeobs.ResourceGuard` evaluates bounded runtime signals:
  - heap pressure
  - goroutine pressure
  - Sentrux desktop process count supplied by runx/cursor-tools probes
- `RuntimeObservability.ObserveResource` fans a resource evaluation into:
  - Prometheus runtime gauges and resource alert counters
  - Agenttrace `resource_alert` events
  - EvoMap NDJSON capsules for self-improvement rollups
- `workerpool.AdaptivePool` now accepts `AdaptiveMetrics` and emits:
  - `ec_workerpool_size`
  - `ec_workerpool_resize_total`
- `metrics.Registry` now exposes:
  - `ec_resource_guard_alerts_total{signal,severity}`
  - `ec_sentrux_desktop_process_count`
  - `ec_workerpool_size{pool}`
  - `ec_workerpool_resize_total{pool,direction}`
- EvoMap and the observability spine gained additive KPI fields:
  - `resource_guard_alerts_total`
  - `sentrux_desktop_process_count`
  - `workerpool_size`
  - `workerpool_resize_total`

## TDD Evidence

RED:

```text
runx worktree run --repo ecommerce --branch feat/v8-p08-oom-observability -- /opt/homebrew/bin/rtk go test ./internal/metrics ./internal/workerpool ./internal/runtimeobs ./internal/evomap -run 'TestV8ResourceMetricsRegisteredAndExposed|TestAdaptivePool_EmitsResizeMetrics|TestResourceGuard' -count=1

metrics: ResourceGuardAlertsTotal/SentruxDesktopProcessCount/WorkerpoolSize/WorkerpoolResizeTotal undefined
workerpool: unknown field Metrics in AdaptiveConfig
runtimeobs: NewResourceGuard/ResourceGuardConfig/ObserveResource/ProcessSnapshot undefined
evomap: ResourceGuardAlertsTotal/SentruxDesktopProcessCount fields undefined
```

GREEN:

```text
runx worktree run --repo ecommerce --branch feat-v8-p08-oom-observability -- /opt/homebrew/bin/rtk go test ./internal/metrics ./internal/workerpool ./internal/runtimeobs ./internal/evomap ./internal/observability/spine ./internal/observability/hooks -count=1

76 tests passed in 6 packages
```

## Validation

| Gate | Result | Evidence |
| --- | --- | --- |
| `git diff --check` | PASS | no whitespace findings |
| `cursor-tools docsync check --repo .` | PASS | documentation drift check OK |
| `runx shell-leak-scan --root .` | PASS | 56 files scanned, no findings |
| `runx shell-leak-scan --root . --include-docs` | PASS | 174 files scanned, no findings |
| Focused package tests | PASS | 81 tests across 7 packages |
| Focused package race tests | PASS | 76 tests across 6 packages |
| Full backend race suite | PASS | 4317 tests across 113 packages |
| `make coverage-check` | PASS | race coverage `84.8% >= 83%` gate |
| `make govulncheck-scan` | PASS | no vulnerabilities found |
| `make build` | PASS | 8 binaries built |
| `make compose-config-prod` | PASS | Docker Compose production config rendered |
| `make tf-fmt-check` | PASS | Terraform formatting check clean |
| `make tf-validate` | PASS | all Terraform modules and AWS/GCP stacks valid |
| `make tf-plan-contract` | PASS | AWS ECS and GCP Cloud Run plan contracts pass |
| branch-local `sentrux gate .` | PASS | Quality `6041 -> 6042`, Coupling `0.04`, Cycles `1`, God files `0`, no degradation |
| Sentrux desktop process check | PASS | no `Sentrux.app/Contents/MacOS/sentrux` process found after gate |
| Memory pressure | PASS | system-wide free memory `44%` after heavy gates |

Note: `runx sentrux gate --repo ecommerce` currently targets the canonical
checkout, not a named runx worktree. For branch-local evidence, this MVP used
`sentrux gate .` inside `runx worktree run`.

## No-Shell-Leak Boundary

The backend does not enumerate macOS processes or inspect Sentrux command
lines. It accepts only an integer Sentrux desktop process count from an
approved runx/cursor-tools probe. This keeps private host, path, and process
details out of app logs, Prometheus labels, Agenttrace labels, and EvoMap
capsules.

## QA Carry-Forward

Pair 8 QA should:

- run race tests for `internal/runtimeobs`, `internal/workerpool`,
  `internal/metrics`, `internal/evomap`, and `internal/observability/spine`;
- replay generated EvoMap NDJSON through `cmd/evomap-rollup`;
- verify resource probes distinguish Sentrux desktop apps from `sentrux --mcp`;
- confirm no desktop Sentrux processes remain after each sprint closeout;
- record full branch gate evidence and any wrapper limitations.
