# Temporal Local Runbook

v1.2.0 introduces local Temporal infrastructure for workflow development. This
runbook covers the compose-managed Temporal dev server, Temporal Web UI, and the
placeholder worker contract. It does not define workflow or activity code; the
backend Temporal agent owns `cmd/temporal-worker` and workflow APIs.

## Services

- `temporal`: Temporal CLI dev server with SQLite persistence in the
  `ec-temporaldata` or `temporal-data` compose volume.
- `temporal` Web UI: served by the dev server on
  `http://127.0.0.1:${TEMPORAL_UI_HOST_PORT:-8233}`.
- `temporal-worker`: image-only compose placeholder under the
  `temporal-worker` profile. It is intentionally not built from this branch
  because `cmd/temporal-worker` has not landed.

Default local contract:

```bash
TEMPORAL_GRPC_HOST_PORT=7233
TEMPORAL_UI_HOST_PORT=8233
ECOMMERCE_TEMPORAL_ADDRESS=temporal:7233
ECOMMERCE_TEMPORAL_NAMESPACE=default
ECOMMERCE_TEMPORAL_TASK_QUEUE=ec-workflows
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

## Worker Handoff

The worker service is present for configuration compatibility only:

```bash
docker compose -f docker-compose.dev.yml --profile temporal-worker config
```

Once `cmd/temporal-worker` lands, replace the image-only service with the same
Dockerfile pattern used by `mc-api`, `wc-sync`, and `agent-worker`:

```yaml
build:
  context: .
  dockerfile: Dockerfile
  args:
    VERSION: ${VERSION:-dev}
    COMMIT: ${COMMIT:-unknown}
    TARGET: temporal-worker
```

The worker must poll `ECOMMERCE_TEMPORAL_TASK_QUEUE=ec-workflows` and connect to
`ECOMMERCE_TEMPORAL_ADDRESS=temporal:7233` from inside compose. Host-side CLI
commands should use `127.0.0.1:${TEMPORAL_GRPC_HOST_PORT:-7233}`.

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
make monitoring-validate
```

There is no Prometheus scrape target for `temporal-worker` yet because the worker
binary and metrics endpoint are not merged. Add the scrape job after the worker
exports `/metrics`, using the existing `agent-worker` job as the local pattern.

## Local Workflow Testing

After workflow code lands, the expected loop is:

```bash
make temporal-up
go test ./internal/workflow/...
docker compose -f docker-compose.dev.yml --profile temporal-worker up temporal-worker
```

Use deterministic Temporal workflow tests for replay coverage. Avoid direct I/O,
wall-clock time, random values, or goroutines in workflow functions; put those
operations in activities and keep workflow state in Temporal history.
