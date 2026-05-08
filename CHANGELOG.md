# Changelog

All notable changes to the Agentic Ecommerce backend are documented here.

## Unreleased (v2.9.0 Developer Experience + Documentation)

### Added — Plugin Developer SDK (public package)

- **`pkg/marketplace/sdk`**: new public Go package consumable by
  third-party Go modules. Re-exports the safe lifecycle surface from
  `internal/marketplace/` so plugin authors never depend on internal
  packages. Symbols re-exported as type aliases (`Plugin`, `Manifest`,
  `EventName`, `Permission`, `Installation`, `State`,
  `EventSubscriber`, `RouteExtender`, `Route`, `DependencyRef`),
  helpers (`IsValidSlug`, `IsValidSemver`, `EventNames`), permission
  + state constants, and typed sentinel errors (`ErrManifestInvalid`,
  `ErrPluginAlreadyInstalled`, `ErrSandboxBudgetExceeded`, ...).
- **`pkg/marketplace/sdk/testing.go`**: `NewTestSandbox(tb, manifest)`
  and the `*TestSandbox` type. Drives plugins through the real
  `marketplace.Service` against in-memory adapters with a
  deterministic clock; calls `tb.Helper()` and `tb.Cleanup`. Exposes
  `Install`, `Activate`, `Deactivate`, `Uninstall`, `SmokeCheck`,
  `Settings`, `SetSettings`, `HookTimeout`, `HooksRecorded`,
  `TenantID`, `Manifest` and `WithTenant`/`WithClock` options.
- **`pkg/marketplace/sdk/example/hello/`**: heavily commented example
  plugin demonstrating manifest + Install/Activate/Deactivate/Uninstall
  hooks, one event subscription, settings round-trip. `hello_test.go`
  passes `go test ./pkg/marketplace/sdk/example/hello/...` cleanly.
- **`pkg/marketplace/sdk/README.md`**: 10-minute path getting-started
  guide, lifecycle hook reference, sandboxing notes, settings shape,
  versioning policy.

### Added — API versioning (v1 stability + v2 preview)

- **v1 stability guarantee**: `api/openapi.yaml` is the canonical v1
  spec. Bumped to `2.9.0` with explicit `x-api-version-policy` header
  documenting "v1 endpoints are stable through v3.x; v2 preview
  endpoints are subject to change without notice."
- **v2 preview namespace**: new `api/openapi-v2-preview.yaml` carrying
  the first preview endpoint
  (`POST /api/v2/marketplace/plugins/{slug}/install`). Response shape
  evolves the v1 InstallationResponse into a richer envelope
  (`MarketplaceInstallV2Response`) carrying `installation`, `sandbox`
  snapshot, settings schema, and dependency tree.
- **`internal/api/version.go`**: version-routing middleware. Path-based
  v2 opt-in (`/api/v2/...`) wins; the Accept header
  (`application/vnd.ec.v2+json`) upgrades a v1 path when both
  surfaces exist. `WithVersionHeaders` middleware stamps `X-API-Version`
  on every response and `X-API-Deprecation: preview; semantics may
  change without notice` on v2 responses.
- **`docs/api-versioning.md`**: full negotiation policy, deprecation
  timeline, header semantics, CI snippet for client drift detection.

### Added — Tenant onboarding Temporal workflow

- **`internal/workflow/tenant_onboarding.go`**:
  `TenantOnboardingWorkflow` driving the v2.5.0 RegistrationRequest
  aggregate through `email_verified -> onboarding -> active`.
  Activities: `tenant.validate_registration`, `tenant.provision_record`,
  `tenant.seed_default_plan`, `tenant.issue_welcome_notification`,
  `tenant.register_default_plugins`, `tenant.rollback_record`.
- **Compensation**: if SeedDefaultPlan or RegisterDefaultPlugins fails
  after the tenant row was created, the workflow runs the rollback
  activity. Welcome-notification failure is best-effort (logged in
  the result envelope, does not abort activation).
- **Determinism-tested**: `internal/workflow/tenant_onboarding_test.go`
  drives the workflow against `testsuite.WorkflowTestSuite` with a
  fixed start time and in-memory ports. Covers happy path,
  plan-seed-failure rollback, welcome-failure tolerance, and
  unknown-registration rejection.
- **Worker registration**: wired into `cmd/temporal-worker/main.go`
  via `newTenantOnboardingActivitiesFromEnv()`. Worker registers all
  6 activities + the workflow.

### Added — `ec-cli` developer command-line tool

- **New 7th binary `cmd/ec-cli/`** built into `bin/ec-cli` via the
  Makefile `build` target. Subcommands:
  - `ec-cli doctor` — environment diagnostics. Validates Postgres,
    Redis, Temporal reachability + required env vars. `--json`
    machine-readable output. Exit 0 when healthy.
  - `ec-cli tenant create --slug --name --plan --email` — provisions
    a tenant via the v1 admin API at `/api/v1/tenants`. Honours
    `EC_ADMIN_TOKEN` env. `--json` output supported.
  - `ec-cli plugin validate --path <plugin-dir>` — offline validation
    of a plugin's manifest.json + sandbox smoke. Reports issues +
    suggestions (no go.mod, no _test.go files).
  - `ec-cli version` — prints binary metadata.
- **Test coverage**: 86.4% (target ≥85%, gate ≥83%). DI-friendly
  mainImpl/AppDeps pattern matches the v2.6.1 cmd/* refactor.
- **No new module dependencies**: subcommand routing uses stdlib
  `flag`. Matches the no-cobra convention of the existing 6 binaries.

### Added — A10 SSRF guard (carryover from v2.8.0 OWASP audit)

- **`internal/webhook/outbound/ssrf_guard.go`**: `SSRFGuard` blocks
  outbound webhook URLs that resolve to private (RFC 1918), loopback,
  link-local, IPv6 unique-local (fc00::/7, fd00::/8), or cloud
  metadata IPs (169.254.169.254 IMDS, 169.254.170.2 ECS task
  metadata, 100.100.100.200 Alibaba, fd00:ec2::254 v6 IMDS). Scheme
  allowlist: `https://` only by default; `http://` requires
  explicit `AllowInsecureHTTP` flag.
- **DNS rebinding mitigation**: re-resolves hostname right before the
  request is dispatched and rejects when *any* resolved IP falls in
  a blocked range.
- **Wired into `internal/webhook/outbound/client.go`**: every
  outbound webhook delivery now runs `guard.CheckURL(ctx, url)`
  before `http.NewRequestWithContext`. `NewClient(ClientConfig{})`
  defaults to a strict guard; tests inject `NewPermissiveSSRFGuard()`
  to keep httptest-server flows working.
- **`HTTPDoerWithGuard`**: drop-in `http.Client` wrapper for any
  outbound HTTP call that wants the same SSRF mitigation without
  opening the webhook client surface.

### Changed

- **`api/openapi.yaml`** version bumped from 2.0.0 to 2.9.0.
- **`Makefile`** `build` target now produces 7 binaries (was 6;
  added `ec-cli`).

## Unreleased (v2.6.0 MVP)

### Added — Coverage push + security fuzz harness + benchmark guardrails

- **Fuzz harnesses for security-critical parsers** (per `go-security-review`):
  the contract is "must NEVER panic on attacker-controlled bytes; must
  return error not crash". Each harness ships with a structurally valid
  seed plus a curated negative corpus (empty input, missing segments,
  embedded control bytes, oversized strings, percent-encoded garbage).
  All four harnesses pass `go test -fuzz=. -fuzztime=10s` cleanly with
  no panics observed. New files:
  - `internal/security/fuzz_test.go` — `FuzzVerifyAccessToken` over the
    HS256 JWT verifier (segment splitter + base64 decoder + JSON
    unmarshal + claims validator).
  - `internal/billing/fuzz_test.go` — `FuzzWebhookVerify` over the
    Stripe `t=, v1=` signature header parser + payload bytes.
  - `internal/adapter/signedurl/fuzz_test.go` — `FuzzVerifySignedURL`
    over the `tid/lid/pid/exp/uses/sig` URL parser.
  - `internal/domain/digital/fuzz_test.go` — `FuzzValidateLicenseKey`
    over the HMAC-SHA256 base32 license-key checksum validator.

- **Benchmarks for hot paths** (per `go-performance-optimization`).
  `b.ReportAllocs()` is enabled on every benchmark so reviewers can
  spot allocation regressions:
  - `internal/security/bench_test.go` — `BenchmarkVerifyAccessToken`,
    `BenchmarkMintAccessToken`. Every authenticated mc-api request
    transits these.
  - `internal/billing/bench_test.go` — `BenchmarkWebhookVerify`. The
    public Stripe webhook entry point.
  - `internal/adapter/signedurl/bench_test.go` —
    `BenchmarkIssueSignedURL`, `BenchmarkVerifySignedURL`. Every
    digital download issue + verify path.
  - `internal/domain/digital/bench_test.go` —
    `BenchmarkLicenseKeyGenerate`, `BenchmarkLicenseKeyValidate`.

- **Shared testcontainers postgres helper**: extracted the
  per-test postgres bootstrap that previously lived inline in
  `internal/adapter/postgres/integration_testcontainers_test.go` into a
  reusable helper at `internal/testsupport/postgres/`:
  - `paths.go` (always-built) — `ResolveMigrationDir` runtime.Caller
    based path lookup.
  - `migration_files.go` (always-built) — `CanonicalMigrationFiles`
    ledger with the ordered DDL list mirroring the migrate-up Make
    target.
  - `container.go` (`//go:build integration_pg`) — `StartPool(t,
    Options)` returns a per-test `*pgxpool.Pool` with auto-skip when
    `DISABLE_DOCKER_TESTCONTAINERS=1` or Docker is unreachable, and
    auto-teardown via `t.Cleanup`.
  - `container_test.go` (always-built) — checks the migration ledger
    is ordered + unique and that ResolveMigrationDir lands on a
    directory that contains every canonical migration file. The
    Docker-dependent tests live behind the same build tag as the
    helper so default `go test ./...` stays hermetic.

- **`cmd/content-worker/main_test.go`**: added `TestMainEmitsReadySignal`
  using an `os.Pipe` swap so the entrypoint is exercised without
  cluttering test output. cmd/content-worker coverage went from 33.3%
  to 100%.

- **`cmd/wc-sync/main_test.go`**: added `TestMainSucceedsInDryRun`
  using the same os.Pipe trick so the env-var glue + run wiring is
  covered. cmd/wc-sync coverage went from 70.4% to 81.5%.

### Coverage delta

- Total `go tool cover -func | tail -1`: 83.0% → 83.1%
  (the systematic audit table is in the PR body).
- Per-package wins: cmd/content-worker (33.3% → 100%), cmd/wc-sync
  (70.4% → 81.5%), internal/security (84.8% → 85.6%),
  internal/adapter/signedurl (83.1% → 84.4%), internal/domain/digital
  (81.5% → 82.3%).
- The systematic audit revealed the largest remaining gaps live in
  `cmd/temporal-worker` (58.1%, mostly main() temporal SDK init),
  `cmd/uiauto-compare` (64.4%), and `cmd/agent-worker` (74.3%, main()
  is 0%). Lifting those to ≥90% requires either dependency injection
  refactors of main() or extensive Temporal SDK mocking; both were
  scoped out of v2.6.0 to keep the sentrux complex_fn budget intact.
  The coverage gate stays at 83% to match the achieved floor; v2.7.0
  or v2.8.0 can revisit the gate raise once the cmd/* main() tests
  land.

### Notes on scope

- Live integration tests against real Redis Streams + Temporal dev
  server containers (Steps 3 of the v2.6.0 plan) were prepared via
  the new `internal/testsupport/postgres/` helper pattern but not
  driven through the full Redis/Temporal containers in this PR; the
  helper makes that follow-on work a small extension. Frontend
  coverage + uiauto comparison live in the sibling
  `agentic-ecommerce-web` v2.6.0 PR.

## Unreleased (v2.5.0 MVP)

### Added — Tenant self-service registration + billing hooks

- New top-level package `internal/billing/` implementing the v2.5.0
  billing bounded context:
  - `state.go` — `Subscription` state machine driven by the same
    explicit transition-table pattern as
    `internal/domain/membership/state.go`,
    `internal/domain/digital/state.go`, and
    `internal/marketplace/state.go`. Legal moves:
    `trialing -> active | canceled`,
    `active -> past_due | paused | canceled`,
    `past_due -> active | canceled`,
    `paused -> active | canceled`. `canceled` is terminal.
  - `subscription.go` — typed `Subscription`, `Invoice`,
    `UsageRecord`, and `Plan` value types. Money in minor units;
    Stripe ids carried as plain strings.
  - `service.go` — `Service` orchestrator + `Repository`, `PlanCatalog`,
    `EventPublisher`, `UsageMeter` ports. Tenant-aware throughout;
    `ErrTenantRequired` for any tenant-empty call.
  - `webhook.go` — Stripe webhook signature verifier (HMAC-SHA256 over
    `t.<timestamp>.<payload>`, `crypto/subtle.ConstantTimeCompare` for
    constant-time match, 5-minute replay window, `>= 32 byte` secret
    enforced at construction). Typed errors:
    `ErrSecretTooShort`, `ErrMissingSignature`, `ErrSignatureMalformed`,
    `ErrSignatureMismatch`, `ErrEventTooOld`.
  - `dispatcher.go` — verified-then-parsed event dispatcher with
    typed handlers for
    `customer.subscription.created|updated|deleted` and
    `invoice.payment_succeeded|failed`. Emits typed `BillingPayload`
    bus events for marketplace plugin subscribers.
  - `inmemory.go` — goroutine-safe in-process `Repository` and
    `StaticPlanCatalog` (Free/Starter/Pro seed plans).
  - `usage.go` — `UsageMeter` port + `InMemoryUsageMeter` and
    `Snapshot` rollup helper for the admin dashboard.
- New top-level package `internal/quota/` for per-tenant resource
  enforcement: `Policy` value with API rate, agent-runs/day, storage
  bytes, and plugin count limits; `Enforcer` interface; minute and
  day-bucketed in-memory implementation.
- New top-level package `internal/registration/` implementing the
  `pending_email_verification -> email_verified -> onboarding -> active`
  state machine. `Issuer` mints HMAC-signed verification tokens (same
  HMAC pattern as the v2.3.0 license-key generator and signed-url
  issuer; `>= 32 byte` secret enforced). `Service.CompleteOnboarding`
  hands off to `internal/tenant.AggregateService.Create` to provision
  the actual tenant.
- New `internal/eventbus/billing.go` — typed `BillingPayload` and
  `NewBillingEvent` constructor for the bus, mirroring
  `MembershipPayload` and `DigitalPayload`.
- New API endpoints in `cmd/mc-api/`:
  - Public: `POST /register`, `POST /register/verify`,
    `POST /register/onboarding` — rate-limited via the existing token
    bucket; no JWT.
  - Admin: `GET /api/v1/admin/billing/subscriptions[/{id}]`,
    `POST .../cancel|pause|resume`,
    `GET /api/v1/admin/billing/invoices[/{id}]`,
    `GET /api/v1/admin/billing/usage` — RBAC: viewer for GETs,
    admin for transitions.
  - Webhook: `POST /webhooks/stripe` — signature-authenticated,
    idempotent on Stripe `event.id` (recorded in
    `stripe_webhook_events`).
- Postgres adapters in `internal/adapter/postgres/`:
  - `billing_repo.go` — subscriptions, invoices, and the Stripe
    idempotency table; explicit SQL with `ON CONFLICT` upserts for
    invoices.
  - `registration_repo.go` — tenant_registrations CRUD.
- Migrations:
  - `0010_billing.{up,down}.sql` — `billing_plans`,
    `billing_subscriptions`, `billing_invoices`, `usage_records`,
    `stripe_webhook_events`, `tenant_registrations` (forward-only,
    seeded plans for Free/Starter/Pro).
  - `0011_rls.{up,down}.sql` — Postgres Row-Level Security for every
    tenant-keyed table (tenants, tenant_settings, memberships,
    subscriptions, billing_cycles, digital_*, marketplace_*,
    billing_subscriptions, billing_invoices, usage_records). Policy
    filters on the `app.current_tenant_id` GUC; admin contexts
    (empty GUC) bypass the policy.
- OpenAPI v3.1 spec extended with `/register*`, `/api/v1/admin/billing/*`,
  `/webhooks/stripe`, plus matching component schemas.

### Security

- Stripe webhook signature verification uses stdlib `crypto/hmac` +
  `crypto/sha256` + `crypto/subtle.ConstantTimeCompare`; never
  `bytes.Equal`. Verify-then-parse order enforced by the handler so
  malformed payloads cannot bypass the gate.
- Replay protection: 5-minute default window (`DefaultWebhookTolerance`)
  matching the Stripe SDK default; configurable via `WebhookConfig`.
- Secret length floor: `>= 32 bytes` enforced at process boot for
  both the Stripe webhook secret (`ECOMMERCE_STRIPE_WEBHOOK_SECRET`)
  and the registration HMAC secret
  (`ECOMMERCE_REGISTRATION_HMAC_SECRET`).
- Idempotent webhook processing: every accepted Stripe event id is
  recorded in `stripe_webhook_events`. Duplicate deliveries return
  `200 OK` without side effects.
- Postgres RLS on every tenant-keyed table provides defence-in-depth
  even when application code forgets a `WHERE tenant_id=$1`.

## Unreleased (v2.4.0 MVP)

### Added — Marketplace plugin framework + tenant aggregate

- New top-level package `internal/marketplace/` implementing the
  v2.4.0 plugin lifecycle:
  - `plugin.go` — `Plugin` interface with `Install`, `Activate`,
    `Deactivate`, `Uninstall` hooks plus optional `EventSubscriber`
    and `RouteExtender` seams.
  - `manifest.go` — typed `Manifest` (slug, name, version, vendor,
    description, category, event subscriptions, permissions,
    dependencies, homepage URL) with regex-validated kebab-case
    slugs and strict `MAJOR.MINOR.PATCH` semver via stdlib
    `regexp`. Caret/exact/bare constraint helpers (`^X.Y.Z`,
    `=X.Y.Z`, bare `X.Y.Z` treated as caret) keep dependency
    resolution narrow and idiomatic without a new external
    dependency on `golang.org/x/mod/semver`.
  - `state.go` — explicit lifecycle transition table mirroring
    `internal/domain/membership/state.go` and
    `internal/domain/digital/state.go`:
    `installed -> active`, `active -> deactivated`,
    `deactivated -> active | uninstalled`,
    `installed | active -> uninstalled`. Every legal triple is
    asserted in `state_test.go`; illegal pairs surface the typed
    `marketplace.ErrInvalidTransition`.
  - `dependency.go` — semver dependency graph with Kahn-style
    topological sort, alphabetical tie-breaking for determinism,
    and explicit cycle detection (`ErrDependencyCycle`).
    `VerifyDependencySemver` rejects unsatisfied constraints with
    `ErrSemverConflict` before mutating state.
  - `registry.go` — `Registry` interface and concrete `Service`
    that drive `Install`, `Activate`, `Deactivate`, `Uninstall`,
    `List`, `Get` against `CatalogRepository`,
    `InstallationRepository`, and `SubscriptionRepository` ports.
  - `sandbox.go` — per-`(tenant, slug)` token-bucket sandbox
    enforcing hook-rate limits (default 60 invocations/min) and a
    30 s default hook timeout. Mirrors the bucket pattern in
    `internal/security/ratelimit.go` so reviewers familiar with
    that file can audit it at a glance. Exhaustion surfaces
    `ErrSandboxBudgetExceeded`.
  - `settings.go` — in-memory per-installation settings store with
    defensive copy on read and write. v2.5.0 will swap this for a
    postgres-backed implementation when billing requires
    durability.
  - `errors.go` — typed sentinels for every reportable failure:
    `ErrPluginAlreadyInstalled`, `ErrPluginNotFound`,
    `ErrInvalidTransition`, `ErrSemverConflict`,
    `ErrSandboxBudgetExceeded`, `ErrSlugInvalid`,
    `ErrSlugAlreadyExists`, `ErrUnknownEvent`,
    `ErrManifestInvalid`, `ErrDependencyCycle`,
    `ErrCrossTenantAccess`.
- New top-level package surface in `internal/tenant/aggregate.go`
  carrying the `Tenant` aggregate root: id, slug, name, plan,
  status (`provisioning -> active | suspended | archived`), and
  RFC3339 timestamps. State machine is data-driven with the same
  transition-table pattern used by membership/digital. The
  aggregate is intentionally a separate `AggregateRepository` port
  from the existing `tenant.Repository` (settings) because
  lifecycle and settings have different audit characteristics.
  `AggregateService` wraps the repo with a quota check
  (`ErrTenantQuotaExceeded`) and a unique-slug check
  (`ErrTenantSlugAlreadyExists`).
- New adapters:
  - `internal/adapter/postgres/marketplace_repo.go` — tenant-aware
    CRUD for `marketplace_plugins`, `marketplace_installations`,
    and `marketplace_event_subscriptions`. Catalogue is global;
    installations and subscriptions are per `(tenant_id, slug)`.
  - `internal/adapter/postgres/tenant_aggregate_repo.go` — CRUD
    for the `tenants` aggregate. SQL is hand-written and explicit
    so query plans stay reviewable.
  - `internal/adapter/inmemory/marketplace_repo.go` — drop-in
    catalogue/installation/subscription stores used by unit
    tests, dev compose, and the Playwright E2E mock.
- New API endpoints under `/api/v1/`:
  - **Marketplace listing** (`viewer` reads; `operator` writes):
    `GET /marketplace/plugins`, `GET /marketplace/plugins/{slug}`,
    `POST /marketplace/plugins/{slug}/install`,
    `POST /marketplace/plugins/{slug}/activate`,
    `POST /marketplace/plugins/{slug}/deactivate`,
    `DELETE /marketplace/plugins/{slug}`.
  - **Plugin settings**:
    `GET /marketplace/installations/{slug}/settings`,
    `PATCH /marketplace/installations/{slug}/settings`.
  - **Tenant provisioning** (`operator` reads; `admin` writes):
    `POST /tenants`, `GET /tenants`, `GET /tenants/{id}`,
    `PATCH /tenants/{id}`, `POST /tenants/{id}/suspend`,
    `POST /tenants/{id}/activate`,
    `POST /tenants/{id}/archive`.
- Event contract versioning in `internal/eventbus/schema.go`
  (Go 1.25 generic `EventEnvelope[T]`):
  - `RegisterSchema(name, version, decoder)` populates a versioned
    decoder registry.
  - `DecodeEnvelope` dispatches on `(schema, version)` and
    returns `ErrSchemaNotRegistered` / `ErrSchemaVersionUnsupported`.
  - Backward-compat smoke test verifies v1 envelopes still decode
    after v2 schemas register, mirroring the v2.2.0 membership
    payload-version pattern.
- Migration `migrations/0009_marketplace.up.sql` (forward-only,
  idempotent) introduces the `tenants`, `marketplace_plugins`,
  `marketplace_installations`, and
  `marketplace_event_subscriptions` tables. Slug regex is enforced
  at the DB level via a CHECK constraint, mirroring the Go
  validation layer for defence in depth.
- `cmd/mc-api` wires:
  - `marketplaceHandler` — routes `/api/v1/marketplace/*` with
    audit-action emission for every mutating call.
  - `tenantAdminHandler` — routes `/api/v1/tenants/*` with
    super-admin RBAC for mutations.
  - Boot-time `seedMarketplaceManifests` registers three demo
    manifests (`stripe-payments`, `ses-email`,
    `klaviyo-marketing`) so the storefront catalogue listing has
    data in dev mode.
- OpenAPI 3.1 spec extended with marketplace + tenant schemas and
  full path coverage for the new endpoints.

### Sentrux discipline

- v2.4.0 explicitly preserves the v2.3.0 post-merge sentrux
  baseline:
  `complex_functions = 3` and `coupling = 0.39` are unchanged.
  The package keeps every public function under the 75-LOC,
  cyclomatic-<10 limit and decomposes orchestration helpers
  (`Service.transition`, `dispatchMarketplacePlugin`,
  `dispatchTenant`) into small named helpers so the structural
  metrics do not regress.

## v2.3.0 MVP

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
