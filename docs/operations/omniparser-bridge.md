# omniparser-bridge -- ecommerce stack integration

> Last verified: 2026-05-11

The OmniParser visual UI parser is too heavy to run on a MacBook
(needs a GPU + Python environment). The fleet host `gpu-host-1` runs the
real OmniParser worker; MacBook agents reach it through the signed
`omniparser-bridge` HTTP service so the fleet endpoint never lands
on argv.

This document captures the deploy contract on the **caller side**
(this repo) -- the bridge implementation lives at
[`nfsarch33/omniparser-bridge`](https://github.com/nfsarch33/omniparser-bridge).

## How it composes with `cmd/uiauto-compare`

`cmd/uiauto-compare` does NOT itself call OmniParser. It compares
deterministic Playwright fixtures against deterministic
uiauto-framework fixtures. The OmniParser dependency lives in the
adjacent repo `uiauto-framework`, which generates the
deterministic fixtures by calling OmniParser through the bridge.

For local development that exercises the OmniParser path:

1. Run the wiremock stub from
   `docker-compose.dev.yml::uiauto-omniparser-stub`. This does NOT
   require the bridge -- it serves a deterministic /probe and /parse
   contract for hermetic CI.
2. Run the real bridge via `runx tunnel daemon omniparser-bridge`
   (after adding the forward to `~/.config/runx/config.yaml` per
   the snippet below). The bridge URL resolves to
   `127.0.0.1:18090` on the MacBook.
3. Set `OMNIPARSER_BRIDGE_URL=http://127.0.0.1:18090` in the
   uiauto-framework env. The framework signs every outbound request
   with `OMNIPARSER_BRIDGE_SECRET` (must match the gpu-host-1-side env).

## runx forward snippet

Add the following block to `~/.config/runx/config.yaml` so MacBook
callers see the bridge as a localhost forward only:

```yaml
forwards:
  omniparser-bridge:
    target: gpu-host-1
    remote: 127.0.0.1:8090
    local:  127.0.0.1:18090
    reconnect:
      initial_backoff: 1s
      max_backoff:     60s
      jitter:          true
```

The gpu-host-1-side bridge listens on `127.0.0.1:8090`; the runx tunnel
maps it to `127.0.0.1:18090` on the MacBook.

## Env-var reference (caller side)

| Variable | Required | Purpose |
| --- | --- | --- |
| `OMNIPARSER_BRIDGE_URL` | yes | Local-side forwarded URL (e.g. `http://127.0.0.1:18090`). |
| `OMNIPARSER_BRIDGE_SECRET` | yes | 32-byte HMAC key shared with the gpu-host-1 bridge. |

The caller signs every POST with the same canonical preimage the
bridge verifies:

```text
"<unix-secs>\n<path-and-args>\n<body-bytes>"
```

The signing helper is identical in shape to the bridge's
`internal/signing` package:

```go
issued := time.Now().UTC().Truncate(time.Second)
mac := hmac.New(sha256.New, []byte(os.Getenv("OMNIPARSER_BRIDGE_SECRET")))
mac.Write([]byte(strconv.FormatInt(issued.Unix(), 10)))
mac.Write([]byte{'\n'})
mac.Write([]byte("/v1/parse"))
mac.Write([]byte{'\n'})
mac.Write(body)
sig := hex.EncodeToString(mac.Sum(nil))

req.Header.Set("X-OmniBridge-Issued-At", strconv.FormatInt(issued.Unix(), 10))
req.Header.Set("X-OmniBridge-Signature", sig)
```

## Security notes

- The bridge enforces a 60 s default replay window. Local clock
  skew greater than 60 s rejects every signed request -- run NTP
  on both ends.
- The bridge enforces an 8 MiB inbound body cap. OmniParser
  payloads are small (base64 PNGs); raise the cap on both sides if
  a future use case needs larger blobs.
- The bridge runs as `nonroot:nonroot` in a distroless static image.
  The gpu-host-1 systemd unit should set `NoNewPrivileges=true`,
  `ProtectSystem=strict`, and `ProtectHome=true`.

## Verifying the integration

A signed round-trip with no upstream listening returns 502 with
`bridge.forward_failed` slog evidence -- this still proves the
sig-verify -> forward chain. The full smoke step is captured in
the bridge repo at `docs/smoke-test.md`.
