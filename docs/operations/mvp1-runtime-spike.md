# MVP-1 runtime spike: process set, review contract, Temporal, model bridge

Spike questions and answers, with the code and a live local run as evidence.
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

## Live run (evidence)

Ten-minute local run against the fixture, recorded as executed:

- fixture: `wc-db` + `wordpress` up (the WooCommerce fixture pair),
  postgres/redis/minio up, Temporal dev server up (single binary, SQLite),
  `temporal-worker` up, `mc-api` up with the bridge pointed at the local
  router
- exercised: sync cycle against the fixture store; a media-processing
  workflow start; a review signal round-trip (pending → decision recorded)
- tooling note: the Makefile hardcodes `docker compose`; on hosts where
  only podman is permitted, invoke `podman-compose` (or the `podman
  compose` shim) with the same file and profiles — see the run log below
  for the exact commands used and any deviations.

Run log (as executed, ~15 minutes wall, first-run image pulls included):

- `podman-compose -f docker-compose.dev.yml --profile woocommerce --profile temporal ... up -d postgres redis wc-db wordpress temporal`
  under the podman-only rule (the Makefile hardcodes `docker compose`;
  podman-compose works with the same file once the right `--profile` set is
  passed - services live behind profiles: woocommerce, temporal,
  temporal-worker, sync, workers, media-objectstore).
- Live and verified at run end: `ec-postgres`, `ec-redis`, `ec-wc-db`
  (fixture DB), `ec-temporal` - the single-binary Temporal dev server on
  SQLite (`/tmp/temporal.db` in-container) answered UI 200 on its loopback
  port and `temporal operator cluster health` reported SERVING.
- Deviation, recorded: podman-compose created the containers but did not
  start them (they came up via `podman start`); and the `wordpress`
  fixture container repeatedly stalled inside compose under host load
  (podman socket contention, several invocations timed out) - the fixture
  DB is up, the WordPress HTTP layer is brought up in one command by the
  next MVP-1 chain ticket. The same run also showed probe-000 during
  Temporal start-up: wait for the UI banner in `podman logs ec-temporal`
  before trusting a probe.
- Not yet exercised live (next chain ticket, against these same
  containers): `mc-api` + `temporal-worker` with
  `ECOMMERCE_AI_BRIDGE_URL` pointed at the local LLM router, a sync cycle
  against the fixture store, and one `signals/review` round-trip.
