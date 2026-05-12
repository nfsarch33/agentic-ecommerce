# EC v8 Pair 4 Product Image Editing QA Research

> Date: 2026-05-12  
> Branch: `qa/v8-p04-image-editing`  
> Scope: memory ceiling, provider fallback/preference audit, and EvoMap-ready media KPI output for the Pair 4 backend image edit workflow.

## Evidence Reviewed

- Pair 4 MVP merge: backend PR #161 at `f7b88a1`.
- MVP contract: `internal/media/image_edit.go`
  - Large requests above `MaxLocalDecodeBytes` skip local-only providers.
  - Provider execution is approval-gated.
  - Provider ordering is configurable and request preferences can reorder attempts.
  - `ImageEditMetric` is emitted through an optional hook on provider success/failure.
- Existing media OOM precedent:
  - `internal/media/product_image.go` rejects local decode over `MaxLocalDecodeBytes`.
  - `docs/operations/image-bridge.md` requires heavy image work to run on fleet bridge hosts, not this MacBook.
- Existing EvoMap/KPI pattern:
  - Monitor and workflow packages emit typed KPI samples with bounded-cardinality strings and numeric values.
  - Agentrace/EvoMap artifacts tolerate degraded hot memory as long as durable file evidence exists.

## QA Decisions

1. Add a negative memory-ceiling test.
   - Large source assets with only local providers must fail with `ErrImageEditNoProvider`.
   - Local providers must not be invoked in that path.
2. Add provider preference audit coverage.
   - Request-level preferred providers should run first, then fall back to configured providers.
   - Unknown preference names should be ignored rather than failing the request.
3. Add EvoMap-ready media KPI sample conversion.
   - Keep the runtime hook abstract.
   - Convert `ImageEditMetric` into a typed sample with bounded-cardinality string fields and numeric fields suitable for EvoMap/EvoLoop ingestion.
4. Keep QA fake-only.
   - No OpenAI, MiniMax, image bridge, VLM, or OmniParser live calls.

## RED Targets

1. `TestImageEditWorkflow_QALargeAssetWithoutRemoteProviderFailsBeforeLocalCall`
2. `TestImageEditWorkflow_QAPreferredProviderOrderFallsBackToConfiguredOrder`
3. `TestImageEditWorkflow_QAMetricsExposeEvoMapMediaKPISample`

## Acceptance

- Focused QA tests pass.
- Full media package passes under race.
- Branch gates match the Pair 4 MVP gate set.
- Evidence lands in `docs/operations/v8-p04-image-editing-qa.md`.
