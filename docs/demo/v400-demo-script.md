# v4.0.0 Demo Script -- 30-minute live walkthrough

This script walks an operator + technical reviewer through the
Agentic Ecommerce v4.0.0 stack end-to-end. The walkthrough exercises
every closed ADR-028 epic and every Existing roadmap item that landed
in the v3.1.0 -> v3.9.1 cycle, plus the v3.9.1 polish bundle.

**Prereqs**:

- Backend `agentic-ecommerce` checked out at tag `v4.0.0` (or
  `main` post-merge of the release PR).
- Frontend `agentic-ecommerce-web` checked out at tag `v4.0.0`.
- Docker running (the v2.x compose stack covers the demo
  topology); `omniparser-bridge` reachable via
  `EC_OMNIPARSER_BRIDGE_URL` if RedNote / TikTok creator-center
  uiauto demos run live.
- Postgres seeded with the demo tenant (`make
  tenant-isolation-seed` or the equivalent `ec-cli tenant create`
  flow).
- `EC_ADMIN_TOKEN` set for the demo admin (used by `ec-cli` and
  the back-office API calls).

**Approximate time-box**: 30 minutes. Each step lists the
walkthrough artefact (CLI command, browser route, ADR / migration
reference) so the demo can be re-played offline.

## Step 1 -- AI onboarding wizard (Existing #10, v3.9.1, ~3 min)

1. Operator opens the back-office onboarding API (or the v4.1.x
   wizard UI when shipped):
   `curl -H "Authorization: Bearer $EC_ADMIN_TOKEN" \
        http://localhost:8080/api/v1/onboarding/wizards/start \
        -d '{"tenant_id": "demo-tenant", "step": 1}'`.
2. Walk through 4 steps: tenant identity, channel pre-flight
   (TikTok + Facebook + RedNote credentials check), pricing +
   fulfilment defaults, agent enable matrix.
3. Migration reference: `migrations/0023_onboarding_wizards.up.sql`.
4. Talking point: 4-step wizard collapses ~2 hours of manual setup
   into a guided flow; v4.1.x ships the matching frontend UI.

## Step 2 -- Sourcing trend ingestor + product enrichment (v3.1.x + v3.2.x, ~4 min)

1. Trigger the China sourcing agent:
   `bin/ec-cli sourcing run --tenant demo-tenant --query "summer
   running shoes" --platform 1688`.
2. Show the sourcing scorer output (40% supplier / 35% margin /
   25% trend) and the AU-import compliance gate filtering
   restricted SKUs.
3. Show the enrichment pipeline producing multilingual descriptions
   (en, zh-cn, ja, ko), hero image with bg-removal, SEO metadata,
   and pgvector trend signal.
4. Reference: PR `#79` v3.1.0 sourcing foundation, PR `#81` v3.2.0
   enrichment pipeline. Cassette replay drops in for the demo so
   no live 1688 / Taobao calls leak credentials.

## Step 3 -- TikTok listing creation + order webhook (v3.3.x, ~3 min)

1. Run TikTok listing agent:
   `bin/ec-cli tiktok-listing publish --tenant demo-tenant
   --product-id <demo-sku>`.
2. Show the seller API client posting to TikTok Shop sandbox
   within 15 s, surfacing the new `tiktok_listing_id`.
3. Send a tampered HMAC webhook to demonstrate rejection:
   `curl -H "X-TikTok-Signature: tampered" \
        http://localhost:8080/webhook/tiktok/order \
        -d <test-payload>` -> `401 Unauthorized` with the v3.3.0
   constant-time HMAC comparison.
4. Send a valid webhook -> Order saga lands an inventory deduction
   atomically (no oversell across TikTok + WC).
5. Reference: PR `#84` v3.3.0 TikTok integration.

## Step 4 -- RedNote post via uiauto facade (v3.4.x, ~3 min)

1. Trigger RedNote post:
   `bin/ec-cli rednote publish --tenant demo-tenant --post-id
   <demo-content-id>`.
2. Show the omniparser-bridge handshake (HMAC-SHA256 +
   constant-time compare) and the uiauto facade walking the
   RedNote creator-center DOM.
3. Show the v3.7.0 EC-10-1 session manager loading the operator's
   stored session blob from `EC_SESSION_MASTER_KEY`-keyed AES-256
   storage (Linux/WSL/CI) or macOS keychain (`darwin` build tag).
4. Reference: PR `#86` v3.4.0 + PR `#92` v3.7.0 uiauto hardening.

## Step 5 -- Pricing agent w/ guardrails + competitor scraper (v3.5.x + v3.9.x, ~3 min)

1. Run dynamic pricing agent:
   `bin/ec-cli pricing recompute --tenant demo-tenant
   --product-id <demo-sku>`.
2. Show the v3.5.0 EC-6-1 supplier cost monitor + EC-6-2 platform
   fee + FX calc feeding the EC-6-3 dynamic pricing agent;
   highlight the safety guardrails (margin floor, channel-specific
   ceiling).
3. Run competitor scraper:
   `bin/ec-cli pricing scrape-competitors --tenant demo-tenant
   --product-id <demo-sku>`. Show competitor price shape from
   `migrations/0020_competitor_prices.up.sql`.
4. Reference: PR `#88` v3.5.0 + PR `#96` v3.9.0 polish.

## Step 6 -- Customer service messaging adapter + FAQ responder (v3.6.x, ~3 min)

1. Send an inbound TikTok DM:
   `curl -H "X-TikTok-Signature: $valid_sig" \
        http://localhost:8080/webhook/tiktok/messaging \
        -d '{"text": "what size for AU 8?"}'`.
2. Show the v3.6.0 EC-8-1 enquiry classifier (bilingual, 50-fixture
   accuracy) routing to the EC-8-2 FAQ responder; reply lands
   within 30 s end-to-end.
3. Show a complex enquiry escalating to the operator alert centre
   (v3.9.1 EC-9-5).
4. Reference: PR `#90` v3.6.0.

## Step 7 -- Margin dashboard with live ROI (v3.6.x + v3.8.x + v3.9.x, ~3 min)

1. Open the frontend `/margin-dashboard` at
   `http://localhost:3000/margin-dashboard`.
2. Show the daily margin rollup (top 20 SKUs, channel filter,
   date range) sourced from `migrations/0019_roi_daily_rollup`
   (v3.8.0) and the EC-9-1 GMV API (v3.6.0).
3. Drill into a single SKU -> show the per-channel ROI heatmap
   from `migrations/0019` populated by the v3.8.0 EC-9-3 query.
4. Show the v3.9.0 EC-6-5 margin dashboard panel with the EC-5-5
   content performance feedback loop annotation.
5. Reference: PRs `#88`, `#90`, `#94`, `#96`.

## Step 8 -- Operator alert centre + acknowledgment workflow (v3.9.x, ~2 min)

1. Show the operator alert centre via the v1 API:
   `curl -H "Authorization: Bearer $EC_ADMIN_TOKEN" \
        http://localhost:8080/api/v1/operator/alerts?tenant=demo-tenant`
   (frontend UI ships in v4.1.x).
2. Acknowledge an alert:
   `curl -X POST -H "Authorization: Bearer $EC_ADMIN_TOKEN" \
        http://localhost:8080/api/v1/operator/alerts/<id>/ack`.
3. Show the SSE feed (Step 10) reflecting the acknowledgment in
   real-time.
4. Migration reference: `migrations/0025_operator_alerts.up.sql`.
5. Reference: PR `#97` v3.9.1.

## Step 9 -- Drop-ship saga + returns workflow (v3.5.x + v3.8.x, ~3 min)

1. Trigger drop-ship saga:
   `bin/ec-cli fulfilment dropship --order <demo-order-id>`. Show
   the saga steps (order aggregator -> drop-ship agent -> carrier
   label) under the v3.5.0 + v3.8.0 lineage.
2. Trigger returns saga:
   `bin/ec-cli fulfilment returns init --order <demo-order-id>
   --reason damaged`. Show the auto-approval threshold from the
   v3.8.0 EC-7-5 saga (configurable per tenant via the
   `0006_tenant_settings_compliance_reporting` extension).
3. Show carrier label generation (AusPost eParcel cassette /
   DHL Express cassette) -- live carrier integration is a v4.1.x
   carry-forward per ADR-029.
4. Reference: PRs `#88`, `#94`.

## Step 10 -- SSE agent activity feed (v3.6.x, ~2 min)

1. Open the frontend `/agent-activity` at
   `http://localhost:3000/agent-activity`.
2. Re-run any of the previous steps in another terminal -> show
   the SSE stream rendering activity within 1 s end-to-end.
3. Show the 100-event rolling window auto-scrolling and the
   reconnect handling (kill the backend `mc-api` for ~10 s; show
   the SSE client reconnecting cleanly).
4. Reference: PR `#54` (frontend) + PR `#90` (backend).

## Wrap-up (~1 min)

- 17-sprint streak: every gate held. `complex_fn = 4` unchanged
  through 18-sprint target.
- 8 production binaries; 25 numbered migrations; ~6000 Prometheus
  series.
- v4.1.x carry-forwards: live carrier APIs, live Stripe webhook +
  refund, live 1688 / Taobao production scaling, frontend v3.9.1
  surfaces, chromedp.
- ADR-029 in `docs/adr/adr-029-v4-release-decisions.md` is the
  canonical v4 release record.

## Re-run / offline replay

Every step has a deterministic replay path:

- Sourcing -> `dnaeon/go-vcr/v3` cassettes
  (`internal/adapter/china/testdata/cassettes/`).
- TikTok / RedNote -> uiauto cassette replay (`tests/uiauto/
  cassettes/`).
- CS -> 50-fixture corpus (`internal/agent/customerservice/
  testdata/`).
- Pricing -> deterministic cost feed
  (`tests/load/k6/v400_release.js` once recorded).
- Returns / drop-ship -> Temporal `testsuite.WorkflowTestSuite`
  fixtures.

The full demo can be replayed against an air-gapped environment by
running the cassette tests + the local docker-compose stack -- no
live integration required.
