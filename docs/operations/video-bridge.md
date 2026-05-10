# video-bridge operator setup (deferred from v3.4.0)

> Last verified: 2026-05-11

**Owner:** ec-stack v3.4.x  
**Sprint context:** v3.4.0 EC-5-3 ships the deterministic
`StubVideoAssembler` in `internal/media/video_assembler.go` so the
content cluster + EC-4-3 channel router run today against an
in-process assembler. Live ffmpeg + Bedrock Polly + 1080x1920 /
1080x1080 outputs are deferred to a follow-up sprint behind a
fleet HTTP bridge analogous to `image-bridge` (v3.2.0) and
`omniparser-bridge` (v3.3.0).

This document captures the operator setup path so the v3.4.0 PR
ships with a working content pipeline AND a clear pickup ticket
for the next sprint.

## Why a bridge

Per `resource-guard.mdc` (memwatch ceilings) and the v322-2 OOM
post-mortem, the MacBook MUST NOT run heavy media pipelines
directly. Specifically:

- `ffmpeg -filter_complex` for 1080x1920 H.264 vertical encoding
  burns 200-300% CPU for the duration of the encode.
- Bedrock Polly synthesise calls are multi-second synchronous
  with retry storms when the throttle limit is hit.
- The temporary intermediate frames during 60-second clip
  assembly inflate the heap to ~600 MiB on a single product run.

All three classes of work belong on the WSL fleet (wsl1 / wsl2)
behind a small HTTP bridge analogous to `image-bridge`
(v3.2.0 EC-2-2 stub-with-doc) and `omniparser-bridge` (v3.3.0
EC-3-5 + v3.4.0 EC-4-1 RedNote facade).

## v3.4.0 contract (this PR)

`internal/media/video_assembler.go`:

- `VideoActionStubAssemble` -> uses the configured
  `StubVideoAssembler`. The cmd/agent-worker binary wires
  `media.NewStubVideoAssembler()` for v3.4.0; the stub passes the
  EC-5-3 RED test (`TestStubVideoAssembler_ProducesMP4WithSubtitlesAndBranding`)
  by emitting a deterministic MP4-like envelope embedding format,
  duration, subtitles, branding overlay, background music URL,
  and a SHA-256 fingerprint of the voiceover script.
- `VideoActionLiveAssemble` via `BridgeVideoAssembler` -> returns
  `media.ErrVideoBridgeUnconfigured` so callers (the EC-4-3
  channel router fan-out and the EC-5-1 video-script-driven
  scheduler in cmd/agent-worker) skip live assembly until the
  bridge is wired.

The pipeline is therefore safe to run today: every code path
either succeeds with the stub or fails loud with a typed
sentinel.

## Bridge story (next sprint pickup)

### 1. Provision the bridge service

- Repo: `video-bridge` (new, mirror `omniparser-bridge` layout).
- Host: `wsl1` (primary), `wsl2` (failover).
- Dependencies (all OSS):
  - `u2takey/ffmpeg-go` (~2.8k stars, Apache-2.0) -- Go wrapper
    around the ffmpeg CLI; declined for the v3.4.0 backend per
    R1 to keep the binary lean.
  - `aws-sdk-go-v2` -- Bedrock Polly client.
  - Optional `chromedp` for previewing assembled clips before
    publish.
- Endpoints:
  - `POST /v1/assemble` -- JSON body matching the
    `VideoAssemblyRequest` struct; response is JSON
    `{output_url, duration_sec, output_bytes_sha256}`.
  - `POST /v1/voiceover` -- internal helper for the assemble
    pipeline; returns a Polly-rendered MP3 byte URL.
- Auth: HMAC-SHA256 header (same scheme as `omniparser-bridge`
  + the v3.2.0 `image-bridge` doc).
- Backends:
  - `assemble` -> `ffmpeg` invoked via `exec.CommandContext`
    behind `u2takey/ffmpeg-go`.
  - `voiceover` -> `BedrockRuntimeClient.SynthesizeSpeech` (or
    `polly.SynthesizeSpeech` direct depending on AWS account
    mapping).

### 2. runx alias

Add to `~/.config/runx/config.yaml`:

```yaml
remote_cmds:
  video-bridge-assemble: "curl -sS --fail -X POST -H X-HMAC-Signature:... ..."
  video-bridge-voiceover: "curl -sS --fail -X POST -H X-HMAC-Signature:... ..."
```

Or (preferred) extend `runx` itself with a `video-bridge`
subcommand that handles the HMAC header inside the binary so
URL + key never appear on argv.

### 3. Ecommerce wiring

In `cmd/agent-worker` startup:

```go
asm, err := videobridge.NewBridgeAssembler(videobridge.Config{
    BridgeURL:   os.Getenv("VIDEO_BRIDGE_URL"),
    HMACSecret:  os.Getenv("VIDEO_BRIDGE_HMAC_SECRET"),
    HTTPTimeout: 120 * time.Second,
})
if err != nil {
    return fmt.Errorf("video bridge: %w", err)
}
pipeline, err := media.NewVideoAssemblyPipeline(logger, media.VideoAssemblyPipelineConfig{
    Assembler:   asm,            // <-- swap stub for bridge adapter
    TenantID:    tenant,
    KeyPrefix:   "video",
    MetricsHook: videoMetricsHook(registry),
})
```

The `videobridge.NewBridgeAssembler` adapter lives in a new
sub-package `internal/adapter/videobridge` and implements the
existing `media.VideoAssembler` port -- no change to
`internal/media/video_assembler.go` needed.

### 4. Content cluster pivot

Once the bridge is live:

- Re-enable `VideoActionLiveAssemble` in the agent-worker
  scheduler (currently blocked by the typed sentinel return from
  `media.BridgeVideoAssembler.Assemble`).
- Add a Prometheus alert on
  `ec_video_assembly_total{action="live_assemble",status="ok"}`
  rate < 1/h during business hours.
- Add a `assembler_method=stub|bridge` label to
  `ec_video_assembly_total` so the failover ratio is visible in
  Grafana.

### 5. Tests

- `internal/adapter/videobridge/assembler_test.go` -- VCR-recorded
  cassette test against the bridge.
- `internal/media/video_assembler_test.go` -- already covers the
  stub + the typed `ErrVideoBridgeUnconfigured` shape. Extend
  with a fake bridge adapter once the bridge ships.

### 6. Acceptance

- The EC-5-3 RED acceptance criterion ("output format only; no
  real ffmpeg call") is met today by the stub.
- The plan's cross-repo split note ("RedNote chromedp + ffmpeg
  both deferred to v3.7.0 EC-10 OR uiauto-framework PR") is
  recorded here.

## v3.4.0 verification

```bash
runx go --repo ecommerce -- test -race ./internal/media/...
# expected: PASS, including TestStubVideoAssembler_ProducesMP4WithSubtitlesAndBranding
```

## References

- ADR-028 EC-5-3 acceptance criterion (PR #159 in cursor-global-kb).
- `resource-guard.mdc` (memwatch ceilings; OOM lessons).
- v322-2 post-mortem (`evoloop-capsules/wsl-omniparser-oom.md`).
- `omniparser-bridge` repo layout (template for video-bridge).
- `docs/operations/image-bridge.md` (v3.2.0 EC-2-2 sister doc).
