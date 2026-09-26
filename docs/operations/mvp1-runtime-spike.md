# MVP-1 runtime spike: process set, review contract, Temporal, model bridge

Spike questions and answers, with the code and a partial infrastructure
run as evidence.
Answer shape: what MVP-1 minimally needs to run, where the review
approve/reject contract lives, whether Temporal is required, and where the
model bridge points.

## 1. Minimal process set for MVP-1

| Process | Why | Notes |
|---|---|---|
| `mc-api` | the API surface (products, orders, workflows routes) | runs WITHOUT Temporal: workflow routes answer 503 `temporal_not_configured` until it is |
| `wc-sync` | pulls the WooCommerce catalogue/orders into the local store | `make sync-run` runs one cycle locally |
| `agent-worker` | AI describe/enrich work off the queue | `make agent-run-once` runs one cycle locally |
| `temporal` (dev server) | backs the review/approve workflows | single binary, SQLite file, no k3s |
| `temporal-worker` | hosts the workflow activities (approve/reject side effects) | only needed with the above |

Supporting state: postgres, redis, minio (all in the dev compose file);
the WooCommerce fixture is the `wc-db` + `wordpress` pair (`make wc-up`).

Everything else in `cmd/` (content-worker, evomap-rollup, testing-lane,
uiauto-*, ec-cli) is outside the MVP-1 loop.

## 2. Where the review approve/reject contract lives

`mc-api` exposes the human gate over Temporal signals:

- `POST /api/v1/workflows/{id}/signals/review` → `signalProductPublishReview`
  (cmd/mc-api/workflow_handlers.go) — the review decision entry point.
- Start routes: `POST /api/v1/workflows/{media-processing|sourcing|
  marketplace-sync|marketplace-replay}`; status via
  `GET /api/v1/workflows[/{id}]`.
- The activities that apply decisions live in internal/workflow (e.g.
  `ImageEditApprovalActivities.Approve/.Reject`, vendor-notify on approval
  outcomes) and are executed by `temporal-worker`.

So the contract is: HTTP on `mc-api`, state machine in Temporal workflows,
side effects in `temporal-worker` activities. Nothing in the approve path
bypasses Temporal.

## 3. Is Temporal required?

For catalogue sync + AI enrichment alone: no (`mc-api` degrades, sync and
agent loops run standalone). For MVP-1 WITH the publish review gate: yes —
the review signal routes only exist over workflow state, and the plan's
MVP-1 acceptance includes the gate. Run it as the single-binary dev server
with a SQLite file (`temporal server start-dev --db-file <file>`); the dev
compose file already runs exactly that shape containerised. No k3s, no
server cluster, no external database.

## 4. Model bridge

`mc-api` and `temporal-worker` read `ECOMMERCE_AI_BRIDGE_URL` (fallback
`MINIMAX_BRIDGE_URL`) and route all model traffic through it. For MVP-1 the
bridge is the local LLM router (`/v1`, OpenAI-compatible, bearer via its
bootstrap env). No process talks to a model provider directly.

## Partial run (infrastructure only); application run pending

What ran, one state per process, nothing more:

| Process | State |
|---|---|
| `postgres` | verified up |
| `redis` | verified up |
| `minio` | not attempted |
| fixture `wc-db` (WooCommerce database) | verified up |
| fixture `wordpress` (storefront HTTP) | not attempted |
| `temporal` dev server | verified up (UI 200, `operator cluster health` SERVING) |
| `temporal-worker` | not attempted |
| `mc-api` | not attempted |
| `wc-sync` | not attempted |
| `agent-worker` | not attempted |

One time figure: about 15 minutes wall clock end to end - roughly 11
minutes of image pulls (first run) and roughly 4 minutes from container
start to the verified-serving probes.

The application run (fixture storefront serving, `mc-api` and
`temporal-worker` with the bridge pointed at the local router, a sync
cycle, a media-processing start and a review-signal round-trip) is the
next ticket's work; this note does not claim it.

Commands actually run (compose file: `docker-compose.dev.yml`):

```
podman-compose -f docker-compose.dev.yml \
  --profile woocommerce --profile temporal \
  up -d postgres redis wc-db wordpress temporal
podman start ec-postgres ec-redis ec-temporal ec-wc-db
podman exec ec-temporal temporal operator cluster health --address 127.0.0.1:7233
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8233/
```

Deviation worth keeping: `podman-compose` created the containers but did
not start them (`podman start` completed the bring-up), and the WordPress
service did not come up in the same invocation - it stays in the
not-attempted state above until the application run.

## Cost and rollback

The stateful pieces keep their data in container volumes: postgres, redis,
the fixture database, and the Temporal dev server's SQLite file
(`--db-filename /tmp/temporal.db` in-container). Recreating the Temporal
container loses in-flight review state; for MVP-1 that is acceptable -
pending reviews are re-creatable from the source records the gate sits on -
and the durable fix (db-file on a named volume) is a one-line compose
change held for the application run. Observed footprint at idle: four
containers, no GPU, no model calls. Without the review gate the stack
still runs: the workflow routes answer 503 `temporal_not_configured`
(the start handlers check for a configured workflow client in
`cmd/mc-api/workflow_handlers.go`), and the gate can be added later
without touching the sync or agent loops.
