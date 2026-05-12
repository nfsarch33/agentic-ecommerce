# v7.3.1 Resource-Aware Orchestration QA

**Recorded**: 2026-05-12T12:17:04+10:00  
**Pair**: v7 Pair 4 QA/Retro  
**Branch**: `qa/v731-resource-aware-orchestration-qa`

## Scope

This QA slice validates the v7.3.0 resource-aware orchestration MVP after merge.
It keeps the code change narrow: prove the agent scheduler is now observable
through the existing workerpool metrics hooks and that the `mc-api` server owns
the scheduler lifecycle during shutdown.

## Findings

- `internal/agent.SchedulerOptions` now accepts `workerpool.PoolMetrics`.
- `internal/agent.NewScheduler` passes those metrics into the scheduler
  workerpool so `agent-scheduler` active/rejected samples flow through the same
  hook surface used by the rest of the runtime.
- `cmd/mc-api.schedulerOptions` wires `ecHooks.Load().Pool` into scheduler
  construction, including lazy scheduler creation through `ensureAgentScheduler`.
- `cmd/mc-api.mainImpl` starts observability before constructing the server so
  `newServer` can see the current hooks.
- `server.Close` now closes the agent scheduler under the existing shutdown
  timeout before running process cleanups.

## TDD Evidence

`TestSchedulerDispatchEmitsWorkerpoolMetrics` was added before the metrics
wiring. The RED state failed to compile because `SchedulerOptions` had no
`Metrics` field. The GREEN state proves `agent-scheduler` active count rises
while a run is executing and returns to zero after the run drains.

`TestSchedulerOptionsUsesCurrentObservabilityHooks` was added before the
composition helper. The RED state failed to compile because `schedulerOptions`
did not exist. The GREEN state proves the `mc-api` composition root attaches
the current observability workerpool hook to scheduler options.

`TestServerCloseClosesAgentScheduler` was added before the lifecycle close. The
RED state showed `server.Close` left the scheduler open:

```text
submit after server close err=<nil> want ErrSchedulerClosed
```

The GREEN state closes the scheduler during server shutdown and rejects future
submissions with `ErrSchedulerClosed`.

## Detached Async Carry-Forward

Pair 4 QA reviewed the remaining detached async surfaces found by read-only
subagent audits. They remain open for the next resource-hardening slice because
this QA branch is scoped to validating the merged scheduler/workerpool MVP:

- `internal/uiauto/ratelimit.RateLimiter.Allow` drain event emission.
- `internal/uiauto/captcha.Detector.emit` CAPTCHA event emission.
- `internal/eventbus.StreamsConsumer.Subscribe` duplicate read-loop ownership.
- `internal/uiauto/memguard.MemGuard.recordOutcome` degraded-state emission.

The preferred fix remains reuse of existing `internal/workerpool` and
`internal/lifecycle` patterns, with TDD leak/close tests before production
changes.

## Validation

Validation completed:

```text
go test ./internal/agent ./cmd/mc-api -run 'TestSchedulerDispatchEmitsWorkerpoolMetrics|TestSchedulerOptionsUsesCurrentObservabilityHooks|TestServerCloseClosesAgentScheduler' -count=1
go test -race ./internal/agent ./cmd/mc-api ./internal/workerpool ./internal/lifecycle -count=1
go test -race -p 1 -count=1 ./...
go test -covermode=atomic -coverprofile=coverage.out -p 1 -count=1 ./...
go tool cover -func=coverage.out | tail -1
go test -race -count=3 ./internal/agent ./cmd/mc-api
govulncheck ./...
cursor-tools docsync check .
runx shell-leak-scan --root . --include-docs
sentrux gate .
git diff --check
```

Results:

- Full backend race gate passed with `-p 1`.
- Coverage held at 85.0%.
- Changed-package triple-run passed under `-race`.
- `govulncheck` reported no vulnerabilities.
- Documentation drift check passed.
- Docs-inclusive shell-leak scan checked 146 files and found no findings.
- Sentrux reported no structural degradation: Quality 6041 -> 6039,
  Coupling 0.04 -> 0.04, Cycles 1 -> 1, God files 0 -> 0.
