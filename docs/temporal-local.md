# Temporal Local Runbook

v1.2.0 introduced local Temporal infrastructure for workflow development. This
runbook covers the compose-managed Temporal dev server, Temporal Web UI, and the
backend worker runtime. v1.6.0 adds schedule-control wiring without registering
concrete schedules in this infra slice.

## Services

- `temporal`: Temporal CLI dev server with SQLite persistence in the
  `ec-temporaldata` or `temporal-data` compose volume.
- `temporal` Web UI: served by the dev server on
  `http://127.0.0.1:${TEMPORAL_UI_HOST_PORT:-8233}`.
- `temporal-worker`: backend worker under the `temporal-worker` profile. It
  polls the configured task queue and registers shipped workflow/activity code.

Default local contract:

```bash
TEMPORAL_GRPC_HOST_PORT=7233
TEMPORAL_UI_HOST_PORT=8233
ECOMMERCE_TEMPORAL_ADDRESS=temporal:7233
ECOMMERCE_TEMPORAL_NAMESPACE=default
ECOMMERCE_TEMPORAL_TASK_QUEUE=ec-workflows
ECOMMERCE_AGENT_SCHEDULES_ENABLED=false
ECOMMERCE_AGENT_SCHEDULES_DEFAULT_INTERVAL=15m
ECOMMERCE_AGENT_SCHEDULES_MAX_CONCURRENT_RUNS=1
ECOMMERCE_AGENT_SCHEDULES_TASK_QUEUE=ec-workflows
```

All published ports bind through `BIND_HOST=127.0.0.1` by default. Do not expose
the dev server on a public interface; it has no production auth or TLS boundary.

## Start and Stop

Start the local Temporal server and UI:

```bash
make temporal-up
make temporal-status
```

Open the UI at:

```bash
open "http://127.0.0.1:${TEMPORAL_UI_HOST_PORT:-8233}"
```

Stop Temporal while preserving the SQLite volume:

```bash
make temporal-down
```

Remove the persisted dev history only when you intentionally want a clean local
Temporal database:

```bash
docker volume ls --filter name=ec-temporaldata
docker volume rm <compose-project>_ec-temporaldata
```

For the production-like compose file, use the same profile directly:

```bash
docker compose -f docker-compose.yml --profile temporal up -d temporal
docker compose -f docker-compose.yml --profile temporal down
```

## Worker Runtime

The worker service can be validated with compose config before it is started:

```bash
docker compose -f docker-compose.dev.yml --profile temporal-worker config
```

The worker must poll `ECOMMERCE_TEMPORAL_TASK_QUEUE=ec-workflows` and connect to
`ECOMMERCE_TEMPORAL_ADDRESS=temporal:7233` from inside compose. Host-side CLI
commands should use `127.0.0.1:${TEMPORAL_GRPC_HOST_PORT:-7233}`.

Schedule creation is intentionally not handled by this runbook. Use
`docs/agent-schedules.md` to inspect registered schedules and verify the
disabled-by-default runtime controls.

## Health and Readiness

The `temporal` compose service uses:

```bash
temporal operator cluster health --address 127.0.0.1:7233
```

Expected status is `healthy` after the dev server finishes booting. If the
container is not healthy:

```bash
docker compose -f docker-compose.dev.yml --profile temporal ps temporal
docker compose -f docker-compose.dev.yml --profile temporal logs temporal
```

Backend `/healthz` and `/readyz` do not depend on Temporal in this infra slice.
When the workflow API lands, `/readyz` should include a lightweight Temporal
client check only when Temporal is configured, matching the current optional
Postgres and Redis readiness pattern.

## Validation

Run the config gates before opening or merging an infra change:

```bash
make compose-temporal-config
make compose-config
make compose-config-prod
make compose-agent-schedules-config
make monitoring-validate
```

`agent-worker` exposes the schedule-control metrics used by the v1.6.0 alert
rules. `temporal-worker` logs its schedule-control config at startup and relies
on Temporal's own service metrics for server-side schedule execution telemetry.

## Local Workflow Testing

After workflow code lands, the expected loop is:

```bash
make temporal-up
go test ./internal/workflow/...
docker compose -f docker-compose.dev.yml --profile temporal-worker up temporal-worker
make agent-schedules-list
```

Use deterministic Temporal workflow tests for replay coverage. Avoid direct I/O,
wall-clock time, random values, or goroutines in workflow functions; put those
operations in activities and keep workflow state in Temporal history.
