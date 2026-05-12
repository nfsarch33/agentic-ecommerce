# EC v8 Pair 6 Temporal Orchestration Research

> Date: 2026-05-12  
> Branch: `feat/v8-p06-temporal-orchestration`  
> Scope: Temporal workflows for marketplace sync and image edit approval, with deterministic workflow code, activity-contained I/O, replayable tests, signals, queries, updates, and worker registration.

## Sources Reviewed

- Temporal Go SDK developer guide: `https://docs.temporal.io/develop/go`
- Temporal Go SDK testing guide: `https://docs.temporal.io/develop/go/best-practices/testing-suite`
- Go SDK API docs: `https://pkg.go.dev/go.temporal.io/sdk/workflow`
- Go SDK testsuite API docs: `https://pkg.go.dev/go.temporal.io/sdk/testsuite`
- Installed `temporal-developer` skill:
  - `references/core/determinism.md`
  - `references/go/go.md`
  - `references/go/patterns.md`
  - `references/go/gotchas.md`
  - `references/go/testing.md`
- Existing EC workflow code:
  - `internal/workflow/product_publish.go`
  - `internal/workflow/media_processing.go`
  - `internal/workflow/gmv_daily_refresh.go`
  - `internal/workflow/worker_e2e_test.go`
  - `cmd/temporal-worker/main.go`
- Existing v8 domains:
  - `internal/marketplacesync`
  - `internal/media/image_edit.go`
  - Pair 1-5 operation docs under `docs/operations/`

## Current Code Facts

- The repo already uses `go.temporal.io/sdk v1.43.0` and `go.temporal.io/api v1.62.11`.
- Existing workflows use named activity constants, `workflow.SetQueryHandler`,
  `workflow.GetSignalChannel`, `workflow.NewSelector`, and `testsuite.WorkflowTestSuite`.
- Existing activity implementation pattern uses struct methods with dependency
  ports, then registers named activities in `cmd/temporal-worker`.
- `internal/marketplacesync.Engine` owns idempotency, retry-to-DLQ, replay, and
  bounded metrics for marketplace product events.
- `internal/media.ImageEditWorkflow` owns provider selection, approval states,
  large-asset routing, provider fallback, and media KPI samples.

## Decisions

1. Keep workflow code deterministic.
   - No native goroutines, native channels, wall-clock calls, random values,
     file I/O, network I/O, or provider execution in workflow functions.
   - Use Temporal query/update/signal APIs only for workflow state interaction.
2. Keep marketplace sync I/O in activities.
   - `MarketplaceSyncWorkflow` dispatches `marketplace_sync.sync`.
   - `MarketplaceReplayWorkflow` dispatches `marketplace_sync.replay`.
   - Activity implementations wrap an executor interface whose production side
     can call the existing `marketplacesync.Engine`.
3. Keep image edit provider execution in activities.
   - `ImageEditApprovalWorkflow` dispatches `image_edit.request`.
   - It waits for approval/rejection by signal or update when the job is
     `pending_approval`.
   - It dispatches `image_edit.approve` or `image_edit.reject`; the activity
     wraps the existing `media.ImageEditWorkflow`/service port.
4. Add query and update surfaces in MVP, not only docs.
   - Marketplace workflow exposes status query.
   - Image approval workflow exposes status query and approval update handler.
   - Image approval also supports signal because existing EC workflow handlers
     already use signal-based review.
5. Worker registration is part of the MVP.
   - New workflows and named activities must be registered by
     `cmd/temporal-worker`.
   - Default activity wiring may be safe/no-op when providers are not configured,
     but registration must be present so deployments do not fail on missing
     workflow/activity names.

## RED Targets

1. `TestMarketplaceSyncWorkflow_DispatchesSyncActivityAndReportsStatus`
   - Fails until `MarketplaceSyncWorkflow`, input/result types, query constant,
     and named sync activity exist.
2. `TestMarketplaceReplayWorkflow_DispatchesReplayActivity`
   - Fails until replay orchestration exists.
3. `TestImageEditApprovalWorkflow_WaitsForApprovalSignalBeforeApproveActivity`
   - Fails until image approval workflow waits for a signal and then calls the
     approve activity.
4. `TestImageEditApprovalWorkflow_AcceptsApprovalUpdate`
   - Fails until the update handler mutates workflow state and unblocks approval.
5. `TestImageEditApprovalWorkflow_ReportsPendingViaQuery`
   - Fails until query state is registered before waiting.
6. `cmd/temporal-worker` registration test fails until new workflows and
   activities are registered.

## Carry-Forward to QA

- Add replay-history tests and worker shutdown/cancellation matrix.
- Add schedule verification or schedule-plan tests if Pair 6 MVP does not wire
  reconciliation schedules.
- Run workflowcheck if available through runx/cursor-tools; otherwise document
  the missing wrapper.
- Record all Temporal runtime metrics and Agenttrace evidence in the QA doc.
