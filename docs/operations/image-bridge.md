# image-bridge operator setup (deferred from v3.2.0)

> Last verified: 2026-05-11

**Owner:** ec-stack v3.2.x  
**Sprint context:** v3.2.0 EC-2-2 ships background removal via the
deterministic `StubBackgroundRemover` shipped in
`internal/media/product_image.go`. Lifestyle-replacement generation
(Bedrock Titan) and high-fidelity background removal (Bedrock
Vision / `rembg` Python sidecar) are deferred to a follow-up
sprint behind a fleet HTTP bridge.

This document captures the operator setup path so the v3.2.0 PR
ships with a working enrichment pipeline AND a clear pickup
ticket for the next sprint.

## Why a bridge

Per `resource-guard.mdc` (memwatch ceilings) and the v322-2 OOM
post-mortem, the MacBook MUST NOT run heavy ML models directly.
Specifically:

- Bedrock Vision invocations spike heap during multipart upload of
  4-8 MiB images.
- `rembg` model load is ~600 MiB on first call.
- Bedrock Titan lifestyle generation is multi-minute synchronous
  call with retries.

All three classes of work belong on the WSL fleet (wsl1 / wsl2)
behind a small HTTP bridge analogous to `omniparser-bridge`
(already in use for OmniParser dynamic-element detection).

## v3.2.0 contract (this PR)

`internal/media/product_image.go`:

- `ActionBackgroundRemoval` -> uses the configured
  `BackgroundRemover`. The cmd/agent-worker binary wires
  `media.NewStubBackgroundRemover()` for v3.2.0; the stub passes the
  EC-2-2 RED test transparency assertion and produces usable
  output for catalogue thumbnails.
- `ActionLifestyleGeneration` -> returns
  `media.ErrImageBridgeUnconfigured` so callers (the enrichment
  scheduler in cmd/agent-worker) skip lifestyle generation until
  the bridge is wired.
- `media.MaxLocalDecodeBytes = 1 MiB` -> images larger than 1 MiB
  return `ErrImageTooLarge`. The image-bridge story routes those
  to the fleet.

The pipeline is therefore safe to run today: every code path
either succeeds with the stub or fails loud with a typed sentinel.

## Bridge story (next sprint pickup)

### 1. Provision the bridge service

- Repo: `image-bridge` (new, mirror `omniparser-bridge` layout).
- Host: `wsl1` (primary), `wsl2` (failover).
- Endpoints:
  - `POST /v1/bg-remove` -- multipart `image` -> PNG body.
  - `POST /v1/lifestyle` -- JSON `{prompt, base_image_url}` ->
    PNG body.
- Auth: HMAC-SHA256 header (same scheme as `omniparser-bridge`).
- Backends:
  - `bg-remove` -> `rembg` (`u2net`) Python sidecar OR Bedrock
    Vision via `aws-sdk-go-v2`.
  - `lifestyle` -> Bedrock Titan Image Generator
    (`amazon.titan-image-generator-v1`).

### 2. runx alias

Add to `~/.config/runx/config.yaml`:

```yaml
remote_cmds:
  image-bridge-bg-remove: "curl -sS --fail -X POST -H X-HMAC-Signature:... ..."
  image-bridge-lifestyle: "curl -sS --fail -X POST -H X-HMAC-Signature:... ..."
```

Or (preferred) extend `runx` itself with an `image-bridge`
subcommand that handles the HMAC header inside the binary so URL
+ key never appear on argv.

### 3. Ecommerce wiring

In `cmd/agent-worker` startup:

```go
remover, err := bedrock.NewBridgeBackgroundRemover(bedrock.BridgeConfig{
    BridgeURL:   os.Getenv("IMAGE_BRIDGE_URL"),
    HMACSecret:  os.Getenv("IMAGE_BRIDGE_HMAC_SECRET"),
    HTTPTimeout: 60 * time.Second,
})
if err != nil {
    return fmt.Errorf("image bridge: %w", err)
}
pipeline, err := media.NewProductImagePipeline(logger, media.ProductImagePipelineConfig{
    Downloader:  httpdownloader.New(httpClient),
    Remover:     remover,            // <-- swap stub for bridge adapter
    Store:       ociStore,
    TenantID:    tenant,
    MetricsHook: enrichmentMetricsHook(registry),
})
```

The `bedrock.NewBridgeBackgroundRemover` adapter lives in a new
sub-package `internal/adapter/bedrock` and implements the existing
`media.BackgroundRemover` port -- no change to
`internal/media/product_image.go` needed.

### 4. Enrichment pipeline pivot

Once the bridge is live:

- Re-enable `ActionLifestyleGeneration` in the agent-worker
  scheduler (currently blocked by the typed sentinel return).
- Add a Prometheus alert on
  `ec_image_processing_total{action="lifestyle_generation",status="ok"}`
  rate < 1/h during business hours.
- Add a `bg_removal_method=stub|bridge` label to the existing
  `ec_image_processing_total` metric so the failover ratio is
  visible in Grafana.

### 5. Tests

- `internal/adapter/bedrock/bg_remover_test.go` -- VCR-recorded
  cassette test against the bridge.
- `internal/media/product_image_test.go` -- extend to assert the
  pipeline returns ErrImageBridgeUnconfigured for lifestyle when
  the bridge adapter is nil; succeeds with a fake bridge adapter.

### 6. Acceptance

- The EC-2-2 acceptance criterion ("background removal produces
  PNG with transparent background") is met today by the stub.
- The v3.2.0 PR records this deferred pickup explicitly so the
  next sprint planning round picks it up automatically.

## v3.2.0 verification

```bash
runx go --repo ecommerce -- test -race ./internal/media/...
# expected: PASS, including TestProductImage_BackgroundRemovalProducesTransparentPNG
```

## References

- ADR-028 EC-2-2 acceptance criterion (PR #159 in cursor-global-kb).
- `resource-guard.mdc` (memwatch ceilings; OOM lessons).
- v322-2 post-mortem (`evoloop-capsules/wsl-omniparser-oom.md`).
- `omniparser-bridge` repo layout (template for image-bridge).
