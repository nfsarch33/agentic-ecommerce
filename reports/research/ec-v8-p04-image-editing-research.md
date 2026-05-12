# EC v8 Pair 4 Product Image Editing Research

> Date: 2026-05-12  
> Branch: `feat/v8-p04-image-editing`  
> Scope: backend MVP only; provider-neutral image edit contract, approval state model, remote-routing guardrails, and operator evidence. No live provider calls in this sprint.

## Evidence Reviewed

- Existing EC media pipeline: `internal/media/product_image.go`
  - Already owns local deterministic background removal.
  - Enforces `MaxLocalDecodeBytes = 1 MiB`.
  - Returns typed `ErrImageBridgeUnconfigured` for deferred lifestyle generation.
  - Records `ec_image_processing_total` and duration metrics through an abstract hook.
- Existing bridge precedent: `internal/media/video_assembler.go` and `docs/operations/video-bridge.md`
  - Uses typed sentinel errors for deferred heavy work.
  - Keeps HMAC/live bridge details outside local unit tests.
  - Avoids raw goroutines and implements `lifecycle.Closer`.
- Existing provider safety precedent: `internal/adapter/minimax/client.go`
  - Requires bridge URL.
  - Rejects direct MiniMax URLs.
  - Bounds HTTP response body reads.
  - Keeps credentials out of argv/test fixtures.
- OpenAI Images API references:
  - OpenAI Images API supports creating image edits from one or more source images plus a prompt.
  - GPT Image models support PNG, WEBP, and JPG source images under 50 MB; multiple source images are supported.
  - Masks are optional and require transparency semantics.
  - JSON image edit requests can reference `image_url` or `file_id`; multipart requests use uploaded binary fields.

## Decisions

1. Add the Pair 4 MVP contract in `internal/media`, adjacent to the existing product image pipeline.
   - The domain behavior is product media workflow state, not a provider transport detail.
   - Provider transports can be added later under `internal/adapter/*` without changing callers.
2. Introduce a small `ImageEditProvider` port.
   - Providers expose stable names and capabilities.
   - The media workflow chooses providers by configured order and request preference.
   - Large assets must skip local-only providers and route to a remote-capable provider.
3. Model approval explicitly.
   - A request can be submitted into `pending_approval` without invoking any provider.
   - Approval executes the provider path.
   - Rejection closes the job without a provider call.
4. Keep tests fake-only.
   - No OpenAI, MiniMax, image-bridge, VLM, or OmniParser live call runs on this MacBook.
   - Provider fakes verify routing, fallback, and state transitions.
5. Keep metrics hook abstract.
   - The MVP emits structured metric samples without coupling `internal/media` to Prometheus.
   - Pair 4 QA will add memory ceiling tests, fallback audit evidence, and EvoMap KPI output.

## TDD RED Targets

1. Invalid provider-neutral requests are rejected with a typed validation error.
2. Large assets skip local providers and select a remote provider.
3. Approval state transitions prevent provider execution before approval and allow approve/reject decisions.
4. Provider fallback honors configured order when the first provider fails.

## Non-Goals

- No new product HTTP endpoints.
- No live OpenAI/MiniMax/bridge adapter implementation.
- No local image generation, VLM, or OmniParser workload.
- No frontend UX change; Pair 5 owns frontend media review UI.

## Carry-Forwards

- Pair 4 QA: memory ceiling tests, provider fallback matrix, EvoMap media KPI artifact, and image-bridge docs update.
- Pair 5: frontend review/approval UX and uiauto/Playwright comparison.
- Pair 6: Temporal workflow orchestration for marketplace sync and image approval.
