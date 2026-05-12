# EC v8 Pair 8 OOM Observability QA

> Date: 2026-05-12  
> Branch: `qa/v8-p08-oom-observability`  
> Scope: sanitized resource-probe parsing, EvoMap rollup survival, leak/race checks, Sentrux cleanup, and post-gate memory posture.

## Summary

Pair 8 QA adds a sanitized parser for runx/cursor-tools resource-probe NDJSON
so backend/runtime observability can consume resource posture without ingesting
raw process command lines. QA also locks the new resource guard fields into
EvoMap rollup markdown.

## Added QA Coverage

- `LoadProcessSnapshotFromResourceProbe` returns the latest sanitized sample
  from resource-probe NDJSON.
- The parser accepts both `memory_free_percent` and the current
  cursor-tools/runx `free_pct` field.
- Unsafe raw process command fields such as `process_cmdline` are rejected with
  `ErrUnsafeResourceProbe`.
- `ProcessSnapshot` now distinguishes:
  - Sentrux desktop process count
  - Sentrux MCP process count
  - memory free percentage
- EvoMap rollup markdown now has regression coverage for:
  - total resource guard alerts
  - max Sentrux desktop process count
  - total workerpool resizes

## TDD Evidence

RED:

```text
runx worktree run --repo ecommerce --branch qa/v8-p08-oom-observability -- /opt/homebrew/bin/rtk go test ./internal/runtimeobs ./internal/evomap -run 'TestLoadProcessSnapshotFromResourceProbe|TestRenderCapsuleMarkdownIncludesResourceGuardFields' -count=1

runtimeobs: LoadProcessSnapshotFromResourceProbe and ErrUnsafeResourceProbe undefined
```

Additional RED after checking the real runx probe payload:

```text
runx worktree run --repo ecommerce --branch qa/v8-p08-oom-observability -- /opt/homebrew/bin/rtk go test ./internal/runtimeobs -run TestLoadProcessSnapshotFromResourceProbeAcceptsRunxFreePct -count=1

TestLoadProcessSnapshotFromResourceProbeAcceptsRunxFreePct failed until free_pct fallback support was added.
```

GREEN:

```text
runx worktree run --repo ecommerce --branch qa/v8-p08-oom-observability -- /opt/homebrew/bin/rtk go test ./internal/runtimeobs ./internal/evomap -run 'TestLoadProcessSnapshotFromResourceProbe|TestRenderCapsuleMarkdownIncludesResourceGuardFields' -count=1

4 tests passed in 2 packages
```

## Validation

| Gate | Result | Evidence |
| --- | --- | --- |
| `git diff --check` | PASS | no whitespace findings |
| `cursor-tools docsync check --repo .` | PASS | documentation drift check OK |
| `runx shell-leak-scan --root . --include-docs` | PASS | 175 files scanned, no findings |
| Focused QA tests | PASS | 41 tests across 4 packages |
| Focused QA race tests | PASS | 64 tests across 5 packages, including workerpool goleak guard |
| Full backend race suite | PASS | 4321 tests across 113 packages |
| `make coverage-check` | PASS | race coverage `84.7% >= 83%` gate |
| `make govulncheck-scan` | PASS | no vulnerabilities found |
| `make build` | PASS | 8 binaries built |
| branch-local `sentrux gate .` | PASS | Quality `6041 -> 6042`, Coupling `0.04 -> 0.05`, Cycles `1`, God files `0`, no degradation |
| `runx cursor-tools resource-probe-once` | PASS | latest probe reported `free_pct=47` |
| Sentrux desktop process check | PASS | no `Sentrux.app/Contents/MacOS/sentrux` process found after gate |
| `memory_pressure` | PASS | system-wide free memory `47%` after heavy gates |

## Resource Probe Evidence

The durable probe file is:

```text
/Users/jason.lian/logs/runx/resource-probe.ndjson
```

Latest observed samples include the current `free_pct` shape:

```json
{"ts":"2026-05-13T01:33:02+10:00","event":"memory_pressure_probe","summary":"System-wide memory free percentage: 47%","free_pct":47}
```

The backend parser intentionally consumes only sanitized numeric fields. It
does not consume raw command lines, argv, shell snippets, or process paths.

## Carry-Forwards

- Extend `cursor-tools resource-probe-once` with explicit sanitized
  `sentrux_desktop_processes` and `sentrux_mcp_processes` fields so the backend
  parser can consume live counts without any process-line inspection.
- Add branch-aware `runx sentrux gate --repo <alias> --branch <name>` support to
  avoid direct branch-local `sentrux gate .` calls inside worktrees.
- Pair 9 should reuse the resource-probe parser for EvoLoop/DRL reward evidence
  instead of inventing another probe format.
