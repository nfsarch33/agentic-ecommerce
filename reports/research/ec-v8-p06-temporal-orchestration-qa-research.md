# EC v8 Pair 6 Temporal Orchestration QA Research

> Date: 2026-05-12  
> Branch: `qa/v8-p06-temporal-orchestration`  
> Scope: replay, cancellation, retry, worker registration, and activity-boundary QA for the v8 Pair 6 Temporal MVP.

## Sources Reviewed

- Installed `temporal-developer` skill:
  - `references/go/testing.md`
  - `references/go/patterns.md`
  - `references/go/gotchas.md`
- Existing EC Temporal replay tests:
  - `internal/workflow/sourcing_test.go`
  - `internal/workflow/product_publish_test.go`
  - `internal/workflow/testdata/sourcing_margin_failure_history.json`
  - `internal/workflow/testdata/product_publish_failure_history.json`
- Pair 6 MVP implementation:
  - `internal/workflow/marketplace_sync.go`
  - `internal/workflow/image_edit_approval.go`
  - `cmd/temporal-worker/main.go`
  - `docs/operations/v8-p06-temporal-orchestration.md`

## Current Facts

- EC uses `go.temporal.io/sdk v1.43.0`.
- The repo already has history replay tests through `worker.NewWorkflowReplayer`.
- The local Temporal CLI is installed, but `workflowcheck` is not installed.
- `go run go.temporal.io/sdk/contrib/tools/workflowcheck@v1.43.0` reports that the SDK module does not contain that package.
- Pair 6 MVP workflow code uses Temporal APIs only: `workflow.ExecuteActivity`, `workflow.SetQueryHandler`, `workflow.SetUpdateHandler`, `workflow.GetSignalChannel`, `workflow.Go`, and `workflow.Await`.
- Provider and marketplace I/O remains in activities behind dependency ports.

## QA Decisions

1. Add replay coverage with committed history JSON.
   - Use the existing `worker.NewWorkflowReplayer` pattern.
   - Start with marketplace sync because it has a single activity path and gives a compact deterministic history fixture.
2. Add cancellation coverage for image approval.
   - Approval workflows can wait indefinitely for human review; cancellation must return a clear Temporal cancellation error while waiting.
3. Add activity-boundary validation.
   - Activity methods are callable independently from workflows, so they must validate dangerous edge cases even if the workflow also validates them.
   - Rejection without a reason should be rejected before delegating to an editor implementation.
4. Keep QA free of live calls.
   - No live Temporal cluster, Shopify, Shopee, OpenAI, MiniMax, image bridge, VLM, or OmniParser calls.
   - Use testsuite, replay fixtures, and in-process fake dependencies only.

## RED Targets

1. `TestMarketplaceSyncWorkflowReplaysAppliedHistory`
   - Fails until a new replay fixture exists and the workflow is registered in the replayer test.
2. `TestImageEditApprovalWorkflowCanBeCanceledWhileAwaitingDecision`
   - Proves cancellation remains safe while the workflow is blocked on human review.
3. `TestImageEditApprovalActivitiesRejectRequiresReason`
   - Fails until `ImageEditApprovalActivities.Reject` rejects empty reasons before delegating to the editor.
4. `TestMarketplaceSyncWorkflowRetriesSyncActivityBeforeDLQ`
   - Proves the Temporal activity retry policy retries transient activity failures before returning the DLQ state.

## Expected Evidence

- Focused workflow QA test pass.
- Targeted race pass for `internal/workflow` and `cmd/temporal-worker`.
- Full backend test pass.
- Coverage, govulncheck, build, docs, shell-leak, Terraform/Compose gates.
- Branch-local Sentrux gate.
- Agenttrace event recording.

## Carry-Forwards

- Add a first-class `runx workflowcheck` or `cursor-tools temporal workflowcheck` wrapper if/when the toolchain exposes a compatible checker.
- Live schedule verification remains local-cluster or deployment-environment scoped; this QA branch stays credential-free.
