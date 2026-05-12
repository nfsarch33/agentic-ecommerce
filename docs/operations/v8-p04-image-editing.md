# EC v8 Pair 4 Product Image Editing MVP

> Date: 2026-05-12  
> Branch: `feat/v8-p04-image-editing`  
> Scope: backend MVP; provider-neutral image edit workflow and approval state model.

## Summary

Pair 4 adds a small product image editing workflow in `internal/media`.
It does not perform live OpenAI, MiniMax, VLM, OmniParser, or image-bridge
calls from the MacBook. Instead, it introduces the backend contract and
state machine that future adapters and the Pair 5 frontend review UX can
use safely.

## Implemented

- `ImageEditRequest`: provider-neutral request envelope with tenant, product,
  source URI, prompt, action, source size, preferred providers, and approval
  flags.
- `ImageEditProvider`: small provider port returning edited asset metadata.
- `ImageEditWorkflow`: in-memory workflow for MVP state transitions:
  - `requested`
  - `pending_approval`
  - `approved`
  - `rejected`
- Large asset routing: requests above `MaxLocalDecodeBytes` skip local-only
  providers and require a remote-capable provider.
- Provider fallback: configured provider order is honored; request-level
  preference can reorder providers without changing global config.
- Metrics hook: structured `ImageEditMetric` samples are emitted on provider
  success/failure without coupling `internal/media` to Prometheus.

## TDD Evidence

RED was captured before implementation:

```text
runx worktree run --repo ecommerce --branch feat/v8-p04-image-editing -- go test ./internal/media -run 'TestImageEditWorkflow' -count=1

undefined: NewImageEditWorkflow
undefined: ImageEditWorkflowConfig
undefined: ImageEditProvider
undefined: ImageEditRequest
undefined: ImageEditActionLifestyleGeneration
undefined: ErrImageEditInvalid
```

GREEN after minimal implementation:

```text
runx worktree run --repo ecommerce --branch feat/v8-p04-image-editing -- go test ./internal/media -run 'TestImageEditWorkflow' -count=1
ok github.com/nfsarch33/agentic-ecommerce/internal/media 0.861s

runx worktree run --repo ecommerce --branch feat/v8-p04-image-editing -- go test ./internal/media -count=1
ok github.com/nfsarch33/agentic-ecommerce/internal/media 0.280s
```

## Operational Boundaries

- No local heavy image generation or image understanding work.
- No live provider calls in tests.
- OpenAI, MiniMax, and fleet bridge providers remain adapter-layer concerns.
- This MVP keeps approval state durable enough for contract tests; production
  persistence belongs in a later adapter/composition sprint if required.
- Pair 4 QA owns memory ceiling tests, fallback matrix evidence, and EvoMap
  KPI output validation.

## Metrics Contract

The workflow emits `ImageEditMetric` through an optional hook:

- `action`
- `provider`
- `status`
- `duration`
- `source_bytes`
- `output_bytes`

Pair 4 QA will decide whether these fields map into existing
`ec_image_processing_total` or a new bounded-cardinality image-edit metric.

## Carry-Forwards

- Add provider adapter tests for image-bridge/OpenAI/MiniMax transport without
  live calls.
- Persist approval jobs if Pair 5 frontend UX needs cross-process state.
- Add Temporal orchestration in Pair 6 for approval workflows.
- Add EvoMap media KPI output in Pair 4 QA.
