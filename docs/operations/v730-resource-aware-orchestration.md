# v7.3.0 Resource-Aware Orchestration MVP

**Recorded**: 2026-05-12T11:38:20+10:00  
**Pair**: v7 Pair 4 MVP  
**Branch**: `feat/v730-resource-aware-orchestration`

## Scope

This MVP starts the v7 Pair 4 resource-aware orchestration slice by replacing
two production async paths that still relied on raw goroutine dispatch. The
goal is a small, evidence-backed change that keeps behavior stable while
tightening the OOM-prevention contract.

## Fix

- `internal/adapter/china.BatchGetProducts` now uses a fixed worker queue.
- Worker count is capped by `LoadProductionScalingConfig().PoolSize`, with
  fallback to `DefaultPoolSize` and an upper bound of `len(skus)`.
- The function still preserves per-SKU result ordering, circuit-breaker
  behavior, and context cancellation propagation.
- `internal/agent.Scheduler` now dispatches agent runs through
  `internal/workerpool.Pool` instead of raw `go s.execute(...)`.
- `Scheduler.Close(ctx)` cancels queued and running work, rejects future
  submissions with `ErrSchedulerClosed`, and drains the scheduler worker pool
  under the caller's deadline.
- Existing priority ordering, max-concurrency, wait/cancel, run-state, and
  structured event contracts remain unchanged.

## TDD Evidence

`TestBatchGetProducts_BoundsWorkerGoroutines` was added before the production
change. Against the previous semaphore-inside-goroutine implementation, the
test failed on an 80-SKU batch with:

```text
goroutine delta=81 want <=18 for bounded worker queue
```

After the fixed-worker refactor, the same test passes and confirms max
in-flight upstream calls stays within `DefaultPoolSize`.

`TestSchedulerCloseCancelsRunningAndQueuedRuns` was added before the scheduler
implementation. The RED state failed to compile because the scheduler had no
close contract and no closed-scheduler sentinel:

```text
scheduler.Close undefined
undefined: ErrSchedulerClosed
```

The GREEN implementation adds owned worker-pool dispatch and verifies close
cancels both queued and running work before rejecting new submissions.

## QA Carry-Forward

Pair 4 QA should validate the MVP under race and soak gates, then decide
whether to close or schedule these remaining detached async surfaces:

- `internal/uiauto/ratelimit.RateLimiter.Allow` drain emission currently uses a
  fire-and-forget goroutine.
- `internal/uiauto/captcha.Detector.emit` emits CAPTCHA events through a
  fire-and-forget goroutine.
- `internal/eventbus.StreamsConsumer.Subscribe` should reject duplicate
  subscriptions or track all read-loop cancellations.
- `internal/uiauto/memguard.MemGuard.recordOutcome` emits degraded-state events
  on a detached background goroutine.

The preferred direction is to reuse existing bounded worker/lifecycle patterns
instead of adding another orchestration abstraction unless the QA evidence
shows shared dispatcher semantics are needed.

## Validation

Initial MVP validation:

```text
go test ./internal/adapter/china -run TestBatchGetProducts_BoundsWorkerGoroutines -count=1
go test ./internal/adapter/china -count=1
go test -race ./internal/adapter/china -count=1
go test -race ./internal/adapter/china -run TestBatchGetProducts_BoundsWorkerGoroutines -count=3
go test ./internal/agent -run TestSchedulerCloseCancelsRunningAndQueuedRuns -count=1
go test ./internal/agent -count=1
go test -race ./internal/agent -count=1 -timeout=30s
go test -race ./internal/workerpool -count=1 -timeout=30s
go test -race ./internal/lifecycle -count=1 -timeout=30s
go test -race -p 1 -count=1 ./...
```
