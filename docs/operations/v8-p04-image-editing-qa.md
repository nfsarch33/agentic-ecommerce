# EC v8 Pair 4 Product Image Editing QA

> Date: 2026-05-12  
> Branch: `qa/v8-p04-image-editing`  
> Scope: memory ceiling, provider fallback/preference audit, and EvoMap-ready media KPI output.

## Summary

Pair 4 QA extends the MVP image edit workflow with hard evidence around
MacBook memory safety and observability. The QA changes keep all provider
tests fake-only and do not call OpenAI, MiniMax, image-bridge, VLM, or
OmniParser locally.

## Added QA Coverage

- Large source assets above `MaxLocalDecodeBytes` fail with
  `ErrImageEditNoProvider` when no remote-capable provider is available.
- Local-only providers are not invoked for over-ceiling assets.
- Request-level preferred provider order is honored first, then falls back
  to configured provider order.
- `ImageEditMetric` now carries tenant and product IDs.
- `ImageEditMetric.MediaKPISample()` converts runtime hook samples into a
  bounded, EvoMap-ready media KPI payload.

## TDD Evidence

RED:

```text
runx worktree run --repo ecommerce --branch qa/v8-p04-image-editing -- go test ./internal/media -run 'TestImageEditWorkflow_QA' -count=1

internal/media/image_edit_qa_test.go:103:20: metrics[1].MediaKPISample undefined
```

GREEN:

```text
runx worktree run --repo ecommerce --branch qa/v8-p04-image-editing -- go test ./internal/media -run 'TestImageEditWorkflow_QA' -count=1
ok github.com/nfsarch33/agentic-ecommerce/internal/media 0.648s
```

## EvoMap Media KPI Fields

`ImageEditMediaKPISample` exposes:

- `tenant_id`
- `product_id`
- `action`
- `provider`
- `status`
- `duration_seconds`
- `source_bytes`
- `output_bytes`

These fields are intentionally bounded-cardinality and can feed EvoMap,
EvoLoop, and DRL reward analysis without adding provider-specific labels.

## Carry-Forwards

- Pair 5: surface approval/rejection in frontend review UX.
- Pair 6: move approval execution into Temporal workflow signals/queries.
- Pair 8: add media conversion KPIs to frontend content and SEO reporting.
