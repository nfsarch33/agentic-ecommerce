# EC v8 Pair 6 Temporal Orchestration QA

> Date: 2026-05-12  
> Branch: `qa/v8-p06-temporal-orchestration`  
> Scope: replay, cancellation, retry, activity-boundary validation, and release-style backend gates for Pair 6 Temporal orchestration.

## Summary

Pair 6 QA hardened the Temporal MVP without adding live external calls.

New QA coverage:

- replay fixture for `MarketplaceSyncWorkflow`;
- cancellation coverage for `ImageEditApprovalWorkflow` while awaiting human review;
- Temporal activity retry proof for marketplace sync;
- activity-boundary validation that rejects image edit rejection calls without a reason before delegating to the editor.

## TDD Evidence

RED was captured before the fix:

```text
runx worktree run --repo ecommerce --branch qa/v8-p06-temporal-orchestration -- runx env personal-shell --exec 'go test ./internal/workflow -run "TestMarketplaceSyncWorkflowReplaysAppliedHistory|TestImageEditApprovalActivitiesRejectRequiresReason|TestImageEditApprovalWorkflowCanBeCanceled|TestMarketplaceSyncWorkflowRetries" -count=1'

--- FAIL: TestImageEditApprovalActivitiesRejectRequiresReason
    Reject err = <nil>, want ErrImageEditInvalid
--- FAIL: TestMarketplaceSyncWorkflowReplaysAppliedHistory
    replay marketplace sync workflow history: open testdata/marketplace_sync_applied_history.json: no such file or directory
```

GREEN after the minimal QA fix:

```text
runx worktree run --repo ecommerce --branch qa/v8-p06-temporal-orchestration -- runx env personal-shell --exec 'go test ./internal/workflow -run "TestMarketplaceSyncWorkflowReplaysAppliedHistory|TestImageEditApprovalActivitiesRejectRequiresReason|TestImageEditApprovalWorkflowCanBeCanceled|TestMarketplaceSyncWorkflowRetries" -count=1'

ok github.com/nfsarch33/agentic-ecommerce/internal/workflow 0.983s
```

## Validation

| Gate | Result |
| --- | --- |
| Focused workflow QA tests | PASS |
| `go test ./internal/workflow ./cmd/temporal-worker -count=1` | PASS |
| `go test -race ./internal/workflow ./cmd/temporal-worker -count=1` | PASS |
| `go test ./... -count=1` | PASS |
| `make coverage-check` | PASS, total 84.8%, gate 83% |
| `make govulncheck-scan` | PASS, no vulnerabilities |
| `make build` | PASS |
| `make compose-config-prod` | PASS |
| `make tf-fmt-check` | PASS |
| `make tf-validate` | PASS |
| `make tf-plan-contract` | PASS |

## Toolchain Notes

- `workflowcheck` remains unavailable in this local Go SDK/toolchain:
  `go run go.temporal.io/sdk/contrib/tools/workflowcheck@v1.43.0 ./internal/workflow`
  reports that the SDK module does not contain that package.
- Pair 6 QA therefore relies on committed replay history, testsuite cancellation tests, retry tests, and the existing full backend release gates.

## Operational Boundaries

- No live Temporal cluster was required.
- No live Shopify, Shopee, OpenAI, MiniMax, image-bridge, VLM, or OmniParser calls were made.
- All heavy image/VLM/UI vision work remains remote-only through approved fleet aliases.
- Real marketplace/image activity executors remain behind dependency ports until provider readiness and credential boundaries are documented.

## Carry-Forwards

- Add a first-class `runx workflowcheck` or `cursor-tools temporal workflowcheck` wrapper when a compatible Temporal checker is available.
- Add live Temporal schedule verification only in an environment with an approved local or deployment Temporal server.
- Pair 8 should feed Pair 6 workflow retry/cancellation metrics into the broader OOM/observability evidence stream.
