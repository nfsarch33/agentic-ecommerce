# v2.0.0 Temporal Workflow Specs

The backend owns Temporal workflow contracts for long-running ecommerce and agent operations. Workflows run on the `ec-workflows` task queue through `cmd/temporal-worker`, use deterministic workflow code, and place I/O in activities. The local runbook for server and worker operations is `docs/temporal-local.md`.

## Runtime Contract

```bash
ECOMMERCE_TEMPORAL_ADDRESS=temporal:7233
ECOMMERCE_TEMPORAL_NAMESPACE=default
ECOMMERCE_TEMPORAL_TASK_QUEUE=ec-workflows
ECOMMERCE_AGENT_SCHEDULES_ENABLED=false
ECOMMERCE_AGENT_SCHEDULES_TASK_QUEUE=ec-workflows
```

Temporal server, UI, and worker profiles are local/dev release-candidate tooling by default. Do not expose Temporal gRPC or UI publicly without an approved production topology, persistence schema, TLS/mTLS boundary, and operator access path.

## Workflow Inventory

| Workflow | Start route | Main activities | Terminal states |
| --- | --- | --- | --- |
| `ProductPublishWorkflow` | `POST /api/v1/workflows/product-publish` | Check compliance, validate media, wait for human review signal, publish to WooCommerce | `completed`, `failed`, `cancelled` |
| `ContentGenerationWorkflow` | `POST /api/v1/workflows/content-generation` | Generate content through approved bridge, retrieve RAG evidence, fact-check claims, evaluate quality, approve/reject | `completed`, `failed`, `rejected` |
| `MediaProcessingWorkflow` | `POST /api/v1/workflows/media-processing` | Source supplier image, process derivatives, run QA, store object, link to product | `completed`, `failed`, `needs_review` |
| `SourcingWorkflow` | `POST /api/v1/workflows/sourcing` | Search suppliers, score candidates, compare prices, check margin rules, recommend | `completed`, `failed`, `needs_review` |
| `MarketplaceSyncWorkflow` | internal worker/API trigger | Dispatch marketplace product event to `marketplace_sync.sync`; idempotency/retry/DLQ remains inside `internal/marketplacesync` activity executor | `applied`, `duplicate`, `dlq`, `failed` |
| `MarketplaceReplayWorkflow` | internal worker/API trigger | Replay a DLQ record through `marketplace_sync.replay` | `applied`, `duplicate`, `dlq`, `failed` |
| `ImageEditApprovalWorkflow` | internal worker/API trigger | Request image edit job, wait for approval signal or update when pending, then dispatch approve/reject activity | `requested`, `pending_approval`, `approved`, `rejected`, `failed` |

All workflow start routes require bearer JWT authentication. Request bodies include the product, tenant, or agent input needed by the workflow. Responses return a workflow ID, run ID when available, status, and timestamps that the frontend can use for `/admin/workflows`.

## Status and Signals

- `GET /api/v1/workflows/{id}` returns status, workflow type, run metadata, activity timeline, and error details when available.
- `POST /api/v1/workflows/{id}/signals/review` sends the human-review decision for `ProductPublishWorkflow`.
- Pair 6 image edit approval uses `image-edit-approval` for signal-based review
  and `image-edit-approval-update` for update-based review.
- Pair 6 query handlers are `marketplace-sync-status` and
  `image-edit-approval-status`.
- Frontend workflow timelines should poll the status endpoint until a terminal state is reached.

Signals must be idempotent from the caller perspective. Repeated approval/rejection attempts should either return the accepted state or a clear conflict/error without duplicating external side effects.

## Determinism Rules

- Workflow functions must not perform direct network I/O, database I/O, filesystem I/O, random generation, goroutine management, or wall-clock reads.
- Activities own calls to PostgreSQL, WooCommerce, object stores, Redis Streams, approved AI bridges, and webhook delivery.
- Use Temporal timers, retries, and activity options rather than ad hoc sleeps or local retry loops.
- Version workflow changes when a deployed history could replay through old code.

## Release Validation

```bash
make compose-temporal-config
make temporal-up
go test ./internal/workflow/...
docker compose -f docker-compose.dev.yml --profile temporal-worker config
runx shell-leak-scan --repo ecommerce
```

`make temporal-up` is optional for pure unit tests but required before validating the local worker profile against a running dev server.
