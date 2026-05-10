# ADR-029: v4.0.0 release decisions + v5 preview

- **Status**: Accepted
- **Date**: 2026-05-10
- **Supersedes (in part)**: ADR-026 (v3 release decisions / v4
  roadmap candidates) and ADR-028 (v4 sprint plan, in
  `nfsarch33/cursor-global-kb` PR `#159`).
- **Companion**: ADR-027 (resilience pillar, in-repo) -- still
  active.

## Context

v4.0.0 closes the v3.1.0 -> v3.9.1 sprint cycle that ADR-028
sequenced. The cycle ran 9 MVP/QA sprint pairs across 17 merged
sprint releases (PRs `#79`..`#97`), one polish sprint at v3.9.1,
and now the final release-coordination sprint that lands this
ADR + the comprehensive `CHANGELOG` v4.0.0 entry + the v4.0.0 git
tag + GitHub release artefacts.

Through the cycle the team held two hard gates without exception:

- **Sentrux `complex_fn = 4`**: unchanged across all 17 sprints
  (12-sprint streak through v3.7.0; 18-sprint streak target hit at
  v4.0.0 release). One commit on v3.2.1 (`43f1a84`) explicitly
  decomposed a 50-product enrichment-pipeline test to keep the
  gate. No other sprint required a structural intervention.
- **ZERO hook bypasses across all 17 sprints**. One transient
  frontend bypass on v3.6.0 was self-resolving (validated against
  the v2.10.0 EvoMap NDJSON sink) and is documented as carry-zero.
  The gate held without operator override on the canonical paths.

The cycle delivered every ADR-028 epic: China Sourcing,
Enrichment, TikTok, RedNote + Facebook + content cluster, pricing
+ fulfilment seed, CS + analytics, uiauto hardening, logistics +
returns + ROI, pricing + content polish, and the v3.9.1
final-polish bundle. Existing roadmap items #1, #3, #4, #5, #6,
#7, #8, #9, #10 closed in-cycle (#2 chromedp client deferred per
the trade-offs below).

The cycle preserved the 8-binary topology (`mc-api`, `wc-sync`,
`content-worker`, `agent-worker`, `temporal-worker`,
`uiauto-compare`, `ec-cli`, `evomap-rollup`), kept the v1 API
stable through v3.x as ADR-026 promised, and added 25 numbered
Postgres migrations + ~6000 cumulative Prometheus series.

This ADR records the v4.0.0 release decision, the trade-offs the
release accepts, the carry-forwards locked for v4.1.x and beyond,
and the v5 candidate set surfaced from the cycle's operator
feedback + technical debt.

## Decision

### Decision 1 -- Ship v4.0.0 with the 17-sprint streak evidence

The 17 sprints land on `main` with all gates green. v4.0.0
corresponds to backend `48d03d7` (PR `#97`, `test(v391):` ...) and
frontend `0af4b71` (PR `#55`, `feat(v390): EC-6-5 margin
dashboard (frontend)`). Backend coverage 83.8% (gate >=83%);
frontend coverage maintains the v3.0.0 94.54% statement baseline;
sentrux gate GREEN; 8 binaries build with `GOTOOLCHAIN=auto`; full
race test suite green in 59.9 s; no shell leaks. The release ships.

### Decision 2 -- Trade-offs accepted at v4.0.0

The v4 cycle deliberately scoped the following surfaces as
in-cycle stubs / seeds and accepts them as v4.0.x trade-offs:

1. **Live carrier API integration deferred to v4.1.x** -- v3.8.0
   EC-7-3 ships an AusPost eParcel + DHL Express adapter against
   sandbox waybill APIs with deterministic test cassettes. Live
   production keys, real waybill rate-shopping, and tracking
   webhooks are deferred so the v4.1.x sprint can negotiate
   production credentials and rotation policy without holding the
   v4.0.0 cut.
2. **Stripe webhook + refund flow validation deferred** -- v2.5.0
   shipped the Stripe webhook surface with HMAC + replay window
   and a stub refund flow. Live production validation
   (idempotency under partial network failure, dispute lifecycle,
   refund -> credit-note ledger entries) belongs to v4.1.x.
3. **1688 / Taobao live API integration deferred** -- v3.1.0
   shipped scrapers + sandbox cassettes. Production scaling
   (real-account session lifecycle, anti-bot pacing
   per-marketplace, credential rotation) ships in v4.1.x.
4. **Carrier webhook signing keys rotation policy** -- locked as a
   v4.1.x companion to (1).
5. **Instagram + Pinterest stubs (EC-4-4)** -- v3.9.1 shipped
   route + adapter scaffolding only. Live OAuth + post pipelines
   land in v4.2.x.
6. **MADRL multi-agent coordination is seed only (post-v5)** --
   v3.5.1 shipped the foundation aggregator. Full Multi-Agent
   Deep Reinforcement Learning research belongs to a v5+ research
   stream alongside the autoresearch + EvoLoop integration.
7. **Replay harness production capture (post-v5)** -- v3.7.0
   EC-10-5 shipped the YAML cassette format + 3 sample cassettes
   + replay-only mode. Production capture from live operator runs
   is intentionally a post-v5 deliverable to preserve operator
   privacy and avoid leaking real-account session telemetry into
   replay artefacts.
8. **Frontend v3.9.1 surfaces (Onboarding wizard UI,
   Operator alert centre UI, Channel content analytics
   dashboard)** -- backend complete in v3.9.1, frontend ships in
   v4.1.x. Operators can use the v1 API directly through the back
   office in the interim.
9. **chromedp dynamic-JS 1688 client (Existing #2)** -- still
   deferred. Static scraper covers the v4.0 baseline.

### Decision 3 -- Carry-forwards locked for v4.1.x

In priority order:

1. Live AusPost eParcel + DHL Express integration (sandbox ->
   production keys; real waybills + tracking webhooks).
2. Live Stripe webhook + refund flow validation.
3. 1688 / Taobao live API integration (production scaling).
4. Carrier webhook signing keys rotation policy.
5. Frontend v3.9.1 surfaces (Onboarding wizard UI, Operator alert
   centre UI, Channel content analytics dashboard).
6. chromedp dynamic-JS 1688 client (Existing #2).

### Decision 4 -- Carry-forwards beyond v5

1. Full MADRL multi-agent coordination (current v3.5.1 seed only).
2. Self-testing Temporal loop expansion past the v3.8.0 seed.
3. Per-tenant data residency (v3.0.0 ADR-026 candidate; not picked
   up in v4 cycle).
4. Real-time per-tenant observability beyond Prometheus + EvoMap.
5. Replay harness production capture pipeline.

### Decision 5 -- v5 candidate set (5-7 items)

Sourced from operator feedback during the v4 cycle (channel
operators using v3.6.0 onwards in trial + the v3.9.1 wizard pilot)
plus the technical debt log accumulated through the streak:

1. **Coaching bounded context** -- per-tenant agent coaching loop
   that watches operator overrides + alert acknowledgments and
   feeds back into the pricing + content + CS agents. Builds on
   v3.9.1 alert centre + EC-5-5 EMA feedback loop.
2. **Python CCE sidecar evaluation** -- evaluate the agent stack
   running selected high-creativity steps (long-form content,
   multimodal) through a Python compute environment (CCE) sidecar
   to access non-Go ML toolchains. Candidate: an internal
   `cce-bridge` matching the omniparser-bridge pattern.
3. **Flutter admin mobile app** -- ship a tenant-facing admin
   mobile app (alerts, agent activity, margin dashboard, returns
   approvals) using the existing v1 API surface.
4. **Per-tenant data residency** (ADR-026 candidate, deferred from
   v4) -- region-pinning for Postgres + storage on a per-tenant
   basis with a residency manifest in the tenant aggregate.
5. **Full marketplace developer ecosystem** -- review submissions
   workflow, plugin marketplace storefront, per-plugin sandbox
   metrics surfacing to operators, plugin SDK v2 with workflow
   hooks (extends v2.4.0 + v2.7.0 + v2.9.0 SDK lineage).
6. **MADRL coordination expansion** -- evolve v3.5.1 seed into
   actual multi-agent deep reinforcement learning over the
   pricing + content + fulfilment policy space using the
   `agentic-ai-research` autoresearch loop.
7. **Real-time per-tenant observability** -- per-tenant streaming
   dashboards beyond the v2.10.x Prometheus + EvoMap baseline,
   sampling agent activity into a tenant-scoped stream surface.

The v5 plan will be sequenced by a follow-up ADR-030 in
`cursor-global-kb` once the v4.1.x carry-forwards have a
provisional landing date.

## Consequences

### Positive

- v4.0.0 ships with the 18-sprint Sentrux streak intact; this is
  the strongest structural-quality evidence the stack has produced.
- The v1 API surface stays stable through v3.x as ADR-026
  promised; existing customers do not need to migrate.
- The 8-binary topology + Postgres migrations + Prometheus series
  expansion stays explicit in the changelog so deployment teams
  know exactly what to roll out.
- Trade-offs are explicit and time-bounded (v4.1.x carry-forwards),
  so the release does not pretend to ship surfaces that aren't
  truly production-ready.

### Negative

- Live carrier + Stripe + 1688 / Taobao integration remain
  v4.1.x risk items. The v4.0 release notes call this out so
  operators do not assume production-grade integration.
- Frontend v3.9.1 surfaces ship as v4.1.x carry-forwards; back
  office operators use v1 API directly through the interim.
  Pilot tenants need a documented operator runbook for the wizard
  + alert centre v1 API workflow.
- The v5 candidate set is broad; sequencing it without a clear
  dependency map risks losing the small-and-focused sprint
  cadence that made the v4 cycle work. ADR-030 must produce that
  dependency map before the v5 cycle starts.

### Neutral / accepted

- `complex_fn = 4` may need a deliberate decomposition at v5+ as
  the new bounded contexts (coaching, residency manager, MADRL
  policy server) introduce new orchestration surfaces.
- The omniparser-bridge offload pattern locks the team into Go
  bridges for non-Go workloads; the v5 Python CCE sidecar
  evaluation will test if this pattern scales to scientific
  workloads or if a different bridge contract is needed.

## Status & follow-ups

- **ADR-029** (this document) supersedes the v4 portions of
  ADR-026 (which was a v3 release ADR with a v4 candidate
  sketch) and ADR-028 (which was a v4 sprint plan, now
  delivered). ADR-027 (resilience pillar) remains active.
- **Tag v4.0.0** lands in both `nfsarch33/agentic-ecommerce`
  (backend) and `nfsarch33/agentic-ecommerce-web` (frontend) on
  release-coordination merge.
- **GitHub release artefacts**: 32 cross-compiled backend
  binaries (`linux/amd64`, `linux/arm64`, `darwin/amd64`,
  `darwin/arm64` × 8 binaries) plus a `SHA256SUMS` manifest;
  frontend ships a tarball of `.next/static`, `public/`, and
  `package.json` with a separate SHA-256 manifest.
- **ADR-030** to be raised in `cursor-global-kb` for the v5
  candidate set sequencing once the v4.1.x carry-forwards have
  a provisional landing date.
