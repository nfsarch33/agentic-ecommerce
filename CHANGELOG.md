# Changelog

All notable changes to the Agentic Ecommerce backend are documented here.

## Unreleased (v2.3.0 MVP)

### Added — Digital goods bounded context

- New domain package `internal/domain/digital/` with the
  `DigitalProduct`, `License`, `DownloadToken`, and `AccessGrant`
  aggregates plus an explicit licence state machine (`active` →
  `revoked` | `expired`) driven by a transition table. Every legal
  `(state, transition) → state` triple is exhaustively asserted by
  table-driven tests; every illegal pair returns the typed
  `digital.ErrInvalidTransition`. Mirrors the v2.2.0 membership
  pattern in `internal/domain/membership/state.go`.
- HMAC-SHA256 licence keys (`internal/domain/digital/license_key.go`)
  built from stdlib `crypto/hmac` + `crypto/sha256`, with constant-
  time verification via `crypto/subtle.ConstantTimeCompare`. The
  generator rejects secrets shorter than 32 bytes and folds the
  tenant id into the HMAC input so a leaked key cannot be replayed
  across tenants.
- New ports `port.DigitalProductRepository`, `port.LicenseRepository`,
  `port.AccessGrantRepository`, `port.DownloadTokenIssuer`, and
  `port.DigitalAccessGrantor`, with compile-time assertions added to
  `internal/port/contract_test.go`.
- New adapters:
  - `internal/adapter/postgres/digital_repo.go` — tenant-keyed CRUD
    plus optimistic `SaveState` for licences. Backed by tables
    introduced in migration `0008_digital.sql`.
  - `internal/adapter/inmemory/digital_repo.go` — drop-in store used
    by tests, the dev compose stack, and the Playwright E2E mock.
  - `internal/adapter/signedurl/issuer.go` — HMAC-SHA256 signed URL
    builder/parser with explicit unit-separator framing
    (`tenant 0x1f licence 0x1f product 0x1f exp 0x1f uses`) and a
    base64url-no-padding signature so URLs survive query-string
    round-trips. Verification rejects tampered signatures, missing
    `sig`, expired tokens, and cross-tenant replay.
- New service layer at `internal/digital/service.go` orchestrates
  licence + access-grant + event-bus interactions. `IssueLicense`,
  `RevokeLicense`, `IssueDownload`, and `GrantAccess` are the four
  entry points; the latter satisfies `port.DigitalAccessGrantor` so
  the v2.4.0 Temporal order-completion path can call exactly the
  same code path without further refactoring.
- New REST endpoints under `/api/v1/`, all gated by the existing
  RBAC middleware (operator+ for mutations, viewer+ for reads):
  - `GET /digital-products`, `POST /digital-products`,
    `GET/PATCH/DELETE /digital-products/{id}`,
    `GET /digital-products/{id}/download?customer_id=...` (admin
    download URL minting).
  - `GET /licenses`, `POST /licenses`, `GET /licenses/{id}`,
    `POST /licenses/{id}/revoke`.
  - `GET /me/licenses`, `GET /me/licenses/{id}/download` —
    customer-scoped surfaces. The customer's UUID is derived
    deterministically from `(tenant_id, subject)` via UUIDv5 so a
    stable identity exists without a separate Member table.
- Eight new event-bus types in `internal/eventbus/event.go`:
  `digital.product.created`, `digital.product.updated`,
  `digital.product.deleted`, `digital.purchased`, `digital.downloaded`,
  `license.activated`, `license.revoked`, `license.expired`. All carry
  the typed `eventbus.DigitalPayload{ Version, TenantID, ProductID,
  ProductSKU, LicenseID, CustomerID, State, Source }`. Tenant id is
  duplicated at the envelope level so webhook bridges that read only
  the payload still see tenant scoping.
- New migration `migrations/0008_digital.up.sql` (forward-only,
  idempotent) creating `digital_products`, `digital_licenses`,
  `digital_access_grants`, and `digital_download_tokens`. Every
  table is `(tenant_id, id)` keyed and constrained so cross-tenant
  reads are impossible at the database level. Re-purchases are
  idempotent via the `(tenant_id, customer_id, product_id)` unique
  constraint on `digital_access_grants`.
- OpenAPI spec at `api/openapi.yaml` updated with all 11 new
  operations and 8 new schemas (`DigitalProductRequest/Response`,
  `LicenseRequest/Response`, `DigitalDownloadResponse`,
  `LicensesListResponse`, `DigitalProductsListResponse`).

### Notes

- Order-flow integration: the `port.DigitalAccessGrantor` seam is in
  place, but the production order-completion path is still HTTP-only
  in v2.3.0. The v2.4.0 marketplace work introduces the Temporal
  order-completion workflow; that workflow will call
  `digitalSvc.GrantAccess` directly without further changes here.
- HMAC secrets default to deterministic dev values
  (`ECOMMERCE_DIGITAL_HMAC_SECRET`,
  `ECOMMERCE_DIGITAL_URL_SECRET`,
  `ECOMMERCE_DIGITAL_DOWNLOAD_BASE_URL`). Production deployments
  MUST override all three to >= 32-byte values.

## Unreleased (v2.2.0 MVP)

### Added — Membership bounded context

- New domain package `internal/domain/membership/` with `Member`,
  `MembershipPlan`, and `Subscription` aggregates plus an explicit
  state machine (`trial`/`active`/`paused`/`cancelled`/`expired`) driven
  by a transition table. Every legal `(state, transition) -> state`
  triple is exhaustively asserted by table-driven tests; every illegal
  pair returns the typed `membership.ErrInvalidTransition`.
- New ports `port.MembershipRepository`, `port.MembershipPaymentGateway`,
  and `port.MembershipNotificationSender` with compile-time
  assertions added to `internal/port/contract_test.go`.
- New adapters:
  - `internal/adapter/postgres/membership_repo.go` (tenant-keyed CRUD
    backed by tables introduced by migration `0007_membership.sql`).
  - `internal/adapter/inmemory/membership_repo.go` (used by tests and
    the dev compose stack).
  - `internal/adapter/stripe/payment_gateway.go` — deterministic
    Stripe stub that returns hash-derived stable IDs so workflow
    replay tests are hermetic. Real Stripe lands in v2.5.0.
  - `internal/adapter/notification/membership_sender.go` — in-memory
    `MembershipNotificationRecorder` for tests + dev.
- `MembershipLifecycleWorkflow` in `internal/workflow/membership_lifecycle.go`
  with `ChargeStripe`, `SendNotification`, and `RecordBillingEvent`
  activities. Workflow code is deterministic (no `time.Now`, no
  `rand`), every side effect goes through an activity, and a
  determinism smoke test (`TestMembershipLifecycleWorkflowDeterministicSmoke`)
  asserts identical results across two runs.
- Membership-specific HTTP endpoints on `mc-api`:
  - `POST/GET /api/v1/memberships`, `GET/PATCH /api/v1/memberships/{id}`,
    `POST /api/v1/memberships/{id}/{cancel,pause,resume}`.
  - `POST/GET /api/v1/membership-plans`, `GET/PATCH/DELETE /api/v1/membership-plans/{id}`.
  - All routes gated by RBAC (`membershipsRole`, `membershipPlansRole`)
    and `withTenantRequired`. Customer-reads-own / admin-reads-any rule
    enforced inside the handler. Mutations emit audit events.
- Eventbus integration:
  - New `EventType` constants `membership.created`, `membership.renewed`,
    `membership.cancelled`, `membership.paused`, `membership.resumed` in
    `internal/eventbus/event.go` (alongside the existing product/order
    types, single source of truth for the bus contract).
  - New `eventbus.MembershipPayload` typed envelope (versioned,
    tenant-aware) and `eventbus.NewMembershipEvent` constructor that
    validates required fields (tenant, subscription, plan, state) and
    rejects non-membership event types. Table-driven serialisation tests
    in `internal/eventbus/membership_test.go`.
  - New workflow-side adapters in `internal/adapter/notification/`:
    `BusSender` (implements `port.MembershipNotificationSender` by
    publishing to an `eventbus.Publisher`, with a
    `transitionToEventType` mapper covering activate/renew/cancel/pause/
    resume) and `MultiSender` (fan-out with `errors.Join`). The
    workflow's notifier is now wired as
    `MultiSender(Recorder, BusSender)` so every state transition emits
    the same canonical eventbus envelope as the handler path.
  - Handler `publishMembershipEvent` refactored to use the typed
    constructor; inline `map[string]any` payload assembly removed.
- OpenAPI spec (`api/openapi.yaml`) updated with all new endpoints +
  `Money`, `MembershipPlanRequest`, `MembershipPlanResponse`,
  `MembershipPlansListResponse`, `MembershipCreateRequest`,
  `MembershipUpdateRequest`, `MembershipResponse`, and
  `MembershipsListResponse` schemas.

### Operational notes

- Postgres migration numbering: existing tree uses `0001`-`0006`
  (compliance reporting), so the membership migration is `0007`.
- The workflow replay test (`TestMembershipLifecycleWorkflowReplaysGoldenHistory`)
  skips with a regen-instruction message when the golden history JSON
  is absent. The deterministic smoke test (always on) plus the
  exhaustive state-machine matrix covers determinism in the MVP run.
  Capturing the golden JSON requires a running Temporal dev server and
  is tracked as a v2.2.1 follow-up.

## Unreleased (v2.1.0 MVP)

### Added

- `uiauto`-profile docker compose services in `docker-compose.dev.yml`:
  `uiauto-chrome` (chromedp/headless-shell on host port 9333 -> container 9222),
  `uiauto-omniparser-stub` (wiremock OmniParser stub on host port 8001),
  and `uiauto-runner` that builds the `ui-agent` binary from the host
  `UIAUTO_FRAMEWORK_PATH` checkout and mounts the frontend's
  `test/uiauto/scenarios/` read-only.
- Documented build harness override at `test/uiauto/Dockerfile.runner`
  tracking upstream issue `nfsarch33/uiauto-framework#8` (Dockerfile pins
  Go 1.24 while go.mod requires Go 1.26). The framework source is pulled
  via a docker compose `additional_contexts` named context so this is a
  build-time override, not a vendor fork.
- Bundled smoke scenario at `test/uiauto/example/scenario.json` so
  `make uiauto-smoke` works without the frontend repo on disk.
- `make` targets `uiauto-smoke`, `uiauto-compare`, `uiauto-up`,
  `uiauto-down`, and `compose-uiauto-config`.
- New `cmd/uiauto-compare` binary plus
  `internal/uiauto/compare/{types,parser,diff,report,runner}.go` that
  parse Playwright JSON reporter output and uiauto-framework
  `demo-metrics.json` and emit a structured diff.json + summary.md to
  `reports/uiauto-comparison/<date>/`. All parser/diff/report functions
  are covered by table-driven tests.
- Default fixtures at `test/uiauto/fixtures/{playwright,uiauto,mapping.json}`
  for the five v2.1.0 prioritised scenarios (home, products, checkout,
  admin-login, admin-agents) so `make uiauto-compare` is hermetic.

### Operational notes

- v2.1.0 is research-mode: uiauto runs are advisory. The plan defers the
  gate-vs-advisory decision for v4 to the v2.8.0 sprint.
- Coverage holds at 83.2% (v2.0.0 baseline); the comparison package
  contributes its own coverage above the gate.

## v2.0.0 - 2026-05-08

### Release Summary

v2.0.0 completes the v1.1.0-v2.0.0 roadmap and promotes the backend from a v1.0 Mission Control API into the durable orchestration spine for the full Agentic Ecommerce stack. The release combines Redis Streams events, tenant-aware data access, Temporal workflows, pgvector-backed RAG and fact-checking, Media Intelligence, outbound webhooks for n8n automation, recurring agent schedules, cloud hardening, performance/security gates, and v1.9 tenant compliance reporting into one documented release candidate.

### Capabilities Included

- Redis Streams event bus with tenant-aware event envelopes, at-least-once delivery semantics, consumer groups, and dead-letter handling.
- Tenant-aware PostgreSQL model coverage for catalog, orders, product media, RAG documents, tenant settings, custom compliance rules, compliance history, and export/reporting surfaces.
- Temporal workflow layer for `ProductPublishWorkflow`, `ContentGenerationWorkflow`, `MediaProcessingWorkflow`, `SourcingWorkflow`, human-review signals, status lookup, and worker deployment through the `temporal-worker` service.
- CCE expansion in Go with pgvector-backed RAG ingestion/search, fact-check evidence retrieval, content evaluation, compliance checks, SEO readiness, and bridge-only AI boundaries.
- Media Intelligence API coverage for supplier image sourcing, deterministic processing, quality validation, object-store persistence, CDN-ready URLs, and workflow-driven product linking.
- Outbound webhook registration, HMAC signing, test delivery, WooCommerce inbound webhook contracts, and credential-free n8n example workflow templates.
- Advanced agent runtime contracts for sourcing, pricing, compliance, schedule controls, worker metrics, and automation events.
- Production hardening for Compose, Terraform AWS ECS/GCP Cloud Run dry-run contracts, Temporal placeholders, S3/GCS media storage, secret-manager mappings, CDN stubs, autoscaling intent, and monitoring validation.
- v1.8-v1.9 QA baselines covering contract tests, k6 performance targets, tenant isolation fixtures, compliance report export, docs-inclusive leak scans, Sentrux gates, and shell-leak hygiene.

### Release Gates

- Backend documentation and contract gates: `VERSION`, `api/openapi.yaml`, `CHANGELOG.md`, `README.md`, `docs/api-reference.md`, `docs/temporal-workflow-specs.md`, `docs/webhook-contracts.md`, and ADR-025 all identify v2.0.0.
- Runtime gates: `go test -race ./...`, `go vet ./...`, `make build`, `make coverage-check`, `make contract-test`, `make release-perf-smoke`, `make monitoring-validate`, `make compose-config`, and `make compose-config-prod`.
- Workflow and automation gates: `make compose-temporal-config`, `go test ./internal/workflow/...`, `make compose-agent-schedules-config`, `make n8n-config`, and `make n8n-workflows-validate`.
- Security gates: `make security-refresh`, `sentrux gate .`, `runx shell-leak-scan --repo ecommerce`, and docs-inclusive checks for live secrets, private hosts, internal IPs, provider credentials, account IDs, `.tfvars`, and direct MiniMax app-service calls.
- Cross-stack gates: frontend generated API types must be regenerated from `api/openapi.yaml`; release notes must include final backend and frontend SHAs.

### Notes

- This release prepares documentation, contracts, and version metadata for v2.0.0. It does not tag or publish a GitHub release by itself.
- ADR-025 records the release decisions and v3.0.0 roadmap preview.

## v1.0.0 - 2026-05-07

### Release Summary

v1.0.0 graduates the Go backend from scaffold to release-ready Mission Control spine for the Agentic Ecommerce stack. The release consolidates the v0.1.0-v0.9.0 work into a public, documented backend with product and order APIs, WooCommerce sync contracts, AI-assisted content generation, compliance and SEO checks, worker scaffolding, Docker Compose operations, cloud deployment dry-runs, observability, JWT/RBAC security, and release gates.

### Capabilities Included

- Product catalog API with PostgreSQL-backed repository contracts, CRUD endpoints, media placeholders, OpenAPI coverage, and generated frontend client compatibility.
- Order, cart, checkout, and order status contracts for the storefront purchase flow.
- WooCommerce sync worker path with dry-run defaults, webhook contract coverage, Redis event-bus channel names, and conflict-resolution API surface.
- AI content agent endpoints routed through the approved fleet bridge boundary, with content suggestions, generated descriptions, and quality scoring hooks.
- Agent orchestration contracts for sourcing, content, pricing, and compliance agents, including scheduler and worker runtime placeholders.
- Content compliance, SEO, and media-processing contracts for publish-readiness checks.
- Production-like Docker Compose stack with `mc-api`, frontend image wiring, PostgreSQL + pgvector, Redis, Prometheus, Grafana, and opt-in worker profiles.
- Credential-free AWS ECS and GCP Cloud Run Terraform dry-run scaffolding under `deploy/terraform/`.
- Observability baseline with `/healthz`, `/readyz`, `/metrics`, structured request logs, request IDs, alert rules, and Grafana dashboard provisioning.
- Security baseline with JWT login, RBAC roles, rate limiting, audit logging for mutations, public safety guidance, and explicit secret boundaries.

### Release Gates

- Backend quality gates: `go test -race ./...`, `go vet ./...`, `make build`, `make monitoring-validate`, and coverage review against the v1.0.0 threshold.
- Deployment gates: Docker Compose config validation, full-stack smoke before promotion, and Terraform `fmt`/`validate` plus plan-only cloud dry-runs.
- Security gates: docs-inclusive shell-leak scan, no committed live credentials, no private endpoints in docs, and no direct MiniMax calls from app services.
- Contract gates: `api/openapi.yaml` remains the source of truth for backend APIs consumed by the frontend.

### Notes

- This release prepares documentation and versioning for v1.0.0. It does not tag or publish a GitHub release by itself.
- ADR-024 records the release decisions and v1.1.0 roadmap preview.
