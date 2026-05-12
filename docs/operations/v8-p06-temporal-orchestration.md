# EC v8 Pair 6 Temporal Orchestration MVP

> Date: 2026-05-12  
> Branch: `feat/v8-p06-temporal-orchestration`  
> Scope: backend MVP; Temporal workflow wrappers for marketplace sync and product image edit approval.

## Summary

Pair 6 moves the v8 marketplace sync and image edit approval contracts behind
Temporal workflow surfaces without adding live external provider calls.

The MVP keeps workflow code deterministic and small:

- marketplace sync and replay are activity-backed workflows over the existing
  `internal/marketplacesync` engine contract.
- image edit approval is an activity-backed workflow over the existing
  `internal/media` image edit workflow contract.
- queries expose current workflow state.
- image edit approval accepts either a signal or an update.
- all I/O remains activity-side.

## Implemented

- `MarketplaceSyncWorkflow`
- `MarketplaceReplayWorkflow`
- `ImageEditApprovalWorkflow`
- `MarketplaceSyncActivities`
- `ImageEditApprovalActivities`
- temporal-worker registration for the new workflows and activities.

Named activity constants:

- `marketplace_sync.sync`
- `marketplace_sync.replay`
- `image_edit.request`
- `image_edit.approve`
- `image_edit.reject`

Interaction constants:

- `marketplace-sync-status`
- `image-edit-approval`
- `image-edit-approval-update`
- `image-edit-approval-status`

## TDD Evidence

RED was captured before implementation:

```text
runx worktree run --repo ecommerce --branch feat/v8-p06-temporal-orchestration -- go test ./internal/workflow -run 'TestMarketplace|TestImageEditApproval' -count=1

undefined: ImageEditRequestActivity
undefined: ImageEditApprovalWorkflow
undefined: MarketplaceSyncInput
```

Worker registration RED:

```text
runx worktree run --repo ecommerce --branch feat/v8-p06-temporal-orchestration -- go test ./cmd/temporal-worker -run TestRegisterWorkflowsAndActivities -count=1

workflows registered = 6, want 9
```

GREEN after minimal implementation:

```text
runx worktree run --repo ecommerce --branch feat/v8-p06-temporal-orchestration -- go test ./internal/workflow -run 'TestMarketplace|TestImageEditApproval' -count=1
ok github.com/nfsarch33/agentic-ecommerce/internal/workflow 1.555s

runx worktree run --repo ecommerce --branch feat/v8-p06-temporal-orchestration -- go test ./internal/workflow -count=1
ok github.com/nfsarch33/agentic-ecommerce/internal/workflow 0.513s

runx worktree run --repo ecommerce --branch feat/v8-p06-temporal-orchestration -- go test ./cmd/temporal-worker -count=1
ok github.com/nfsarch33/agentic-ecommerce/cmd/temporal-worker 1.300s
```

## Operational Boundaries

- No live Shopify, Shopee, OpenAI, MiniMax, image-bridge, VLM, or OmniParser
  calls are introduced by this MVP.
- Production marketplace/image provider wiring remains behind activity
  dependency ports.
- Workflow code must stay deterministic: no direct I/O, random generation,
  native goroutines/channels/selects, or wall-clock reads.
- Image approval uses `workflow.Go` plus `workflow.Await` so either the signal
  or update path can unblock the workflow.

## Carry-Forwards

- Pair 6 QA must add replay-history tests, cancellation tests, and worker
  shutdown evidence.
- Add schedule-plan or schedule registration coverage for marketplace
  reconciliation if required by the final v8 release checklist.
- Add a runx/cursor-tools wrapper for `workflowcheck` if it is not already
  available in the deployed toolchain.
- Wire real marketplace/image activity executors only after provider readiness
  and credential boundaries are documented.
