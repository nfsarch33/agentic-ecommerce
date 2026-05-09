# Operations runbook (v2.10.1+)

This runbook documents the response procedures for the failure
modes that the v2.10.x resilience pillar can detect and report on.
It is the canonical operator-facing companion to
[`docs/observability.md`](../observability.md) and
[ADR-027](../adr/adr-027-resilience-pillar.md).

For the bridge-specific deploy contract see
[`docs/operations/omniparser-bridge.md`](omniparser-bridge.md).

## OOM alarm response

1. Pager fires on `increase(ec_oom_alarms_total[5m]) > 0`.
2. Open the `ec-resilience` Grafana dashboard. The "OOM alarms by
   binary" panel identifies the breaching binary; the "Heap-in-use
   bytes" panel shows the dwell window.
3. Cross-check `kubectl logs <pod>` (or `docker logs <container>`)
   filtered for `memwatch.heap_ceiling_critical`. The slog
   structure carries `binary`, `heap_in_use_bytes`, `ceiling_bytes`,
   and `dwell_ms`.
4. If the alarm is sustained, `lifecycle.Manager.Shutdown()` will
   trigger a graceful exit; the orchestrator restarts the pod and
   the dashboards return to baseline within 5 minutes. If they do
   NOT return to baseline, escalate to the on-call engineer with
   the breaching binary and the most recent slog payload.
5. Inspect `ec_workerpool_queued` + `ec_workerpool_saturation_total`
   for the same window to identify the saturating workload (which
   pool was buffering work).
6. If a single tenant is responsible, raise their per-tenant memcap
   override (see middleware.MemCap.TenantOverride) or rate-limit
   their slot via the v2.5 quota Enforcer.

## Goroutine-leak alarm response

1. Pager fires on `ec_goroutine_count > GOROUTINE_CEILING for 60s`
   in any binary.
2. The runtime dump file
   `~/.cache/ec/goroutine-dump-<unix-ts>.txt` is written when the
   alarm fires. Pull it from the affected pod via `kubectl cp`.
3. The top stack frames identify the leaked goroutine type. Common
   causes:
   - Event-bus consumers leaking on cancelled context (look for
     `internal/eventbus/streams.go` in the stack).
   - Temporal activity callbacks not honouring `ctx.Done()`.
   - Marketplace plugin event delivery leaking on a hung subscriber.
4. The fix is always "honour ctx.Done() in the goroutine and
   return". The chaos suite's
   `TestGoroutineCeilingTriggersAlarm` is a regression gate for
   the alarm path itself.

## Postgres flap response

The chaos suite (`tests/chaos/postgres_flap_test.go`) proves the
mc-api pool recovers within 5 s of a Postgres restart. In
production:

1. Pager fires on `up{job="postgres"} == 0`.
2. mc-api `/healthz` returns 503 with `Retry-After: 5`. The HTTP
   middleware emits 503 because the `pgxpool.Ping` probe is part of
   the readiness check; the worker pool does not ack new tasks.
3. Once Postgres recovers, the pool rebuilds connections lazily.
   Verify recovery by hitting `/healthz` or looking at
   `ec_workflow_runs_total{status="success"}` resuming on the
   `ec-overview` dashboard.
4. If Postgres does not recover within the orchestrator's grace
   window, the `lifecycle.Manager` graceful drain still runs
   cleanly because every Closer honours its context deadline.

## Redis flap response

`tests/chaos/redis_flap_test.go` mirrors the Postgres flap with the
same 5 s recovery budget.

1. Pager fires on `up{job="redis"} == 0`.
2. The rate limiter falls open (per-tenant token bucket continues
   to drain locally; refills are paused). The event bus fans out
   to a fallback in-memory queue that is drained on Redis recovery.
3. Verify recovery via `ec_workerpool_saturation_total{pool="eventbus"}`
   returning to zero.

## Temporal flap response

`tests/chaos/temporal_flap_test.go` proves the frontend port
recovers within 5 s. In production:

1. Pager fires on the Temporal frontend `7233/tcp` becoming
   unreachable.
2. mc-api `POST /workflows/start` returns 503 with `Retry-After`.
3. Long-running workflows are unaffected -- they are persisted in
   Temporal's history; the worker reconnects automatically once
   the frontend returns.
4. Verify recovery via `ec_workflow_runs_total{workflow="..."}` for
   any workflow that the upstream callers retry.

## Bridge offload response (`omniparser-bridge`)

The bridge is configured to fail loud on signature mismatch
(401 Unauthorized) and upstream timeout (504 Gateway Timeout).

- 401 spike: rotate the `OMNIPARSER_BRIDGE_SECRET` on both ends
  via the deploy pipeline; signed envelopes mid-flight return as
  `signature mismatch`.
- 504 spike: check `OMNIPARSER_LOCAL_ENDPOINT` reachability from
  the node-a host. The OmniParser worker may have OOM'd; restart it
  and `ec_oom_alarms_total{binary="omniparser-bridge"}` will
  return to zero.

## Toolchain rebuild response

When `go.mod` requires a Go version newer than the host runtime,
all repos use `runx make build/install --repo <alias>` which sets
`GOTOOLCHAIN=auto`. v2.10.1 captured the rebuild evidence in
`tests/benchmarks/v2.10-toolchain.json`.

If `go test ./...` errors with
`go.mod requires go >= 1.25.10 (running go 1.24.11; GOTOOLCHAIN=local)`,
either set `GOTOOLCHAIN=auto GOSUMDB=sum.golang.org` in the calling
shell or route through `runx make test --repo ecommerce`.

## Hook bypass discipline

Personal-repo pushes that need to bypass the
`pre-push.allowMainPush=false` gate (e.g. fresh-repo direct push
to main) follow this pattern:

```bash
# 1. Note window start (UTC)
date -u +%Y-%m-%dT%H:%M:%SZ

# 2. Set the bypass for THIS repo only.
git config hooks.allowMainPush true

# 3. Push under the personal-shell so token env stays scrubbed.
runx env personal-shell --exec 'git push -u origin main'

# 4. Restore the bypass to its default.
git config --unset hooks.allowMainPush

# 5. Note window end (UTC).
date -u +%Y-%m-%dT%H:%M:%SZ
```

Each window is logged in the PR description so the audit trail is
preserved. The v2.10.1 omniparser-bridge initial-commit window
ran 2026-05-09T05:17:50Z -> 2026-05-09T05:17:54Z.
