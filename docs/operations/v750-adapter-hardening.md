# v7.5.0 Adapter Hardening MVP

**Recorded**: 2026-05-12T13:53:17+10:00  
**Pair**: v7 Pair 6 MVP  
**Branch**: `feat/v750-adapter-hardening`

## Scope

This MVP starts the v7 Pair 6 adapter-hardening slice by tightening carrier
adapter consistency around transient upstream failures. Payment adapters already
had broad typed-error table coverage, and social adapters already map key auth
and rate-limit failures. The smallest useful gap was carrier retry behavior:
AusPost and DHL both constructed the shared `internal/httpclient.Client`, but
their request paths bypassed it.

## TDD Evidence

`TestBothCarriers_QuoteRetriesTransient5xxThenSucceeds` was added before the
production change. The RED state confirmed both carriers returned after the
first transient 503 instead of retrying through the shared transport:

```text
carrier: unavailable: AusPost quote status=503
carrier: unavailable: DHL quote status=503
```

The GREEN state routes AusPost and DHL quote/label calls through
`internal/httpclient.Client` with a bounded one-retry budget and 10 ms retry
delay. Per-call hooks preserve dynamic request data:

- AusPost sets the HMAC signature from the current method, path, and payload.
- DHL resolves the access token first, then appends a per-call bearer auth hook.

The shared transport now exposes `DoWithHooks` so adapters can reuse retry,
timeout, response-hook, and circuit-breaker behavior without mutable shared
state or duplicated request loops.

## Behavior Boundary

- Transient 5xx responses get one retry before surfacing
  `ErrCarrierUnavailable`.
- Deterministic 4xx responses still surface `ErrLabelGenerationFailed`.
- Transport errors still surface `ErrCarrierUnavailable`.
- Parser and validation behavior remains unchanged.
- No live carrier calls are made; tests use `httptest.Server`.

## Validation

Completed before broad release gates:

```text
go test ./internal/adapter/carrier -run TestBothCarriers_QuoteRetriesTransient5xxThenSucceeds -count=1
go test ./internal/adapter/carrier ./internal/httpclient -count=1
go test -race ./internal/adapter/carrier ./internal/httpclient -count=1
go test ./internal/adapter/carrier -run TestBothCarriers_QuoteRetriesTransient5xxThenSucceeds -count=3
go test -race -p 1 -count=1 ./...
go test -covermode=atomic -coverprofile=coverage.out -p 1 -count=1 ./...
go tool cover -func=coverage.out
govulncheck ./...
cursor-tools docs-check --repo .
runx shell-leak-scan --root . --include-docs
sentrux gate .
git diff --check
```

Gate snapshot:

```text
full race: PASS
coverage: 85.1%
govulncheck: no vulnerabilities found
docs-check: PASS
shell-leak-scan: 149 files scanned, no findings
sentrux: Quality 6041 -> 6037, Coupling 0.04, Cycles 1, God files 0
diff check: PASS
```
