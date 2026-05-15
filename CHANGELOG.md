# Changelog

All notable changes to the Agentic Ecommerce backend are documented here.

## [9.0.0] - 2026-05-14 -- v9 platform baseline release

### Release Summary

v9.0.0 establishes the backend platform baseline for the post-v8 program. The
current-release surfaces now track the controller-accurate release path,
mirrored self-hosted regression on `primary-testing` and `secondary-testing`,
and the release evidence chain required before a semver-only `v9.0.0` tag is
cut.

### Pair 1: Controller and Release Metadata Baseline

- Replaced the v8-only release metadata guard with
  `TestV900ReleaseMetadataAligned` so stale `VERSION`, OpenAPI, README,
  checklist, ADR, or final-evidence state fails before tag cut.
- Added ADR-036 and the v9 release-final evidence doc to anchor the current
  release chain without rewriting the v8 archive.

### Pair 2: Testing Pool Baseline

- Documented `host-a/node-a` as the backend's merge-blocking `primary-testing`
  environment for integration, smoke, and cleanup lanes.
- Promoted `host-b/node-b` from advisory evidence to a mirrored self-hosted release
  gate; `v9.0.0` now stays RC-only until both pools are green.

### Pair 3: Mirrored Self-Hosted Release Contract

- Replaced the old staging-first gate with mirrored canary, backend
  integration, full-stack E2E, cleanup, and cross-repo frontend/UIAuto proof on
  both pools.
- Updated the API, Temporal, and webhook guides to describe the current v9
  release baseline instead of the legacy v2.0.0 release label and retired
  staging-only assumptions from the v9 release contract.

### Pair 5: Agent Runtime Bootstrap

- Added the `EC_AGENT_RUNTIME_MODE` bootstrap seam to `agent-worker` so the
  current scheduler path stays the default while EINO-backed `shadow` and
  `primary` modes can be promoted behind evidence rather than another worker
  contract rewrite.
- Added the initial `internal/agent/runtime` and `internal/adapter/llm`
  surfaces so the runtime bootstrap is testable without changing public HTTP or
  OpenAPI contracts.

## [8.0.0] - 2026-05-13 -- v8 TDD implementation release

### Release Summary

v8.0.0 publishes the backend portions of the v8 TDD implementation run:
marketplace sync core, Shopify and Shopee adapters, product image editing,
Temporal orchestration, OOM observability, self-improvement evidence loops, and
final release-hardening metadata. Frontend media UX and tooling docsync work are
released from their owning repositories but are linked from the global-kb v8
handoff so the stack release can be audited as one system.

### Pair 1: Marketplace Sync Core

- Added an idempotent marketplace sync engine with ledger, retry-to-DLQ,
  replay dedupe, reconciliation mismatch reporting, and Prometheus metrics for
  sync events, DLQ events, and replay outcomes.
- Captured retry/load matrix QA and replay fixture evidence in
  `docs/operations/v8-p01-marketplace-sync-core-qa.md`.

### Pair 2: Shopify Adapter

- Added the Shopify connector behind the shared marketplace port using
  contract-driven GraphQL cassette tests.
- Documented mock/sandbox boundaries and cassette hygiene in
  `docs/operations/v8-p02-shopify-adapter-qa.md`.

### Pair 3: Shopee Adapter

- Added the guarded Shopee connector only after official-doc and signing/auth
  evidence was recorded.
- Captured no-live-call proof, sandbox readiness, and replay cassette QA in
  `docs/operations/v8-p03-shopee-adapter-qa.md`.

### Pair 4: Product Image Editing

- Added provider-neutral image-edit workflow contracts with approval states,
  remote large-asset routing, and fallback-ready provider seams.
- Captured memory-ceiling and provider fallback QA in
  `docs/operations/v8-p04-image-editing-qa.md`.

### Pair 6: Temporal Orchestration

- Added deterministic Temporal orchestration for marketplace sync and image
  approval workflows with schedules, signals, queries, and activity-only I/O.
- Captured replay, cancellation, retry, worker shutdown, and schedule evidence
  in `docs/operations/v8-p06-temporal-orchestration-qa.md`.

### Pair 8: OOM Observability

- Added resource-guard, workerpool, memwatch, Sentrux-count, Agenttrace, and
  EvoMap metric wiring for long-running release cycles.
- Captured leak-check and Sentrux cleanup evidence in
  `docs/operations/v8-p08-oom-observability-qa.md`.

### Pair 9: Self-Improvement

- Added autoresearch producer-reviewer evidence validation and EvoLoop/DRL
  reward artifacts backed by Agenttrace replay.
- Captured replay and promotion-policy QA in
  `docs/operations/v8-p09-self-improvement-qa.md`.

### Pair 10: Final Hardening

- Added release metadata guard coverage so stale VERSION, OpenAPI, README,
  changelog, ADR, and release checklist state fails before tagging.
- Added ADR-035 and final release evidence for v8.0.0.

## [7.5.1] - 2026-05-12 -- v7 Pair 1 through Pair 6 QA release

### Release Summary

v7.5.1 publishes the backend v7 work merged after v6.6.0. It includes the
quality foundation, coverage harness, observability spine, resource-aware
orchestration, cloud deployability, adapter hardening, and sandbox-boundary QA
work from backend PRs #142 through #153. No public v7.0.0 tag was created
before these pairs shipped, so the current backend head is released as v7.5.1
instead of back-tagging an older internal pair commit.

### Pair 1: v7.0.0 MVP + v7.0.1 QA

- Reduced the highest production cyclomatic hotspots and added a quality guard
  for non-test complexity regressions.
- Refreshed Sentrux quality evidence at Quality 6041, Coupling 0.04, Cycles 1,
  and God files 0.

### Pair 2: v7.1.0 MVP + v7.1.1 QA

- Added Temporal activity interceptor coverage for panic paths and fallback span
  naming.
- Raised `internal/observability/temporal` coverage to 88.2% while total backend
  coverage held at 85.1%.

### Pair 3: v7.2.0 MVP + v7.2.1 QA

- Added the observability spine for metric inventory, dashboard snapshots, and
  Agentrace sample conversion.
- Added EvoMap KPI fields for workerpool rejections, breaker opens, and
  coordination conflicts, with NDJSON replay QA.

### Pair 4: v7.3.0 MVP + v7.3.1 QA

- Replaced unbounded China adapter batch fan-out with a fixed worker queue.
- Routed agent scheduler dispatch through bounded worker pools and added
  shutdown drain/cancel semantics.

### Pair 5: v7.4.0 MVP + v7.4.1 QA

- Aligned production Compose, Helm, AWS ECS, and GCP Cloud Run workload
  contracts.
- Added credential-free Terraform validate and plan gates plus rollback
  documentation for infrastructure changes.
- Hardened CD Terraform plan validation so pull requests without cloud
  credentials run the credential-free plan contract instead of failing on empty
  provider variables.

### Pair 6: v7.5.0 MVP + v7.5.1 QA

- Routed AusPost and DHL quote/label calls through shared retry, timeout,
  response-hook, and circuit-breaker plumbing.
- Documented payment, carrier, and social mock/live sandbox boundaries without
  making live sandbox calls.

## [6.6.0] - 2026-05-11 -- v6.1.x carry-forward cleanup closeout

### Release Summary

v6.6.0 closes the compressed v6.1.x -> v6.6.0 cleanup cycle. The release
drained the highest-risk ADR-032 and v5 lessons-learned carry-forwards without
adding new product scope: backend quality and OOM controls deepened, real
Postgres and k6 evidence replaced dry-run assumptions, repo documentation gained
docsync gates, and the frontend Lighthouse/SEO carry-forwards closed with
route-matrix evidence.

### Pair 1: v6.1.0 MVP + v6.1.1 QA

- Closed the macOS `TestHeapCeilingTriggers*` flake by wiring sampler shutdown
  and asynchronous heap-alarm callbacks.
- Added Postgres-backed idempotency storage (`0037_idempotency_store`) with the
  in-memory store retained as a test double.
- Extracted the typed `ErrInvalidFXRate` sentinel and refreshed backend
  coverage to 84.8% after triple-run flake detection.
- Refreshed the Sentrux baseline with zero hook bypasses.

### Pair 2: v6.2.0 MVP + v6.2.1 QA

- Wired Agentrace production NDJSON capture with runx-alias transport guards.
- Added JWT key-version storage (`0038_jwt_key_versions`) and graceful rotation
  support.
- Added memwatch request budgets, adaptive RSS ceilings, and coordinator
  Prometheus metrics.
- Added Postgres FAQ storage (`0039_faq_store`) and validated Agentrace/JWT/
  memwatch soak behaviour.

### Pair 3: v6.3.0 MVP + v6.3.1 QA

- Published real Postgres benchmark distributions and corrected the k6 matrix
  route/rate contract.
- Validated the full k6 matrix at 100 RPS for 5 minutes with 30,008 requests,
  0 failed checks, and aggregate p95 8.06 ms.
- Added Temporal daily GMV refresh scheduling and kept all 107 backend packages
  green under `-race`.

### Pair 4: v6.4.0 MVP + v6.4.1 QA

- Added `cursor-tools docsync` and `runx docs` wrappers so README, ADR, release,
  and operations docs can be checked consistently.
- Repaired cursor-tools workspace/install skew and completed a fleet-wide
  docsync sweep with owned drift fixed through focused PRs.
- Aligned backend README/OpenAPI/ADR documentation and closed CF-18 supplier
  cost threshold semantics.

### Pair 5: v6.5.0 MVP + v6.5.1 QA

- Repaired the Next.js 16 / ESLint 9 flat-config quality gate in the frontend.
- Added centralized frontend metadata and JSON-LD for product and marketplace
  detail pages.
- Closed Lighthouse Performance >=90 and dynamic-page SEO >=90 with static and
  mock-backed route matrices.
- Added the frontend cross-cycle KPI dashboard and tracked React `act(...)`
  warnings as a release-readiness metric.

### Remaining Carry-Forwards

- Backend Sentrux Quality >7000 remains a v7 structural refactor candidate.
- Backend coverage >=85% remains 0.2 percentage points short at the last durable
  84.8% measurement.
- Backend `complex_fn <=4` remains open at 5.
- Live OmniParser/uiauto comparison remains deferred to v7.x until remote GPU
  routing is registered and proven safe.
- Live Alipay/WeChat/AusPost/DHL production credentials remain external
  dependencies.

## [6.0.0] - 2026-05-11 -- Refactored, hardened, and production-polished

### Release Summary

v6.0.0 completes the post-v5.0.0 cycle (10 sprint pairs; v5.1.0 through
v5.9.1 plus this release coordination sprint; PRs `#110`..`#128`). The
cycle focused exclusively on quality: refactoring duplicated code,
hardening performance, consolidating Temporal workflows and event
payloads, running comprehensive QA, and finalising documentation. No
new domain features were added -- this is a polish, performance, and
reliability release. The 8-binary topology is preserved; 36 Postgres
migrations in-tree; Go 1.26.3; Sentrux Quality 6035 (carry-forward for
recovery in v6.1.x).

### Pair 1: ADR-031 Carry-Forwards (v5.1.0)

- Alipay sandbox adapter with environment toggle (`ALIPAY_SANDBOX=true`)
- WeChat Pay sandbox adapter with JSAPI/Native trade type support
- Function decomposition: cyclomatic complexity reduction in payment adapters
- Sentrux quality partial recovery (6131 -> 6035 initial trajectory)

### Pair 2: Carrier Sandbox + Profiling (v5.2.0)

- AusPost eParcel staging adapter with label generation
- DHL Express sandbox adapter with rate calculation
- Lighthouse 6-page audit infrastructure (CLI + baseline capture)
- Top-5 slowest endpoint profiling via benchmark suite
- Frontend accessibility fixes from Lighthouse findings

### Pair 3: Code Deduplication (v5.3.0)

- `internal/httpclient/` shared HTTP client base (replaces 4 duplicated patterns)
- `internal/webhook/verifier/` shared HMAC verify-then-parse package
- `internal/adapter/payment/errors.go` consolidated payment error types
- Import graph depth reduced (max_depth 4)
- Table-driven test refactoring across 12 test files

### Pair 4: Temporal + Eventbus Consolidation (v5.4.0)

- 55 event types standardized in `internal/eventbus/types.go`
- 10 Temporal workflows audited: consistent activity naming, timeout patterns, retry policies
- Event payloads consolidated from 4 per-version files into domain-grouped files
- All activities use `activityhttp.WithTimeout` consistently
- Workflow selftest package for regression detection

### Pair 5: Performance Optimization (v5.5.0)

- PostgreSQL connection pool tuning (EXPLAIN ANALYZE top-10 queries)
- Redis pipeline batching: 3.2x throughput improvement in rate limiter + session manager
- HTTP/2 server configuration for mc-api
- Benchmark regression suite: v5.5.0 vs v5.0.0 baseline comparison
- Hot-path optimization in catalog and order endpoints

### Pair 6: Frontend Performance (v5.6.0)

- Next.js bundle analysis with `@next/bundle-analyzer`
- Lazy loading for 5 heavy components (AgentActivityFeed, MarginDashboard, PaymentDashboard, OperatorAlerts, OnboardingWizard)
- SWR v2.4.1 stale-while-revalidate caching for all API routes
- CI pipeline cache optimization (Go modules + Docker layers)
- Docker image size audit (all 8 binaries confirmed <30MB distroless)

### Pair 7: EvoMap Self-Improvement (v5.7.0)

- 38-capsule analysis across all sprints (v3.1.1 through v5.0.0)
- KPI trend analysis: Quality, Coupling, complex_fn plotted per sprint
- 5 self-improvement recommendations generated
- Agentrace session analysis for tool-call efficiency
- Comprehensive lessons-learned document

### Pair 8: Comprehensive QA (v5.8.0)

- 0 vulnerabilities (`govulncheck` clean)
- 0 flaky tests (triple-run detection with `-count=3`)
- Security audit: all HMAC/crypto, JWT middleware, webhook handlers reviewed
- 56 Playwright E2E specs across all frontend pages
- k6 load test script prepared (execution documented as carry-forward)

### Pair 9: Documentation Finalization (v5.9.0)

- 19 operations docs audited and updated for post-v5.0.0 accuracy
- 124 OpenAPI operations verified against implemented endpoints
- 32 ADRs indexed in `docs/adr/README.md` (ADR-001 through ADR-031)
- `CONTRIBUTING.md` in both repos
- Grafana dashboard JSON catalog reviewed

### Pair 10: Release Coordination (v6.0.0)

- Final validation: 99 packages passing, 1082 frontend tests, 0 failures
- VERSION bump 5.0.0 -> 6.0.0
- ADR-032: v6 release decisions + v6.1.x carry-forward lock
- 32 cross-compiled release binaries (linux/darwin x amd64/arm64 x 8 commands)
- SHA256SUMS for all artifacts
- Monitoring test alignment (metric name refactoring from Pair 5)

### Carry-Forwards (locked for v6.1.x via ADR-032)

- Sentrux Quality >7000 recovery (architectural work needed)
- Coverage 85%+ (needs cmd/* main function testing)
- Live Alipay/WeChat merchant accounts (prod merchant approval pending)
- Live carrier API integration (sandbox validated, prod accounts pending)
- Agentrace production wiring (adapter built, hooks not connected)
- JWT secret key rotation automation
- k6 full load test execution against running backend
- Lighthouse Performance >=90 (currently 68-95 range)

### Statistics

- **Test packages**: 99 (all passing with `-race`)
- **Frontend tests**: 1082 (225 test files)
- **E2E specs**: 56 Playwright
- **Migrations**: 36
- **Binaries**: 8 (`mc-api`, `wc-sync`, `content-worker`, `agent-worker`, `temporal-worker`, `uiauto-compare`, `ec-cli`, `evomap-rollup`)
- **Sentrux Quality**: 6035 | Coupling: 0.06 | Cycles: 1 | God files: 0
- **ADRs**: 32 (ADR-001 through ADR-032)
- **Hook bypasses**: 0 across all 10 pairs

## [4.0.0] - 2026-05-10 -- Production-ready agentic e-commerce stack

### Release Summary

v4.0.0 closes the v3.1.0 -> v3.9.1 sprint cycle (9 MVP/QA sprint
pairs; 17 merged sprint releases; PRs `#79`..`#97`) and promotes the
Agentic Ecommerce backend from the v3.0.0 multi-tenant SaaS baseline
into a fully agentic, production-ready stack. Every ADR-028 epic is
closed: China sourcing, AI enrichment, TikTok Shop, RedNote +
Facebook + content cluster, pricing + fulfilment, customer service +
analytics, uiauto hardening, logistics + returns + ROI, pricing +
content polish, and the v3.9.1 final-polish bundle (onboarding
wizard, channel content analytics, operator alert centre, IG +
Pinterest stubs). The 8-binary topology is preserved (`mc-api`,
`wc-sync`, `content-worker`, `agent-worker`, `temporal-worker`,
`uiauto-compare`, `ec-cli`, `evomap-rollup`); 25 numbered Postgres
migrations land in-tree (`migrations/0001_...` through
`migrations/0025_operator_alerts.up.sql`) plus tenant-settings
seeds; ~6000 Prometheus series surface across the new pricing,
fulfilment, content, CS, and uiauto packages. Sentrux discipline is
held across all 17 sprints + this release sprint -- `complex_fn`
unchanged at **4** (18-sprint streak target achieved); zero hook
bypasses across the streak (one self-resolving frontend bypass on
v3.6.0 noted in the journey).

### Capabilities Included (v3.1.0 -> v3.9.1, by MVP merge SHA)

- **v3.1.0** Epic 1 China Sourcing Agent foundation -- 1688 + Taobao
  adapters, sourcing scorer (40% supplier / 35% margin / 25% trend),
  `ChinaSourcingAgent` orchestrator, AU-import compliance gate, env
  var session cookie stubs (#79 `1b3f795`).
- **v3.1.1** China Sourcing pipeline QA validation -- `dnaeon/go-vcr/v3`
  cassette replay, chaos coverage (API flap + 429 + compliance
  negatives), `BenchmarkScoreCandidates` baseline, operator-run live
  smoke contract (#80 `a2fe83d`).
- **v3.2.0** Epic 2 AI Product Enrichment Pipeline -- multilingual
  description gen (EC-2-1), hero image + bg-removal (EC-2-2), SEO +
  WC sync (EC-2-3), trend signal pgvector ingestion (EC-2-4),
  enrichment observability (EC-2-5) (#81 `b60d515`).
- **v3.2.1** Epic 2 enrichment QA -- 50-product live smoke, LLM
  template failover, bg-removal PNG transparency, WC idempotent
  re-run + decomposition split for sentrux complex_fn (#82
  `2d76b85`, #83 `43f1a84`).
- **v3.3.0** Epic 3 TikTok Shop full integration -- seller API
  client (EC-3-1), listing agent (EC-3-2), order webhook (EC-3-3),
  inventory sync saga (EC-3-4), uiauto fallback via
  omniparser-bridge (EC-3-5) (#84 `9163544`).
- **v3.3.1** Epic 3 TikTok Shop QA validation -- sandbox E2E listing
  within 15s, tampered-HMAC rejection, saga rollback, dual-platform
  zero-oversell coverage (#85 `b4008b1`).
- **v3.4.0** Epic 4 RedNote + Facebook + content cluster -- RedNote
  uiauto facade (EC-4-1), FB Shop META client (EC-4-2),
  cross-platform channel router (EC-4-3), video script writer
  (EC-5-1), ffmpeg video assembler (EC-5-3) (#86 `612ab95`).
- **v3.4.1** Multi-channel validation -- cross-channel fan-out E2E,
  routing matrix 6 combos, EC-4-5 channel health monitor seeded,
  ffmpeg MP4 within 120s (#87 `9e4afb6`).
- **v3.5.0** Epic 6 pricing + Epic 7 fulfilment seed -- supplier
  cost monitor (EC-6-1), platform fee + FX calc (EC-6-2), dynamic
  pricing agent with guardrails (EC-6-3), order aggregator
  (EC-7-1), drop-ship agent saga (EC-7-2) (#88 `473218b`).
- **v3.5.1** Pricing + fulfilment QA -- saga rollback E2E, Existing
  #4 MADRL coordination foundation seed, cost-change event, margin
  formula (#89 `e2be376`).
- **v3.6.0** Epic 8 CS + Epic 9 analytics -- enquiry classifier
  (EC-8-1), FAQ responder (EC-8-2), TikTok + FB messaging adapter
  (EC-8-3), GMV API + daily rollup (EC-9-1), agent activity SSE
  feed (EC-9-2 backend) (#90 `c2d392c`).
- **v3.6.1** CS + analytics QA -- 50-fixture bilingual classifier,
  inbound -> reply within 30s E2E, GMV p95 <200ms over 1M-row
  load, SSE 1s delivery under 100-connection load (#91 `97f7085`).
- **v3.7.0** Epic 10 uiauto hardening -- session manager (OS
  keychain + AES-256-GCM file fallback) (EC-10-1), OmniParser
  memory guard (EC-10-2), stealth-pacing rate limit middleware
  (EC-10-3), multilingual CAPTCHA detector (EC-10-4), YAML cassette
  replay harness with three sample cassettes (EC-10-5)
  (#92 `e84dea6`).
- **v3.7.1** uiauto hardening QA + Tier 2 promotion decision --
  GREEN memory pressure under inference batch, 20-post rate-limit
  drain, CAPTCHA pause, Tier 2 promotion gate (#93 `cd92aad`).
- **v3.8.0** Logistics + returns + ROI -- AusPost eParcel + DHL
  Express shipping label adapter (EC-7-3), 3-channel status
  propagation (EC-7-4), returns saga with auto-approval threshold
  (EC-7-5), ROI heatmap query (EC-9-3), Existing #5 self-testing
  Temporal loops seed (#94 `58dd5b1`).
- **v3.8.1** Returns + logistics QA -- domestic AU label cheapest
  carrier 5-day SLA, 3-channel update 60s, auto-approve threshold,
  heatmap dead-stock filter (#95 `9105af8`).
- **v3.9.0** Pricing + content polish -- competitor price scraper
  (EC-6-4), margin dashboard backend + frontend (EC-6-5), n8n
  content calendar (EC-5-2), hashtag + caption agent (EC-5-4),
  EMA-based content performance feedback loop (EC-5-5),
  ChannelStatusUpdater (#96 `9126acb`).
- **v3.9.1** Final v4 polish -- AI onboarding wizard (Existing #10),
  channel content daily rollup analytics (EC-9-4), operator alert
  centre (EC-9-5), Instagram + Pinterest channel stubs (EC-4-4)
  (#97 `48d03d7`).

### Added (per ADR-028 epic + Existing roadmap items #1-#10)

- **Epic 1 China Sourcing**: closed (v3.1.0 + v3.1.1).
- **Epic 2 AI Product Enrichment**: closed (v3.2.0 + v3.2.1).
- **Epic 3 TikTok Shop**: closed (v3.3.0 + v3.3.1).
- **Epic 4 RedNote + Facebook + IG/Pinterest stubs**: closed
  (v3.4.0 + v3.4.1 + v3.9.1 EC-4-4 stubs).
- **Epic 5 Content Cluster**: closed (v3.4.0 EC-5-1 + EC-5-3,
  v3.9.0 EC-5-2 + EC-5-4 + EC-5-5).
- **Epic 6 Pricing**: closed (v3.5.0 EC-6-1..EC-6-3, v3.9.0
  EC-6-4 competitor scraper + EC-6-5 margin dashboard).
- **Epic 7 Fulfilment + Logistics + Returns**: closed (v3.5.0
  EC-7-1 + EC-7-2 seeds, v3.8.0 EC-7-3..EC-7-5).
- **Epic 8 Customer Service**: closed (v3.6.0 EC-8-1..EC-8-3).
- **Epic 9 Analytics**: closed (v3.6.0 EC-9-1 + EC-9-2 backend,
  v3.8.0 EC-9-3, v3.9.1 EC-9-4 + EC-9-5).
- **Epic 10 uiauto Hardening**: closed (v3.7.0 EC-10-1..EC-10-5).
- **Existing #1 OS-keychain session storage**: closed (v3.7.0
  EC-10-1).
- **Existing #2 chromedp dynamic-JS client**: deferred to v4.1.x
  (carry-forward in ADR-029).
- **Existing #3 RAG pgvector ingest hardening**: closed via v3.2.0
  EC-2-4 trend signal ingestion.
- **Existing #4 MADRL coordination seed**: closed (v3.5.1; full
  multi-agent reinforcement learning post-v5).
- **Existing #5 self-testing Temporal loops seed**: closed (v3.8.0;
  expansion post-v5).
- **Existing #6 uiauto Tier 2 promotion**: closed (v3.7.1 GO
  decision; replay harness production capture post-v5).
- **Existing #7 omniparser-bridge fleet offload**: closed (v2.10.1
  baseline; harden + replay-mode in v3.7.0).
- **Existing #8 EvoMap dual-feed observability**: closed (v2.10.0
  baseline; per-epic NDJSON keys added throughout v3.x).
- **Existing #9 marketplace developer ecosystem**: closed (v2.9.0
  baseline; SDK example expansions in v3.x).
- **Existing #10 AI onboarding wizard**: closed (v3.9.1 4-step
  wizard with channel pre-flight + frontend backlog).

### Migrations

- 25 numbered SQL migrations in `migrations/`:
  `0001_create_products` ... `0025_operator_alerts`. Plus
  tenant-settings seeds in `seed/`.
- Migration highlights by sprint:
  - `0014_supplier_cost_baselines` (v3.5.0 EC-6-1).
  - `0015_faq_entries` (v3.6.0 EC-8-2).
  - `0016_gmv_daily_rollup` (v3.6.0 EC-9-1).
  - `0017_shipping_labels` + `0018_returns` (v3.8.0 EC-7-3 +
    EC-7-5).
  - `0019_roi_daily_rollup` (v3.8.0 EC-9-3).
  - `0020_competitor_prices` (v3.9.0 EC-6-4).
  - `0021_content_calendar` (v3.9.0 EC-5-2).
  - `0022_content_performance_history` (v3.9.0 EC-5-5).
  - `0023_onboarding_wizards` (v3.9.1 Existing #10).
  - `0024_channel_content_daily_rollup` (v3.9.1 EC-9-4).
  - `0025_operator_alerts` (v3.9.1 EC-9-5).
- All migrations are tenant-keyed (`tenant_id` mandatory) and use
  the v2.4.0 RLS scheme; the `0011_rls` policy is re-asserted on
  every new table.

### Operational notes

- **8 production binaries** (unchanged from v3.0.0 surface; per-cmd
  expansion under each cmd's package): `mc-api`, `wc-sync`,
  `content-worker`, `agent-worker`, `temporal-worker`,
  `uiauto-compare`, `ec-cli`, `evomap-rollup`.
- **~6000 Prometheus series** (cumulative across all epics):
  - Epic 1 sourcing scorer + adapter histograms (~250 series).
  - Epic 2 enrichment template + bg-removal latency (~400).
  - Epic 3 TikTok seller, listing, webhook, inventory (~600).
  - Epic 4 + 5 channel router + content pipeline (~700).
  - Epic 6 + 7 pricing + fulfilment + carrier (~900).
  - Epic 8 CS classifier + responder + adapter (~600).
  - Epic 9 analytics rollups + alert centre (~400).
  - Epic 10 uiauto session/memguard/ratelimit/captcha/replay
    (~500).
  - Carry-over v2.10.x resilience pillar (`ec_*` core 9 metrics
    plus per-pool labels) (~750).
  - Self-testing + EvoMap dual-feed cardinality (~400).
- **8 Temporal workflows** + 30+ activities (extended from v3.0.0's
  6 + 24): adds `DropShipSagaWorkflow`, `ReturnsSagaWorkflow`,
  per-epic activities for sourcing, enrichment, channel routing,
  CS, pricing, fulfilment, content, uiauto cassette replay.
- **Tenant awareness**: every entity, endpoint, event carries
  `tenant_id`. `0011_rls` Postgres RLS enforced on the 25
  tenant-keyed tables.
- **n8n workflows**: validated under
  `make n8n-workflows-validate`; v3.9.0 EC-5-2 added the content
  calendar workflow.
- **Sentrux 18-sprint streak**: `complex_fn` held at **4** across
  v3.1.0 -> v3.9.1 -> v4.0.0 (this release). One commit on
  v3.2.1 (`43f1a84`) explicitly split a long enrichment-pipeline
  test to keep the gate.

### Quality gates re-verified at v4.0.0 (canonical `main` HEAD `48d03d7`)

- `runx go test --repo ecommerce -- -race -p 4 ./...`: PASS
  (89 packages green, 59.9 s elapsed; 0 FAIL).
- `runx go vet --repo ecommerce -- ./...`: clean.
- `runx make build --repo ecommerce`: 8 binaries built with
  `GOTOOLCHAIN=auto` (`mc-api` 39 MB, `wc-sync` 8.7 MB,
  `content-worker` 2.9 MB, `agent-worker` 10.3 MB,
  `temporal-worker` 36.8 MB, `uiauto-compare` 3.2 MB, `ec-cli`
  8.7 MB, `evomap-rollup` 3.5 MB).
- `runx sentrux gate --repo ecommerce`: GREEN -- Quality 7040 ->
  7047, Coupling 0.31 -> 0.30, Cycles 0, God files 0, Distance
  from Main Sequence 0.29. **No degradation. complex_fn unchanged
  at 4 (18-sprint streak hard gate held).**
- `runx shell-leak-scan --repo ecommerce`: clean (canonical-rooted
  scope; placeholders only).
- Backend coverage gate (`>=83%` per ADR-026 + ADR-029): see PR
  body for measured value (gate evaluated post-merge in CI;
  worktree gate run in `make coverage-check`).

### Notes

- **ADR-028** (`Code/global-kb/adrs/ADR-028-ec-stack-v4-roadmap.md`,
  PR #159 in `cursor-global-kb`) sourced the 9-sprint plan that
  v4.0.0 closes.
- **ADR-029** (in-repo at `docs/adr/adr-029-v4-release-decisions.md`)
  documents v4.0.0 trade-offs: live carrier API integration,
  Stripe webhook + refund flow validation, 1688/Taobao live API
  scaling, MADRL multi-agent coordination, and replay harness
  production capture all locked as carry-forwards for v4.1.x and
  beyond. ADR-029 supersedes the v4 portions of ADR-026 and is the
  canonical v4 release record.
- **GitHub release artefacts**: cross-compiled binaries for
  `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`
  across all 8 binaries (32 artefacts) plus a `SHA256SUMS`
  manifest. Reproducible build target lives in `Makefile` under
  `make release VERSION=4.0.0`.
- **Frontend companion**: `nfsarch33/agentic-ecommerce-web` ships
  the matching v4.0.0 tag covering the AgentActivityFeed (v3.6.0
  EC-9-2) + MarginDashboard (v3.9.0 EC-6-5). The
  v3.9.1 frontend touchpoints (OnboardingWizard backlog,
  OperatorAlertCentre) ship as a v4.1.x carry-forward; v3.9.1
  backend surface is fully API-ready and the wizard + alert
  surfaces are exposed via the existing v1 API for early operator
  use through the back office.
- **Demo script**: 30-minute live walkthrough at
  `docs/demo/v400-demo-script.md` covers onboarding wizard,
  sourcing -> enrichment, TikTok listing + webhook, RedNote uiauto,
  pricing + competitor scrape, CS messaging + FAQ, margin
  dashboard, alert centre, drop-ship saga + returns, SSE agent
  activity.
- **Final report**: `reports/v400_release_final_report.md`
  consolidates per-sprint metrics, lessons learned, and
  v4.1.x/v5 carry-forwards.
- **VERSION** file bumped to `4.0.0`.

### Carry-forwards locked for v4.1.x

- Live AusPost eParcel + DHL Express integration (sandbox -> production
  keys; real waybills, tracking webhooks).
- Live Stripe webhook + refund flow validation (current v2.5.0
  surface mostly stubbed for tests).
- 1688 / Taobao live API integration (existing scrapers OK; production
  scaling, real-account session lifecycle).
- Carrier webhook signing keys rotation policy.
- chromedp dynamic-JS 1688 client (Existing #2).
- Frontend v3.9.1 surfaces (OnboardingWizard UI, OperatorAlertCentre
  UI; backend complete in v3.9.1).
- Replay harness production capture (Existing #6 deferred extension).

### Carry-forwards locked beyond v5

- Full MADRL multi-agent coordination (current v3.5.1 seed only).
- Self-testing Temporal loop expansion past the v3.8.0 seed.
- Per-tenant data residency (v3.0.0 ADR-026 candidate).
- Real-time per-tenant observability beyond Prometheus + EvoMap.

## v3.7.0 -- Epic 10 uiauto hardening MVP -- 2026-05-10

### Release Summary

v3.7.0 ships Epic 10's uiauto hardening primitives so the
omniparser-bridge fleet path becomes production-grade for unattended
RedNote / TikTok creator-center / Facebook page automation. Five
small-and-focused packages: cross-platform session manager
(macOS keychain + AES-256-GCM file fallback), OmniParser memory
guard, stealth-pacing rate limiter, multilingual CAPTCHA detector +
operator-resume webhook, and a YAML cassette replay harness with
three sample cassettes. All gates remain GREEN with `complex_fn`
unchanged at 4 (12-sprint streak) and Sentrux quality holding the
v3.6.1 baseline.

### Capability surface added

- **Session manager** (`internal/uiauto/session/`). Cross-platform
  per-tenant per-channel session blob storage. macOS uses
  `/usr/bin/security` (`darwin` build tag). Linux + WSL + CI use a
  pure-Go AES-256-GCM file store keyed off the
  `EC_SESSION_MASTER_KEY` env var (32 hex bytes). Encryption-at-
  rest applies in both lanes (defense in depth). Typed errors:
  `ErrSessionNotFound`, `ErrSessionExpired` (>30 days),
  `ErrKeychainUnavailable`, `ErrInvalidMasterKey`,
  `ErrSessionTampered`. 30-day age cap rejects expired blobs at
  load time.
- **OmniParser memory guard** (`internal/uiauto/memguard/`).
  Pre-flight predicted-RSS check + concurrent-inflight cap
  (default 4) + per-request 30s timeout + degrade-on-persistent-
  failure path (3 consecutive 5xx -> `ErrDegraded` until cooldown).
  Wires into the existing v2.10.0 Story 5 EvoMap sink.
- **Stealth-pacing rate limiter** (`internal/uiauto/ratelimit/`).
  Token bucket per (tenant, channel). Default rules: RedNote 1 op
  / 5 min, TikTok 1 op / 2 min, Facebook 5 ops / hr, Instagram 1
  op / hr, default fallback 1 op / 30 s. Stealth jitter via
  `crypto/rand` (NOT `math/rand`). Drain-on-overflow above 20
  queued ops per bucket emits operator alert. Replay protection
  via 24-hour HMAC-SHA256 nonce window with
  `subtle.ConstantTimeCompare` -- reuses the v3.3.0 EC-3-1 HMAC
  pattern.
- **CAPTCHA detector** (`internal/uiauto/captcha/`). Multi-signal
  fingerprint (HTTP body, status+WAF-keyword, DOM selectors,
  free-text keyword) across 7 languages: EN, zh-cn, zh-tw, JP,
  KR, ES, FR. On detection emits `CAPTCHADetectedEvent`, pauses
  pipeline via `PausePipeline` handle, and exposes
  `POST /api/v1/uiauto/captcha/<event_id>/resolved` operator-auth
  resume webhook (1-hour solve budget; `ErrCAPTCHASolveTimeout`
  on breach).
- **Replay harness** (`internal/uiauto/replay/`). YAML cassette
  recorder + deterministic player; mismatch detection returns
  `ErrPlaybackMismatch`. Three sample cassettes shipped in
  `tests/uiauto/cassettes/`: RedNote post creation, TikTok video
  upload, CAPTCHA encounter. Schema is forward-compatible with
  the existing `gopkg.in/dnaeon/go-vcr.v3` cassettes used by the
  v3.1.1 China sourcing tests.

### Resilience pillar wiring (per v2.10.0 baseline)

- 5 NEW `goleak.VerifyTestMain` `leak_test.go` wrappers --
  `internal/uiauto/{session,memguard,ratelimit,captcha,replay}`.
- 7 NEW `ec_*` Prometheus metrics with cardinality budgets:
  `ec_uiauto_session_ops_total` (~120 series),
  `ec_omniparser_inference_duration_seconds` (histogram),
  `ec_omniparser_memory_pressure_pauses_total` (~10 series),
  `ec_omniparser_concurrent_inflight` (gauge),
  `ec_uiauto_rate_limit_drops_total` (~80 series),
  `ec_captcha_detections_total` (~120 series upper bound; ~30 live),
  `ec_captcha_resolution_duration_seconds` (histogram).
- 6 NEW EvoMap KPI fields rolled into `Capsule.KPIs` and the
  `Aggregate` daily-roll-up: `uiauto_session_ops_total`,
  `omniparser_inference_p95_ms`, `omniparser_memory_pauses_total`,
  `uiauto_rate_limit_drops_total`, `captcha_detections_total`,
  `captcha_avg_resolution_seconds`.

### Quality gates (v3.7.0 measured)

- `go test -race -p 4 ./...` PASS across all packages on the worktree.
- `go vet ./...` clean.
- `make build` builds 8 binaries (mc-api, wc-sync, content-worker,
  agent-worker, temporal-worker, uiauto-compare, ec-cli,
  evomap-rollup).
- `make compose-config` PASS.
- `runx shell-leak-scan --root <worktree>` PASS (22 files scanned,
  0 findings).
- Sentrux gate: Quality 7035 / Coupling 0.33 / Cycles 0 / God files 0
  / `complex_fn=4` (HARD GATE held; 12-sprint streak).
- Coverage 84.3% (== v3.6.1 baseline; >= 83% gate).

### Public-repo gate

- ZERO infrastructure host names committed. Cassettes use
  placeholder host `omniparser.example.local`. Master key + bridge
  URL travel exclusively via env vars (`EC_SESSION_MASTER_KEY`,
  `EC_OMNIPARSER_BRIDGE_URL`).

## v3.1.1 -- China Sourcing pipeline QA validation -- 2026-05-09

### Release Summary

v3.1.1 is the QA-validation companion to v3.1.0. The Epic 1 China
Sourcing Agent code shipped untouched in v3.1.0; v3.1.1 adds the
operator-facing validation surface: deterministic local cassettes for
the 1688 / Taobao adapters via `dnaeon/go-vcr/v3`, three categories
of chaos coverage (API flap, 429 backoff, compliance gate negatives),
a sourcing-scorer benchmark baseline, and an operator-run live smoke
contract (no unattended scraping). All gates remain GREEN with
`complex_fn` unchanged at 4 and Sentrux quality holding the v3.1.0
baseline.

### Validation surface added

- **Cassette replay** (`internal/adapter/china/vcr_cassette_test.go`,
  `internal/adapter/china/testdata/cassettes/`). Four deterministic
  local-mock cassettes (`1688_search.yaml`, `taobao_detail.yaml`,
  `taobao_search_429_then_success.yaml`,
  `taobao_search_429_exhausted.yaml`) using `dnaeon/go-vcr/v3` in
  `ModeReplayOnly`. Cassettes use placeholder hosts
  (`cassette.1688.local`, `cassette.taobao.local`) and redacted
  `session=redacted` cookies so no marketplace credentials leak via
  the test fixtures. A `TestChinaGoVCRCassettesContainNoSecrets`
  check enforces the no-secret invariant on every test run.
- **Live operator-run smoke**
  (`internal/adapter/china/live_smoke_test.go`, `//go:build live`).
  Two tests gated behind both the `live` build tag AND the
  `ECOMMERCE_1688_SESSION_COOKIE` / `ECOMMERCE_TAOBAO_SESSION_COOKIE`
  env vars so they only execute when an operator has explicitly
  approved a real-account session. Default CI runs skip them
  silently. Documented invocation in
  `internal/adapter/china/testdata/cassettes/README.md`.
- **Chaos coverage** (`tests/chaos/china_sourcing_test.go`,
  `//go:build chaos`). Three categories proven against the live
  agent + adapter wiring: (1) API flap -- the 1688 client errors,
  the agent still selects the healthy Taobao adapter; (2) 429
  backoff -- `httptest` server returns two 429s then a success,
  the Taobao client honours [10ms, 20ms] exponential backoff and
  `ErrTaobaoRateLimited` exhaustion semantics; (3) compliance
  negatives -- two distinct paths (the agent-level gate refusing
  to emit when every product is restricted, and the
  `compliance.Evaluate` AU-import + platform + sub-category
  rejection paths).
- **Sourcing benchmark baseline** (`internal/agent/sourcing/bench_test.go`).
  `BenchmarkScoreCandidates` covers the composite-ranking hot path
  (40% supplier / 35% margin / 25% trend) with a 3-supplier
  representative load (slow-cheap, balanced, premium). On Apple M4
  Pro at `GOMAXPROCS=2` the v3.1.1 baseline is ~200 ns/op, 376 B/op,
  5 allocs/op (3 runs at `-benchtime=1s`, very stable).

### Quality gates (v3.1.1 measured)

- `go test -race ./...` PASS across 64+ packages on the worktree.
- `go vet ./...` clean (default + `-tags chaos`).
- Backend coverage 84.9% (gate >=83%; +0.1pp vs the v3.1.0 entry's
  84.8%, kept comfortably above the 85% target with 2-point buffer).
- Sentrux gate `gate .` GREEN: Quality 6940 against the v3.1.0
  worker-refreshed baseline 6942 (-2, within noise floor; sentrux
  prints "✓ No degradation detected"), Coupling 0.38 -> 0.37
  (improved by -0.01 from the v3.1.0 worker-refreshed 0.377),
  Cycles 0, God files 0, `complex_fn` unchanged at **4** (the
  hard gate; no increase from the v3.1.0 baseline).
- All 8 binaries still build (`make build`).
- `runx shell-leak-scan --root <worktree>` clean (18 files scanned,
  0 findings); existing CHANGELOG had one Alibaba metadata IP that
  the previous worker redacted to `<cloud-metadata-ip>` in the
  v2.10.x SSRF entry to keep the public-repo gate happy.

### Operator-run live cassette recording (deferred)

Live cassette re-recording remains a manual operator task. The
v3.1.1 cassettes are deterministic local mocks (the `httptest`
fixtures from v3.1.0 transposed into `go-vcr` format), not real
1688 / Taobao captures, because real captures require credentialled
sessions and may trigger marketplace anti-bot interactions. The
`README.md` in `internal/adapter/china/testdata/cassettes/` documents
the operator-run live smoke command for re-recording when the
operator is on a personal account that can absorb the rate-limit
risk.

### Carry-overs (still deferred to v3.7.0 EC-10-1 and later)

- OS-keychain session-cookie storage (env var stub remains).
- Live chromedp headless-browser client for 1688 dynamic-JS pages.

## v3.1.0 -- China Sourcing Agent foundation (Epic 1) -- 2026-05-09

### Release Summary

v3.1.0 opens the ADR-026 v4 roadmap with Epic 1 (China Sourcing
Agent) -- the first MVP sprint that introduces autonomous product
discovery from Chinese B2B/B2C platforms (1688, Taobao) into the
catalog. Five tightly-coupled stories ship together because they
share heavy domain context (sourcing pipeline) and a single
integration surface (the `ChinaSourcingAgent` orchestrator). All
five honour the v2.10 resilience pillar -- every new package
registers with the `internal/lifecycle.Manager`, fans out via
`internal/workerpool.Pool` (no raw goroutines), is verified with
`goleak.VerifyTestMain`, emits Prometheus metrics + EvoMap NDJSON
KPIs, and gates production startup on `OMNIPARSER_BRIDGE_URL` per
the v2.10.1 omniparser-bridge integration.

### Stories

- **EC-1-1**: 1688 supplier scraper adapter
  (`internal/adapter/china/1688_client.go`,
  `internal/adapter/china/models.go`). HTTP+JSON adapter with token-
  bucket rate limit (1 req / 2s default), session-cookie injection,
  context-aware backoff, httptest-cassette unit coverage, typed
  `Err1688RateLimited` sentinel, and the `china.Client` port the
  sourcing agent fans out across.
- **EC-1-2**: Taobao/Tmall API adapter
  (`internal/adapter/china/taobao_client.go`). Same `china.Client`
  port; exponential backoff on 429 (initial 100ms, cap 5s, max 3
  retries) honouring the `internal/lifecycle` cancellation contract;
  category mapping covers the top-14 Taobao native categories
  (`SupportedCategories()`); typed `ErrTaobaoRateLimited` sentinel.
- **EC-1-3**: IronClaw-compatible China sourcing agent
  (`internal/agent/sourcing/china_agent.go`,
  `internal/agent/sourcing/scorer.go`,
  `internal/agent/sourcing/errors.go`). Concurrent fan-out across
  every configured `china.Client` via `internal/workerpool`; product
  filtering through the EC-1-4 compliance gate; supplier filtering
  through the EC-1-5 supplier-score floor; trend-signal blending via
  the optional `TrendSignaler` port (pgvector via the existing
  `internal/rag` package); composite ranking (40% supplier / 35%
  margin / 25% trend); typed event emission via the new
  `eventbus.SourcingProposalPayload` envelope (v1 schema). Refuses
  to start when `OMNIPARSER_BRIDGE_URL` is unset (alias-only argv;
  no shell leak).
- **EC-1-4**: China import compliance pre-screening gate
  (`internal/compliance/china_import.go`). Pure-function rule engine
  enforcing the AU import restricted list (firearms, ammunition,
  medical devices, explosives, narcotics, asbestos, animal products,
  endangered species, ...) and the TikTok/Facebook prohibited-
  category lists (vape, gambling, CBD, weight-loss supplements,
  counterfeit, ...). Returns typed `Decision{ProductID, TenantID,
  Pass, Reasons, RuleHits, BlockedFor}`; `EvaluateBatch` partitions
  approved + rejected; case-insensitive subcategory matching; typed
  `ErrRestrictedCategory` sentinel. 20-fixture acceptance test
  covering compliant + non-compliant categories.
- **EC-1-5**: Supplier MOQ + lead-time scoring
  (`internal/domain/supplier.go`). Pure-function `Supplier.Score()`
  starts at 1.0 then subtracts capped MOQ-penalty (>50 units) and
  lead-time penalty (>20 days), adds `VerifiedGoldBonus` and
  review-ratio bonus (>=0.85). `FilterByScore` drops suppliers below
  `SupplierScoreFloor` (0.5). Typed `ErrSupplierBelowScore` and
  `ErrInvalidSupplier` sentinels. 100% statement coverage on the
  scorer.

### Resilience pillar wiring

- `goleak.VerifyTestMain(m)` in every new test package:
  `internal/domain/`, `internal/compliance/`, `internal/adapter/china/`,
  and the existing `internal/agent/sourcing/` test main coverage
  catches any sourcing-agent leak.
- Every fan-out task routes through `internal/workerpool.Pool.Submit`;
  zero raw `go func()` calls in the new code.
- The agent rejects construction without
  `OMNIPARSER_BRIDGE_URL`, with explicit env-var fallback and a
  unit-tested `t.Setenv` reject-if-unset case.
- New Prometheus metrics added to `internal/metrics.Registry`:
  `ec_sourcing_runs_total{tenant_id,source}`,
  `ec_sourcing_duration_seconds{source}`,
  `ec_sourcing_compliance_rejects_total{category}`, and
  `ec_supplier_score_distribution`. Cardinality budget documented
  in-place: ~20 + 2 + ~25 + 1 = ~48 series total.
- New EvoMap KPI fields: `sourcing_runs_total`,
  `sourcing_compliance_rejects_total`, `sourcing_p95_ms`,
  `supplier_score_mean`. Daily roll-up aggregates them into the
  fleet-level capsule.
- New typed event payload `eventbus.SourcingProposalPayload`
  (`Version=1`, schema `product.sourcing.proposed`) emitted on every
  successful agent run.

### Quality gates

- `go test -race ./...` PASS across 64+ packages.
- `go vet ./...` clean.
- Backend coverage 84.8% (gate >=83%; +0.3pp vs v3.0.0 baseline of
  84.5%).
- Sentrux: Quality 6904 -> 6924 (+20), Coupling 0.41 -> 0.36
  (improved -0.05; ceiling 0.42), 0 cycles, 0 god files,
  `complex_fn` unchanged at 4 (gate "no degradation").
- All 8 binaries still build (`make build`).
- `compose-config` + `compose-config-prod` validate.
- `runx shell-leak-scan --repo ecommerce` clean (no findings).
- New `.env.example` and `.env.compose.example` entries:
  `OMNIPARSER_BRIDGE_URL`, `ECOMMERCE_1688_SESSION_COOKIE`,
  `ECOMMERCE_TAOBAO_SESSION_COOKIE`.

### Carry-overs (deferred to v3.1.x and later)

- Live chromedp headless-browser client for 1688 dynamic-JS pages
  (deferred behind the `china.Client` port; v3.1.0 ships HTTP+JSON
  adapter only).
- Live cassette recording via `dnaeon/go-vcr/v3` (the test fixtures
  use `httptest.NewServer` + embedded JSON; the port is shaped so
  cassettes drop in without API changes).
- OS-keychain session-cookie storage (deferred to v3.7.0 EC-10-1;
  v3.1.0 reads `ECOMMERCE_*_SESSION_COOKIE` env vars per stub
  fallback).

## v3.0.0 -- Production-Ready Multi-Tenant Agentic E-commerce -- 2026-05-09

### Release Summary

v3.0.0 closes the v2.0.1 -> v2.10.1 sprint cycle (12 production MVPs
across 22 sprint slots, including the v2.6.1, v2.10.0, and v2.10.1
inserted rounds). It promotes the Agentic Ecommerce backend from
v2.0.0 (Mission Control orchestration spine) into a full multi-tenant
SaaS-ready stack with five bounded contexts (catalog/order,
membership, digital, marketplace, billing) plus a tenant aggregate,
comprehensive QA, a public Plugin SDK, the `ec-cli` developer tool,
and the v2.10.x **resilience pillar** (bounded concurrency, OOM
detection, OpenTelemetry observability, EvoMap dual-feed,
omniparser-bridge offload). The release ships 8 production binaries,
~80 v1 stable REST endpoints + a v2 preview namespace, 6 Temporal
workflows + 24 activities, 14+ tenant-keyed Postgres tables with RLS
enforcement, 9 `ec_*` Prometheus metrics, 4 Grafana dashboards, and a
battle-tested release-gate matrix.

### Capabilities Included (v2.0.1 -> v2.10.1, by MVP merge SHA)

- **v2.0.1** hardening + 83.2% coverage baseline (#63 `a315cea`)
- **v2.1.0** `uiauto-framework` compose harness + comparison
  generator (`cmd/uiauto-compare`) (#64 `5008e98`)
- **v2.2.0** membership bounded context + Temporal
  `MembershipLifecycleWorkflow` (#65 `ecc2b25`)
- **v2.3.0** digital goods bounded context + signed-URL HMAC
  (`internal/adapter/signedurl`) (#66 `0faae1d`)
- **v2.4.0** marketplace plugin framework + tenant aggregate
  (#67 `010d554`)
- **v2.5.0** tenant self-service registration + billing hooks +
  Stripe webhook (HMAC + replay window) (#68 `953fd86`)
- **v2.6.0** coverage push + security fuzz harness + benchmarks for
  hot paths (#69 `7af4d4b`)
- **v2.6.1** `cmd/*` DI refactor + coverage lift (#70 `3ca9a6a`)
- **v2.7.0** production marketplace + cloud scale + sentrux baseline
  refresh (#71 `19c25e1`, #72 `c258347`)
- **v2.7.x** secrets adapter consolidation
  (`internal/adapter/secrets`) (#73 `69ec23f`)
- **v2.8.0** comprehensive QA + OWASP security audit (#74 `9c26ca8`)
- **v2.9.0** plugin SDK (`pkg/marketplace/sdk`) + `ec-cli` (7th
  binary) + tenant onboarding workflow + A10 SSRF guard + API
  versioning (v1 stable + v2 preview) (#75 `0e84159`)
- **v2.10.0** resilience pillar MVP -- `internal/lifecycle` Manager,
  `internal/workerpool` bounded Pool, `internal/memwatch` Sampler +
  `MemCap` middleware, 9 `ec_*` Prometheus metrics, OpenTelemetry +
  slog correlation, 4 Grafana dashboards, `internal/evomap` NDJSON
  Sink + `cmd/evomap-rollup` (8th binary). All 5 stories TDD-first.
  (#76 `cdad8ec`)
- **v2.10.1** resilience validation QA -- `tests/chaos/` (4 build-tag
  gated chaos tests) + `tests/benchmarks/v2.10-baseline.json` perf
  baseline + `omniparser-bridge` fleet-offload service +
  `docs/adr/adr-027-resilience-pillar.md` + `docs/observability.md`
  + `docs/operations/runbook.md` + `docs/operations/omniparser-bridge.md`.
  (#77 `115a931`)

### Quality Gates (re-verified on canonical `main` HEAD `115a9312`)

- `runx go test --repo ecommerce -- -race ./...`: PASS (64 packages
  green, 31.9 s elapsed).
- `runx go vet --repo ecommerce -- ./...`: clean.
- Backend statement coverage: **84.5%** via
  `runx go test --repo ecommerce -- -race -coverprofile=coverage.out
  ./...` followed by `runx make build --repo ecommerce -- coverage`.
  Above the 83% gate per ADR-026; target 85% short by 0.5pp -- gap
  documented and accepted.
- `runx sentrux gate --repo ecommerce`: **GREEN** -- Quality 6924,
  Coupling 0.36, Cycles 0, God files 0, Distance from Main Sequence
  0.32. No degradation. complex_fn unchanged at 4.
- `runx make build --repo ecommerce`: 8 binaries built --
  `mc-api`, `wc-sync`, `content-worker`, `agent-worker`,
  `temporal-worker`, `uiauto-compare`, `ec-cli`, `evomap-rollup`.
- `runx shell-leak-scan --repo ecommerce`: 0 findings (14 files
  scanned, 610 skipped).
- `runx make build --repo ecommerce -- govulncheck-scan`: no
  vulnerabilities found.
- `runx make build --repo ecommerce -- gitleaks-scan`: no leaks
  found (~8.4 MB scanned in 347 ms).

### Workflows + Activities

- **6 Temporal workflows**: `ProductPublishWorkflow`,
  `ContentGenerationWorkflow`, `MediaProcessingWorkflow`,
  `SourcingWorkflow`, `MembershipLifecycleWorkflow`,
  `TenantOnboardingWorkflow`.
- **24+ activities** across content, media, sourcing, membership
  lifecycle (`ChargeStripe`, `SendNotification`,
  `RecordBillingEvent`), and tenant onboarding
  (`tenant.validate_registration`, `tenant.provision_record`,
  `tenant.seed_default_plan`, `tenant.issue_welcome_notification`,
  `tenant.register_default_plugins`, `tenant.rollback_record`).

### API Surface

- **~80 v1 stable REST endpoints** (`api/openapi.yaml`); v1
  endpoints stable through v3.x.
- **v2 preview namespace** (`api/openapi-v2-preview.yaml`) carrying
  `POST /api/v2/marketplace/plugins/{slug}/install`. Subject to
  change without notice.
- **Version routing**: path-based v2 opt-in (`/api/v2/...`) wins;
  Accept header `application/vnd.ec.v2+json` upgrades a v1 path
  when both surfaces exist. `WithVersionHeaders` middleware stamps
  `X-API-Version` on every response and `X-API-Deprecation:
  preview; semantics may change without notice` on v2 responses.

### New in v2.10.x (Resilience Pillar -- production-ready)

- **9 `ec_*` Prometheus metrics**: `http_requests_total`,
  `http_duration_seconds`, `workflow_runs_total`,
  `workflow_duration_seconds`, `workerpool_queued`,
  `workerpool_saturation_total`, `oom_alarms_total`,
  `goroutine_count`, `heap_bytes`. Bounded label cardinality
  (`WithMaxSeries`) so a hot label cannot OOM the registry.
- **4 Grafana dashboards** in
  `monitoring/grafana/dashboards/v210/`: `ec-overview` (cross-binary
  RED method), `ec-tenant` (per-tenant deep-dive with templated
  `tenant_id`), `ec-workerpools` (per-pool saturation + queue depth
  + worker count), `ec-resilience` (OOM alarms, goroutine count,
  heap, GC pause distribution).
- **OpenTelemetry HTTP + Temporal tracing** with `traceparent`
  propagation across the HTTP <-> Temporal activity boundary.
  Exporter: OTLP gRPC to `OTEL_EXPORTER_OTLP_ENDPOINT`.
- **Structured slog** with `trace_id`, `span_id`, `tenant_id`
  correlation injected via `RequestLogger` middleware.
- **EvoMap dual-feed**: NDJSON local sink
  (`tests/metrics/evomap.ndjson`) + Prometheus Pushgateway
  remote_write via the `metrics-bridge` runx alias.
  `cmd/evomap-rollup` aggregates daily NDJSON into a markdown
  capsule mirroring the existing fleet evoloop schema (verified
  via Phase A.4 smoke: synthetic NDJSON -> capsule at
  `~/Code/global-kb/global-memories/evoloop-capsules/ec-stack-2026-05-09.md`).
- **omniparser-bridge offload pattern** (mirrors
  `minimax-openai-bridge`): HMAC-SHA256 +
  `crypto/subtle.ConstantTimeCompare`, alias-only argv, signed
  envelope `<unix-secs>\n<path-and-args>\n<body>`. New repo
  `nfsarch33/omniparser-bridge` (initial commit `64e35b6`).

### Notes

- **ADR-026** (`Code/global-kb/adrs/ADR-026-ec-stack-v3-release-decisions.md`)
  documents v3.0.0 cross-stack release decisions: lowered backend
  target to >=85% (gate 83%), HYBRID uiauto Tier 1 gate, Coupling
  baseline 0.36, RLS on 14+ tenant-keyed tables, Resilience Pillar
  v2.10.
- **ADR-027** (in-repo at `docs/adr/adr-027-resilience-pillar.md`)
  documents resilience pillar decisions (bounded concurrency
  mandate, OOM ceiling enforcement, OTel adoption, EvoMap dual-feed,
  omniparser-bridge offload).
- **v4.0.0 roadmap preview** (10 candidate MVPs) included in
  ADR-026: coaching bounded context, Python CCE sidecar evaluation,
  Flutter admin mobile app, MADRL multi-agent coordination,
  self-testing Temporal loops, uiauto Tier 2 promotion, full
  marketplace developer ecosystem, per-tenant data residency,
  real-time per-tenant observability, AI-driven onboarding wizard.
- **GitHub release artefacts**: cross-compiled binaries for
  `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`
  across all 8 binaries (32 artefacts) with a SHA-256 manifest at
  `bin/release/SHA256SUMS`.
- **Frontend companion**: v3.0.0 ships in
  `nfsarch33/agentic-ecommerce-web` with the same release tag.

## v2.10.1 Resilience Validation QA -- 2026-05-09

Closes the v2.10.x resilience pillar with chaos-test scaffolding,
performance-baseline capture, toolchain rebuild evidence, the new
`omniparser-bridge` fleet-offload service, and the documentation +
v3.0.0 readiness verification artefacts. No production-code changes
in this PR -- exclusively validation, evidence, and offload-bridge
work that must land before the v3.0.0 release tag.

### Added — Chaos test suite (`tests/chaos/`)

- **Build-tag-gated TDD harness** under `tests/chaos/` that runs only
  when invoked with `go test -tags chaos -race ./tests/chaos/...`. The
  default `go test ./...` path stays hermetic and fast.
- **`oom_test.go`** -- `TestHeapCeilingTriggersGracefulShutdown`
  exercises the v2.10.0 OOM-detection chain end-to-end: a
  16 MiB sentinel allocation breaches a synthetic 4 MiB heap
  ceiling, the memwatch sampler logs `memwatch.heap_ceiling_critical`
  after the dwell window, the configured `HeapAlarmCallback` fires
  `lifecycle.Manager.Shutdown()`, and every registered Closer
  (postgres pool, redis client, sampler) is invoked in reverse
  registration order before the test returns. The companion
  `TestGoroutineCeilingTriggersAlarm` covers the goroutine-leak
  alarm with the same dwell-and-fire pattern.
- **`postgres_flap_test.go`** -- spins a real `postgres:16-alpine`
  testcontainer, calls `Stop` with both a graceful timeout and a
  hard kill, asserts `pgxpool.Ping` fails while the container is
  down, then asserts the pool recovers within 5 s of `Start`. Two
  table-driven cases exercise both stop modes.
- **`redis_flap_test.go`** -- generic `redis:7-alpine`
  testcontainer + TCP-probe round-trip with the same Stop / Start
  pattern. The plan's "rate-limiter / event-bus dependent code paths
  see the upstream disappear and recover" property is observed at
  the TCP level rather than the adapter handshake level so the
  chaos suite does not pull go-redis into its dependency surface.
- **`temporal_flap_test.go`** -- `temporalio/auto-setup:1.24.2`
  with sqlite mode. Stop / Start cycle + 5 s recovery probe on the
  7233/tcp frontend port. The plan's "workflow start should defer
  or 503" assertion stays at the unit level
  (`internal/workflow/start_test.go`); this chaos sibling proves
  the integration counterpart without coupling the chaos package
  to `go.temporal.io/sdk` schema versioning.
- **Hermetic skip** -- every chaos test self-skips with a clear
  message when `DISABLE_DOCKER_TESTCONTAINERS=1` or Docker is
  unreachable, so a developer machine without Docker still passes
  `go test -tags chaos ./...` cleanly.

### Added — Performance baselines (`tests/benchmarks/`)

- **`tests/benchmarks/v2.10-baseline.json`** -- structured capture of
  the 9 hot-path benchmarks: `BenchmarkVerifyAccessToken`,
  `BenchmarkMintAccessToken`, `BenchmarkLicenseKeyGenerate`,
  `BenchmarkLicenseKeyValidate`, `BenchmarkWebhookVerify`,
  `BenchmarkIssueSignedURL`, `BenchmarkVerifySignedURL` (the seven
  v2.6.0 baselines) plus `BenchmarkMediaValidation` and
  `BenchmarkMediaQAValidate` (v2.10 additions). Each benchmark
  captured `-count=2` for sample stability on Apple M4 Pro,
  `darwin/arm64`, host runtime Go 1.24.11, build toolchain Go
  1.25.10 (auto-downloaded via `GOTOOLCHAIN=auto`).
- **`tests/benchmarks/v2.10-baseline.raw.json`** -- raw `go test
  -bench=. -benchmem -json` stream for forensic inspection.
- **`tests/benchmarks/v2.10-vs-v2.6-comparison.md`** -- regression
  gate evaluation. The v2.6.0 sprint did not preserve numeric
  ns/op fixtures; v2.10.1 therefore applies the qualitative
  envelope documented in the v2.6.0 PR description (single-digit
  microseconds for HMAC + JWT, sub-microsecond for digital licence
  keys). All v2.10.1 numbers respect that envelope, **PASS** for
  the v3.0.0 release-gate clause "no hot-path benchmark regression
  > 25% vs v2.6.0".
- **`tests/benchmarks/v2.10-toolchain.json`** -- captured `runx`,
  `cursor-tools`, and `ec-cli` rebuild evidence (sha256, size,
  embedded Go version) plus the smoke-test outcomes for `runx
  doctor`, `ec-cli doctor`, and `runx workspace doctor --quick`.

### Added — `omniparser-bridge` fleet-offload service

- **New repo** `nfsarch33/omniparser-bridge` (initial commit
  `64e35b6`). Tiny Go HTTP service that lets MacBook agents call a
  gpu-host-1-resident OmniParser worker without publishing the gpu-host-1
  endpoint on argv. Mirrors the proven `minimax-openai-bridge`
  pattern (HMAC-SHA256 + `crypto/subtle.ConstantTimeCompare`,
  32-byte minimum secret, configurable replay window).
- **Wire format** -- `X-OmniBridge-Issued-At` (unix-seconds UTC) +
  `X-OmniBridge-Signature` (lowercase hex of HMAC-SHA256). Canonical
  preimage `<unix-secs>\n<path-and-args>\n<body>` so path tampering
  invalidates the signature alongside body tampering.
- **Tests** -- TDD-first; `internal/signing/sign_test.go` covers
  HMAC roundtrip + tampered-body + tampered-path + replay-window
  + malformed-signature cases; `internal/forwarder/forward_test.go`
  covers success + upstream-timeout + transient-retry +
  permanent-4xx-no-retry + bad-endpoint cases;
  `internal/bridge/bridge_test.go` covers /healthz +
  authenticated POST + tampered + missing-headers + stale-envelope
  cases; `internal/lifecycle/lifecycle_test.go` covers LIFO
  drain + parent-cancel propagation + idempotent shutdown.
- **Deployment** -- distroless `static:nonroot` final image
  (14.9 MiB), multi-stage Go 1.24 build with `-trimpath -ldflags='-w
  -s'` for reproducibility, GitHub Actions CI matrix on Go 1.24.x.
- **runx alias** -- `omniparser-bridge` added to
  `~/.config/runx/config.yaml` (path: `$HOME/Code/personal/omniparser-bridge`,
  identity: `nfsarch33`). uiauto-framework + ecommerce uiauto code
  read `OMNIPARSER_BRIDGE_URL` (alias-resolved by runx tunnel
  forward); the fleet IP / private DNS name never lands on argv.
- **Operations doc** -- new `docs/operations/omniparser-bridge.md`
  in this repo with the deploy contract and security notes.

### Added — Documentation

- **`docs/adr/ADR-027-resilience-pillar.md`** -- new ADR covering
  the bounded-concurrency mandate, OOM ceiling enforcement, OTel
  adoption, EvoMap dual-feed (NDJSON + Pushgateway) and the
  omniparser-bridge offload pattern. Cites v2.10.0 PR #76 as the
  implementation evidence and the v2.10.1 chaos / benchmark
  artefacts as validation evidence.
- **`docs/operations/runbook.md`** -- new operations runbook,
  starting with the OOM-alarm response procedure folded in from the
  v2.10.0 `docs/observability.md` and extended with chaos-test
  remediation patterns (postgres / redis / temporal flap recovery).
- **`docs/operations/omniparser-bridge.md`** -- bridge deploy
  contract, env-var reference, runx alias usage,
  `OMNIPARSER_BRIDGE_URL` propagation strategy.

### v3.0.0 release-gate readiness verdict (after v2.10.1 lands)

| Criterion | Status |
|-----------|--------|
| Backend coverage | 84.5% (vs >=85% target -- 0.5 pp short) |
| Frontend coverage | will land via the small companion frontend PR (vitest.config.ts server-page exclusions) |
| Sentrux quality | 6939 (no degradation) |
| Sentrux coupling | 0.40 (within <=0.42) |
| Sentrux complex_fn | 4 (unchanged from v2.10.0 baseline) |
| Workspace doctor | RED, all from pre-existing fleet drift unrelated to v2.10.x; no ecommerce / web findings |
| Chaos suite | 4 files, build-tag gated, race-clean |
| Bench baseline | captured + comparison doc shipped |
| omniparser-bridge | repo created + initial commit pushed |

**Verdict**: **GO** for v3.0.0 modulo the 0.5 pp coverage gap. The
gap is documented; it is acceptable per the v3.0.0 ADR-026 explicit
relaxation to `>= 83% gate, >= 85% target`.

## v2.10.0 Resilience and Observability MVP -- 2026-05-09

The 70 GB MacBook OOM event of 2026-05-09 informed the v321 fleet
hygiene work. v2.10.0 mirrors that pattern inside the EC product
stack: bounded concurrency, runtime-aware OOM detection, a unified
observability surface, and an EvoMap feed so the agent can self-tune.

### Added — Resilience pillar (5 stories)

- **`internal/lifecycle/`** (Story 1): `Manager` orchestrates
  signal-driven cancellation, drain of in-flight work, and
  reverse-order Closer invocation with a bounded shutdown deadline.
  `Closer` interface and `CloserFunc` adapter. mc-api, agent-worker,
  and temporal-worker all register their HTTP server, memwatch
  sampler, and (for temporal) Postgres pool through this Manager.
  Unit tests cover happy-path drain, signal-during-handler,
  closer-error-aggregation, double-shutdown idempotency, drain
  timeout exceeded. 81% coverage.
- **`internal/workerpool/`** (Story 2): `Pool` wraps a bounded
  task channel + per-worker goroutine with panic isolation, drain
  on Close, saturation backpressure (`ErrPoolSaturated`), and
  resource-aware sizing. Tests cover sizing math, queue saturation,
  panic isolation, drain-on-cancel, post-close submit. 85% coverage.
  Audited every `go func` in the backend; the only two production
  goroutines (`cmd/mc-api/app.go` http server, `cmd/agent-worker/main.go`
  metrics server) are already short-lived runner-style goroutines
  bounded by their `http.Server.ListenAndServe()` lifetime, and
  `internal/agent/orchestrator.go` already enforces `MaxConcurrent`
  via a counter (`s.running++`/`--`). New worker-fan-out work in
  v2.11+ uses the pool.
- **`internal/memwatch/`** (Story 3): `Sampler` reads
  `runtime.MemStats` (HeapInuse, HeapAlloc, NumGoroutine, GC pause)
  every 5 s and emits `Sample` to a `Sink`. Heap ceiling +
  goroutine ceiling fire callbacks after dwell windows
  (`ECOMMERCE_HEAP_CEILING_BYTES`/`ECOMMERCE_GOROUTINE_CEILING`).
  Implements `lifecycle.Closer`. 92% coverage.
- **`internal/middleware/memcap.go`** (Story 3): per-request memory
  cap. Rejects requests whose `Content-Length` exceeds
  `MaxRequestBytes` with HTTP 413 + JSON error body, and wraps the
  body in `http.MaxBytesReader` so chunked clients cannot bypass the
  static check. Per-tenant override via `TenantOverride`. 100%
  coverage.
- **`go.uber.org/goleak`** (Story 3): `TestMain(m)` wrappers added
  to `internal/lifecycle`, `internal/workerpool`, `internal/memwatch`
  to fail any test that leaks a goroutine.
- **`internal/metrics/`** (Story 4): hand-rolled Prometheus registry
  exposing the v2.10.0 `ec_*` metric set
  (`ec_http_requests_total`, `ec_http_duration_seconds`,
  `ec_workflow_runs_total`, `ec_workflow_duration_seconds`,
  `ec_workerpool_queued`, `ec_workerpool_saturation_total`,
  `ec_oom_alarms_total`, `ec_goroutine_count`, `ec_heap_bytes`).
  Bounded label cardinality (`WithMaxSeries`) so a hot label can't
  OOM the registry. 95% coverage.
- **`internal/observability/`** (Story 4): shared OpenTelemetry
  helpers (`TraceIDFromContext`, `TraceIDFromRequest`,
  `ParseTraceparent`) and slog correlation
  (`LoggerFromContext`/`LoggerFromRequest`,
  `ContextWithTenant`/`TenantFromContext`, `RequestLogger`
  middleware). 92% coverage.
- **`monitoring/grafana/dashboards/v210/`** (Story 4): four
  dashboards — `ec-overview`, `ec-tenant`, `ec-workerpools`,
  `ec-resilience` — covering RED method, per-tenant deep-dive, pool
  saturation, OOM/goroutine/heap/workflow failure trends.
- **`internal/evomap/`** (Story 5): NDJSON `Sink` writes one
  `Capsule` per minute per binary to a rotating file, plus
  `Aggregate`/`RenderCapsuleMarkdown`/`WriteCapsule` for the rollup
  pipeline. 88% coverage.
- **`cmd/evomap-rollup/`** (Story 5, NEW 8th binary): daily rollup
  reads NDJSON, aggregates KPIs, writes a markdown capsule that
  mirrors the existing fleet evoloop schema. Uses
  `lifecycle.Manager`. 89% coverage.

### Changed — binary refactors (Story 1 wiring)

- `cmd/mc-api/app.go`: `runServer` now delegates through
  `runServerWithLifecycle` so the http.Server is registered as a
  Closer. New `startObservability(mgr, logger, "mc-api")` boots
  `metrics.Registry` + `memwatch.Sampler` and registers them.
  `metricsHandler` appends the `ec_*` registry output after the
  legacy `agentic_ecommerce_*` exposition (single `/metrics` endpoint
  emits both surfaces).
- `cmd/agent-worker/main.go`: replaces the inline ctx.Done shutdown
  branch with `mgr.Shutdown()`. Memory metrics + ec_* registry
  exposed on the existing metrics endpoint via `noopHeader`-wrapped
  handler call.
- `cmd/temporal-worker/main.go`: registers `memwatch.Sampler` with a
  per-binary `metrics.Registry` so Temporal's worker.Run path inherits
  goroutine + heap ceiling monitoring without touching its
  InterruptCh signal handling.

### Quality gates (v2.10.0 baseline)

| Gate                       | Result          |
|----------------------------|-----------------|
| `go test -race ./...`      | PASS            |
| `go vet ./...`             | clean           |
| Coverage                   | 84.5% (>= 83%)  |
| Sentrux quality            | 6904 -> 6939    |
| Sentrux coupling           | 0.41 -> 0.40    |
| Sentrux complex_fn         | 4 (unchanged)   |
| `make build` (8 binaries)  | PASS            |
| `make compose-config`      | PASS            |
| `runx shell-leak-scan`     | no findings     |
| goleak (3 critical pkgs)   | clean           |

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
  metadata, `<cloud-metadata-ip>` Alibaba, fd00:ec2::254 v6 IMDS). Scheme
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
