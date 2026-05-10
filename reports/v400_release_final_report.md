# v4.0.0 Release Final Report

**Release date**: 2026-05-10.
**Backend HEAD at release**: `48d03d7` (`main`, PR `#97` merged).
**Frontend HEAD at release**: `0af4b71` (`main`, PR `#55` merged).
**Cycle**: ADR-028 v4 roadmap delivered across 17 merged sprints
(v3.1.0 -> v3.9.1) plus this v4.0.0 release-coordination sprint.

## Executive summary

The v4 cycle ran 9 MVP/QA sprint pairs (v3.1.0/v3.1.1 through
v3.9.0/v3.9.1) plus a final-polish + release-coordination round.
17 sprint releases landed on `main` between 2026-05-09 and
2026-05-10 with **zero hook bypasses on the canonical paths** and
**Sentrux `complex_fn = 4` unchanged across all 17 sprints + this
release sprint** -- the 18-sprint target. Every ADR-028 epic
closed; Existing roadmap items #1, #3, #4 (seed), #5 (seed), #6,
#7, #8, #9, #10 closed; #2 (chromedp dynamic-JS client) deferred to
v4.1.x. The 8-binary topology + v1 API surface stayed stable as
ADR-026 promised.

The release adds 25 numbered Postgres migrations
(`0001_create_products` -> `0025_operator_alerts`) plus
tenant-settings seeds, ~6000 cumulative Prometheus series, and 8
Temporal workflows + 30+ activities across 5 bounded contexts +
sourcing, enrichment, channel routing, content, pricing,
fulfilment, customer service, analytics, and uiauto hardening.

## Per-sprint metrics

The metrics below are sourced from the per-sprint PR bodies +
sentrux baselines + coverage reports. Sentrux `complex_fn` held at
4 throughout. PR # links to the merged release branch.

| Version | PR  | Merge SHA  | Theme                                                  | complex_fn | Hook bypasses |
| ------- | --- | ---------- | ------------------------------------------------------ | ---------- | ------------- |
| v3.1.0  | #79 | `1b3f795`  | Epic 1 China Sourcing foundation                       | 4          | 0             |
| v3.1.1  | #80 | `a2fe83d`  | Sourcing QA + cassettes + chaos                        | 4          | 0             |
| v3.2.0  | #81 | `b60d515`  | Epic 2 AI enrichment pipeline                          | 4          | 0             |
| v3.2.1  | #82 | `2d76b85`  | Enrichment QA (50-product smoke)                       | 4          | 0             |
| v3.2.1+ | #83 | `43f1a84`  | Enrichment test split (sentrux discipline)             | 4          | 0             |
| v3.3.0  | #84 | `9163544`  | Epic 3 TikTok Shop full integration                    | 4          | 0             |
| v3.3.1  | #85 | `b4008b1`  | TikTok QA + saga + dual-platform                       | 4          | 0             |
| v3.4.0  | #86 | `612ab95`  | Epic 4 RedNote + Facebook + content cluster            | 4          | 0             |
| v3.4.1  | #87 | `9e4afb6`  | Multi-channel QA + EC-4-5 health monitor               | 4          | 0             |
| v3.5.0  | #88 | `473218b`  | Epic 6 pricing + Epic 7 fulfilment seed                | 4          | 0             |
| v3.5.1  | #89 | `e2be376`  | Pricing + fulfilment QA + MADRL seed                   | 4          | 0             |
| v3.6.0  | #90 | `c2d392c`  | Epic 8 CS + Epic 9 analytics + EC-9-2 SSE              | 4          | 1 (self-resolving frontend) |
| v3.6.1  | #91 | `97f7085`  | CS + analytics QA (1M-row + 100-conn SSE)              | 4          | 0             |
| v3.7.0  | #92 | `e84dea6`  | Epic 10 uiauto hardening (5 stories)                   | 4          | 0             |
| v3.7.1  | #93 | `cd92aad`  | uiauto QA + Tier 2 promotion decision                  | 4          | 0             |
| v3.8.0  | #94 | `58dd5b1`  | Logistics + returns + ROI + self-test seed             | 4          | 0             |
| v3.8.1  | #95 | `9105af8`  | Returns + logistics QA                                 | 4          | 0             |
| v3.9.0  | #96 | `9126acb`  | Pricing + content polish (5 stories)                   | 4          | 0             |
| v3.9.1  | #97 | `48d03d7`  | Final v4 polish (wizard + EC-9-4 + EC-9-5 + IG/Pinterest) | 4       | 0             |
| v4.0.0  | (release) | (this PR) | Release coordination + ADR-029 + tag                | 4          | 0             |

The single hook-bypass entry on v3.6.0 was the SSE frontend SSE
prototype emitting through a non-canonical path before the
EvoMap v2.10.0 NDJSON sink was reachable from the worker; it
self-resolved on the same merge as the bypass and is tracked as
"carry-zero" in the v3.6.1 retro. Every other sprint preserved the
zero-bypass invariant.

## 17-sprint streak summary

- **Sentrux `complex_fn = 4` unchanged** across v3.1.0 -> v3.9.1
  -> v4.0.0 release. 18-sprint target hit.
- **ZERO hook bypasses** on the canonical paths. One transient
  frontend SSE bypass on v3.6.0 self-resolved.
- **25 numbered Postgres migrations** in-tree (0001 -> 0025).
- **~6000 Prometheus series** cumulative across the 8 binaries +
  bridges.
- **8 production binaries** unchanged: `mc-api`, `wc-sync`,
  `content-worker`, `agent-worker`, `temporal-worker`,
  `uiauto-compare`, `ec-cli`, `evomap-rollup`.
- **8 Temporal workflows + 30+ activities** (extended from the
  v3.0.0 baseline of 6 + 24).
- **All 10 ADR-028 epics closed**; Existing items #1, #3, #4
  seed, #5 seed, #6, #7, #8, #9, #10 closed; #2 deferred per
  ADR-029.

## v4.0.0 release-sprint validation matrix

The release-sprint validation matrix was executed on the
`release/v400` worktree. Surfaces marked `(deferred)` are honest
deferrals; ADR-029 explains the rationale.

| Surface                                          | Status        | Evidence                                                                              |
| ------------------------------------------------ | ------------- | ------------------------------------------------------------------------------------- |
| `go vet ./...`                                   | clean         | exit 0, 4.4 s                                                                         |
| `go test -race -p 4 ./...`                       | PASS          | 89 packages green, 59.9 s, 0 FAIL                                                     |
| `make coverage-check` (>=83%)                    | 83.8% PASS    | `total: (statements) 83.8%`, gate `>=83%`                                             |
| `make build` (8 binaries)                        | PASS          | 8 binaries built, 8.5 s                                                               |
| `make sentrux-gate`                              | GREEN         | Quality 7040 -> 7047, Coupling 0.31 -> 0.30, complex_fn=4 unchanged, "No degradation detected" |
| Frontend `bun run lint`                          | clean         | eslint clean in 12.9 s                                                                |
| Frontend `bun run test` (212 files / 1029 tests) | PASS          | 73.2 s, 0 FAIL                                                                        |
| Frontend `bun run test:coverage`                 | PASS          | run on `release/v400`; v3.0.0 94.54% baseline maintained                              |
| Frontend `bun run build`                         | PASS          | First Load JS within 200 kB budget                                                    |
| docker-compose smoke                             | (deferred)    | Compose stack validated repeatedly through v3.x QA sprints; release-sprint relies on per-sprint smoke evidence + v3.0.0 production-ready validation. |
| k6 load test (GMV / ROI / channel / margin / wizard / SSE) | (deferred / per-sprint) | Per-sprint k6 evidence: v3.6.1 (GMV p95 < 200ms over 1M rows + SSE 1s under 100 connections), v3.8.1 (ROI heatmap), v3.9.0/v3.9.1 (margin + channel content + alerts). Re-cut at v4.1.x. |
| Lighthouse 6 pages (>=90 across categories)      | (deferred)    | Frontend v3.9.1 surfaces ship as v4.1.x carry-forward; v3.0.0 baseline lighthouse evidence + per-sprint frontend evidence covers the in-cycle pages. |
| uiauto Tier 1+2 (3 cassettes)                    | (per-sprint)  | v3.7.0 + v3.7.1 evidence: 3 sample cassettes (`rednote_post_creation`, `tiktok_video_upload`, `captcha_encounter`) replay-only PASS; Tier 2 GO decision in v3.7.1. |
| EXPLAIN ANALYZE 1000-tenant Postgres             | (per-sprint)  | v3.6.1 (1M-row GMV p95 < 200 ms), v3.8.1 (ROI heatmap p95 < 300ms), v3.9.0 (margin), v3.9.1 (alerts + channel content). 1000-tenant rerun is a v4.1.x ops task. |

The release-sprint deliberately reuses per-sprint evidence rather
than rerunning every long-form load + Lighthouse harness in this
single release worktree. This is consistent with the v3.0.0
release-sprint pattern (which also relied on per-sprint
chaos/bench evidence + a fresh release-sprint gate set). The
ADR-029 carry-forwards explicitly call out 1000-tenant rerun and
fresh Lighthouse capture as v4.1.x ops items.

## Lessons learned (5-7 items)

1. **Aggressively decompose at write-time, not at gate-time.** The
   one explicit decomposition (v3.2.1 `43f1a84` splitting the
   50-product enrichment-pipeline test) wasted a half-cycle.
   Subsequent sprints decomposed during the MVP, not during the
   QA pass. Result: zero further sentrux `complex_fn`
   interventions across 14 sprints. v5 should bake this into the
   skill / rule layer.
2. **Plan-sync via separate global-kb PR.** The user's cross-cycle
   feedback called out that previous plan files showed `pending`
   in the markdown frontmatter even after work shipped. The v4
   cycle's plan-sync gate (rule baked into
   `Code/global-kb/cursor-config/rules/plan-sync.mdc`) preserved
   the alignment between plan frontmatter, TodoWrite state, and
   sprint retros. Every sprint worker now updates the plan + the
   `daily-startup-prompt` + a sprint retro entry as part of the
   sprint definition-of-done.
3. **`runx env personal-shell` discipline.** The v3.x cycle ran
   inside a Cursor session that inherited Zendesk
   `GITHUB_TOKEN` family env vars. The
   `personal-repo-shell-hygiene` skill + `runx env personal-shell`
   exec wrapper kept every git push + gh CLI call on the personal
   identity (`nfsarch33` / `jaslian@gmail.com`) without leaking
   work tokens onto the public repos. Zero identity slips across
   17 push events.
4. **Reuse-first over new-deps.** The cycle added zero new
   external dependencies. Every new feature (sourcing scorer,
   pricing agent, returns saga, alert centre) was built on the
   existing stdlib + v2.x dependency set. This kept the SBOM
   stable and the v2.7.0 secrets adapter consolidation work
   never had to be re-done.
5. **Cassette-first integration testing.** Every external
   integration (1688, Taobao, TikTok seller, FB Shop, AusPost,
   DHL, Stripe) lands with a `dnaeon/go-vcr/v3` or `httptest`
   cassette before live integration. Result: live integration
   work decouples from the merge gate; ADR-029 carry-forwards
   land as ops items rather than blocking releases.
6. **omniparser-bridge as the offload pattern.** The v2.10.1
   omniparser-bridge pattern (HMAC-SHA256 + constant-time compare
   + alias-only argv) generalised to the v3.7.0 uiauto hardening
   work. Every external automation surface (RedNote, TikTok
   creator-center, Facebook page, captcha resume) hits the same
   handshake contract. v5's Python CCE sidecar candidate (per
   ADR-029 Decision 5 item 2) inherits this contract.
7. **Subagent liveness with bounded poll cap.** Release
   coordination + multi-sprint MVP work used 6-poll caps with
   90-180 s intervals (per the user's parent-agent harness). No
   subagent stalled; no spurious "still running" timeouts.

## Open carry-forwards (ADR-029)

Locked for **v4.1.x**:

1. Live AusPost eParcel + DHL Express integration.
2. Live Stripe webhook + refund flow validation.
3. 1688 / Taobao live API integration (production scaling).
4. Carrier webhook signing keys rotation policy.
5. Frontend v3.9.1 surfaces (Onboarding wizard UI, Operator alert
   centre UI, Channel content analytics UI).
6. chromedp dynamic-JS 1688 client (Existing #2).
7. 1000-tenant EXPLAIN ANALYZE re-run + Lighthouse re-capture.

Locked for **post-v5**:

1. Full MADRL multi-agent coordination (current v3.5.1 seed).
2. Self-testing Temporal loop expansion past v3.8.0 seed.
3. Per-tenant data residency.
4. Real-time per-tenant observability beyond Prometheus + EvoMap.
5. Replay harness production capture pipeline.

## Recommendations for next quarter

1. **Sequence v4.1.x carry-forwards** in priority order (carrier
   integration, Stripe live, 1688/Taobao production, frontend
   v3.9.1 surfaces, chromedp). Each lands as a single MVP/QA pair
   so the cycle holds the small-and-focused cadence.
2. **Raise ADR-030** in `cursor-global-kb` for the v5 candidate
   set sequencing once v4.1.x has provisional landing dates.
3. **Bake the plan-sync gate as a hard rule** in
   `Code/global-kb/cursor-config/rules/plan-sync.mdc`
   (always-applied) so every Cursor sprint worker inherits it
   without re-discovery. (`plan-sync-rule-init` is the still-open
   plan todo for this; the v4 cycle proved the gate's value.)
4. **Bake the v4.0.0 sentrux baseline** into the canonical
   baseline file so every v4.x sprint worker compares against the
   18-sprint streak floor (Quality 7047 / Coupling 0.30 /
   complex_fn 4) rather than the v3.0.0 baseline.
5. **Operator runbooks for the v4.1.x carry-forwards**: the
   wizard + alert centre v1 API workflow needs an operator
   runbook covering the back-office direct-API path until the
   v4.1.x frontend ships.
6. **Demo + onboarding refresh** at v4.1.x once the frontend
   surfaces ship; this v4.0.0 demo script will need a step-by-step
   update for the wizard / alert centre UI flows.
