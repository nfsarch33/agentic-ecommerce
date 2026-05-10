# ADR-030: v4.1.0–v5.0.0 Roadmap

- **Status**: Proposed (pending operator approval)
- **Date**: 2026-05-10
- **Supersedes**: ADR-029 v5 candidates section (Decision 5) and
  carry-forwards-beyond-v5 section (Decision 4). ADR-029 v4.1.x
  carry-forwards (Decision 3) remain active until resolved in the
  sprints below.
- **Companion**: ADR-029 (v4.0.0 release decisions), ADR-027
  (resilience pillar, still active)
- **Inputs**:
  - Phase 1+6 QA Audit (`v4-comprehensive-qa-audit-2026-05-10.md`):
    9 improvement candidates (IC-1 through IC-9), all streams GREEN
  - Phase 2 Integration Analysis
    (`v5-roadmap-integration-analysis-2026-05-10.md`): 33
    prioritized candidates across 4 tiers from 3 external documents
    + ADR-029 carry-forwards
  - ADR-029 v4.0.0 release decisions: 6 locked carry-forwards, 7 v5
    candidates, 5 beyond-v5 items

## Context

v4.0.0 shipped on 2026-05-10 with an 18-sprint Sentrux
`complex_fn=4` streak, 83.8% backend coverage, 94.65% frontend
coverage, 83 race-clean packages, 16 constant-time HMAC surfaces,
and zero critical issues across all four QA streams plus the Phase 6
resilience audit.

The QA audit surfaced 9 improvement candidates (IC-1 through IC-9)
that strengthen the existing v4.0.0 codebase. The Phase 2 document
integration identified 33 new feature candidates from three external
documents (Cloud-Native Plan, Payment Integration Plan, 10 Priority
Epics) plus ADR-029's 7 v5 candidates and 6 locked carry-forwards.

The v4.0.0 codebase has two critical functional gaps blocking
production revenue: (1) a Stripe-only billing layer with no
multi-provider payment infrastructure, and (2) a Cloud Run / ECS
runtime inadequate for the mixed workload topology (stateless APIs +
stateful Temporal + long-running agents + GPU uiauto).

This ADR sequences the path from v4.1.0 through v5.0.0 using the
proven MVP → QA sprint-pair cadence that delivered 10 epics across
v3.1.0–v3.9.1 without breaking the Sentrux gate.

## Decision

### Sprint Cadence

10 sprint pairs (20 sprints) following the proven pattern:

- **MVP sprint**: 5 stories, TDD-first, worktree-isolated,
  conventional commit, PR with full gate evidence
- **QA sprint**: hardening tests, load validation, carry-forward
  closure, IC resolution
- **Plan-sync gate**: global-kb PR with capsule + retro + daily
  startup pointer (enforced by `.cursor/rules/plan-sync.mdc`)
- **Sentrux `complex_fn=4` HARD GATE** continues through v5.0.0

### Sprint Allocation

---

#### Sprint Pair 1: v4.1.0 MVP / v4.1.1 QA — Carry-Forwards + QA Hardening

**v4.1.0 MVP** (5 stories):

| # | Story | Source | Extends |
|---|-------|--------|---------|
| 1 | RLS policies for migrations 0012–0025 (operator_alerts, onboarding_wizards, content_calendar, competitor_prices, shipping_labels, returns) | IC-1 | `migrations/0026_rls_backfill.up.sql` |
| 2 | `context.WithTimeout` on all outbound HTTP calls within Temporal activity implementations | IC-7 | `internal/workflow/*.go` |
| 3 | Postgres `statement_timeout` GUC per-connection for hot-path queries | IC-8 | `internal/store/`, connection config |
| 4 | Live Stripe webhook + refund flow validation (production idempotency, dispute lifecycle, refund → credit-note ledger) | CF-1 | `internal/billing/` |
| 5 | Live AusPost eParcel + DHL Express (production keys, real waybills, tracking webhooks) | CF-2 | `internal/adapter/logistics/` |

**v4.1.1 QA** focus:

- Payment + carrier E2E validation (Stripe production webhook replay, AusPost/DHL production smoke)
- IC-3: Convert ~15 operational `fmt.Errorf` to sentinel errors with `%w` wrapping
- IC-9: Expose Redis pool sizing (PoolSize, DialTimeout, ReadTimeout, WriteTimeout) as environment variables
- IC-2: Backfill `IF NOT EXISTS` on 6 indexes in migration 0001
- `govulncheck` + `go vet` full pass (blocked at v4.0.0 by toolchain issue)
- Race test suite on current Go toolchain

---

#### Sprint Pair 2: v4.2.0 MVP / v4.2.1 QA — Payment Foundation

**v4.2.0 MVP** (5 stories):

| # | Story | Source | Extends |
|---|-------|--------|---------|
| 6 | `PaymentGateway` port interface: Authorize/Capture/Void/Refund + `WebhookHandler` + `Money` type | Phase 2 #7 | NEW `internal/port/payment.go`, `internal/payment/types.go` |
| 7 | Stripe adapter (full lifecycle, wraps existing `billing/service.go`) | Phase 2 #8 | NEW `internal/payment/stripe/`, dep `stripe/stripe-go/v82` |
| 8 | Alipay adapter via gopay (CN market) | Phase 2 #9 | NEW `internal/payment/alipay/`, dep `go-pay/gopay` |
| 9 | WeChat Pay V3 adapter via gopay (CN market) | Phase 2 #10 | NEW `internal/payment/wechat/`, dep `go-pay/gopay` (shared) |
| 10 | Temporal Payment Saga: reserve inventory → authorize → capture → fulfil + LIFO compensations | Phase 2 #14 | NEW `internal/workflow/payment_saga.go` (7th Temporal workflow) |

**v4.2.1 QA** focus:

- Interface compile checks (`var _ port.PaymentGateway = (*X)(nil)`)
- Stripe/Alipay/WeChat sandbox integration tests
- Webhook HMAC verification for all 3 providers
- `Money` minor-unit enforcement tests
- Saga compensation injection tests (fail at each step)
- IC-5: Add Grafana dashboard panels for v3.5.0–v3.9.1 metrics (pricing, fulfilment, CS, UIAuto, logistics, alerts)

**New go.mod deps**: `github.com/stripe/stripe-go/v82`, `github.com/go-pay/gopay`

---

#### Sprint Pair 3: v4.3.0 MVP / v4.3.1 QA — Payment Expansion + Frontend

**v4.3.0 MVP** (5 stories):

| # | Story | Source | Extends |
|---|-------|--------|---------|
| 11 | PayPal adapter via gopay | Phase 2 #11 | NEW `internal/payment/paypal/`, dep `go-pay/gopay` (shared) |
| 12 | gopay OSS SDK evaluation + integration (pin version, circuit breaker wrapping) | Phase 2 #12/#13 | `internal/payment/router.go`, `internal/payment/idempotency.go` |
| 13 | Payment webhook normaliser: unified inbound across Stripe/Alipay/WeChat/PayPal + retry queue + DLQ | Phase 2 #15 | NEW `internal/payment/webhook/normaliser.go`, extends `internal/eventbus/` |
| 14 | Frontend payment dashboard page (checkout flow, payment method selection, order status) | NEW | `agentic-ecommerce-web` |
| 15 | AI PaymentAdvisorActivity: LLM-driven payment method recommendation + risk scoring | Phase 2 #17 | NEW `internal/payment/advisor.go` (Temporal activity, existing LLM client) |

**v4.3.1 QA** focus:

- Multi-provider payment E2E (Stripe + Alipay + WeChat + PayPal sandbox full cycle)
- PayPal sandbox integration validation
- Webhook normaliser deduplication + retry under testcontainers Postgres
- IC-6: Add Prometheus alert rules for OOM (`ec_oom_alarms_total`), pricing guardrails (`ec_pricing_decisions_total{outcome="guardrail_blocked"}`), CAPTCHA spikes (`ec_captcha_detections_total`)
- Payment advisor mock tests (LLM response validation)

---

#### Sprint Pair 4: v4.4.0 MVP / v4.4.1 QA — Cloud-Native Foundation

**v4.4.0 MVP** (5 stories):

| # | Story | Source | Extends |
|---|-------|--------|---------|
| 16 | GKE Autopilot Terraform modules + Workload Identity Federation | Phase 2 #21 | NEW `deploy/terraform/gcp-gke/`, consumes existing `modules/*` |
| 17 | Helm charts for all 8 binaries (mc-api, temporal-server StatefulSet, temporal-worker, agent-worker, content-worker, wc-sync, uiauto-compare, evomap-rollup) | Phase 2 #22 | NEW `deploy/helm/*/` |
| 18 | Dockerfile multi-stage optimization (slim images, layer caching, non-root user) | Phase 2 | `Dockerfile`, `deploy/docker/` |
| 19 | Health + readiness probes for all binaries (HTTP `/healthz` + `/readyz`) | Phase 2 | `cmd/*/main.go`, Helm chart `livenessProbe`/`readinessProbe` |
| 20 | KEDA autoscaler for Temporal workers (queue-depth ScaledObject, PDB for mc-api) | Phase 2 #26 | `deploy/helm/*/templates/`, KEDA operator |

**v4.4.1 QA** focus:

- `terraform plan` validation against sandbox GCP project
- `helm lint` + `helm template --dry-run` for all charts
- K8s deployment smoke test (kind cluster or GKE sandbox)
- Load testing: k6 500 RPS, p99 < 200ms through K8s ingress
- IC-4: Add `operator-alerts.spec.ts` E2E test (frontend)
- PDB disruption test (rolling upgrade simulation)

---

#### Sprint Pair 5: v4.5.0 MVP / v4.5.1 QA — Observability + Runtime

**v4.5.0 MVP** (5 stories):

| # | Story | Source | Extends |
|---|-------|--------|---------|
| 21 | OpenTelemetry SDK + Collector integration (traces + metrics, GKE DaemonSet) | Phase 2 #23/#24 | NEW `internal/telemetry/otel.go`, NEW `deploy/helm/otel-collector/` |
| 22 | Cloud Trace / Jaeger backend (GCP Cloud Trace IAM + GMP metric export) | Phase 2 #23 | OTel Collector config, IAM bindings |
| 23 | Go 1.26 runtime bump (new GC with ~40% overhead reduction, if available) | Phase 2 #19 | `Dockerfile`, `go.mod`, `.golangci.yml`, `Makefile` |
| 24 | Next.js 16 bump (Turbopack default, React Compiler stable, `default.js` fix) + TS 5.8 | Phase 2 #20 | `package.json`, `next.config.ts`, `tsconfig.json` |
| 25 | K8s NetworkPolicies (default-deny + explicit allow) + Pod Security Standards (restricted) | Phase 2 #28 | NEW `deploy/k8s/networkpolicies/`, `deploy/k8s/rbac/` |

**v4.5.1 QA** focus:

- OTel trace verification in Cloud Trace UI
- GMP metric scraping validation
- OTel Collector `memory_limiter` OOM guard test
- Race test suite on Go 1.26 (`go test -race ./...`)
- `govulncheck` on updated `go.mod`
- Lighthouse 6-page re-capture after Next.js 16 bump (gate ≥ 90)
- NetworkPolicy east-west isolation test
- Runtime regression testing (benchmark comparison pre/post bump)

---

#### Sprint Pair 6: v4.6.0 MVP / v4.6.1 QA — Channel Expansion

**v4.6.0 MVP** (5 stories):

| # | Story | Source | Extends |
|---|-------|--------|---------|
| 26 | Instagram Shopping full implementation (promote from stub: OAuth + post pipeline) | CF-5 / Phase 2 #29 | `internal/adapter/social/instagram_client.go` |
| 27 | Pinterest Shopping full implementation (promote from stub: OAuth + pin pipeline) | CF-5 / Phase 2 #29 | `internal/adapter/social/pinterest_client.go` |
| 28 | Channel onboarding wizard extension (IG + Pinterest + payment provider setup) | CF-5 | `agentic-ecommerce-web`, extends v3.9.1 wizard backend |
| 29 | 1688/Taobao live API production scaling (anti-bot pacing, credential rotation, session lifecycle) | CF-3 | `internal/adapter/china/` |
| 30 | Carrier webhook key rotation policy (AusPost + DHL signing key lifecycle) | CF-4 | `internal/adapter/logistics/` |

**v4.6.1 QA** focus:

- IG + Pinterest sandbox posting validation (OAuth token refresh cycle)
- Multi-channel expansion validation (6+ channel E2E)
- Carrier production smoke (AusPost waybill + DHL tracking webhook)
- 1688/Taobao anti-bot pacing validation under sustained load
- Onboarding wizard E2E (full tenant → channel → payment setup flow)

---

#### Sprint Pair 7: v4.7.0 MVP / v4.7.1 QA — MADRL + Observability + DR

**v4.7.0 MVP** (5 stories):

| # | Story | Source | Extends |
|---|-------|--------|---------|
| 31 | MADRL coordination expansion (promote v3.5.1 seed into pricing/content/fulfilment policy space) | V5-6 | `internal/coordination/` |
| 32 | Multi-agent pricing/fulfilment coordination (MADRL policy gradient over joint action space) | V5-6 | `internal/coordination/`, `internal/agent/pricing/`, `internal/agent/fulfilment/` |
| 33 | Real-time per-tenant observability dashboards (streaming agent activity into tenant-scoped surface) | V5-7 | Extends `internal/observability/`, Grafana |
| 34 | Per-tenant metrics isolation (label-based tenant scoping in Prometheus/GMP) | V5-7 | `internal/metrics/`, `internal/tenant/` |
| 35 | EKS Auto Mode disaster recovery (cross-cloud GKE ↔ EKS, RTO < 30 min) | Phase 2 #25 | NEW `deploy/terraform/aws-eks/` (replaces `aws-ecs/`) |

**v4.7.1 QA** focus:

- MADRL reward signal convergence test (statistically significant improvement over baseline)
- Multi-agent coordination conflict resolution validation
- Per-tenant dashboard E2E (tenant isolation proof)
- Cross-cloud failover smoke (GKE → EKS, measure RTO)
- `trivy config deploy/helm/*` security scan

---

#### Sprint Pair 8: v4.8.0 MVP / v4.8.1 QA — Flutter + Coaching + Marketplace

**v4.8.0 MVP** (5 stories):

| # | Story | Source | Extends |
|---|-------|--------|---------|
| 36 | Flutter admin app (mobile ops dashboard: alerts, agent activity, margin dashboard, returns approvals) | V5-3 | NEW `agentic-ecommerce-mobile/` (consumes v1 API) |
| 37 | Coaching bounded context (agent coaching loop watching operator overrides + alert acknowledgments) | V5-1 | NEW `internal/coaching/`, extends `internal/agent/content/feedback_loop.go` |
| 38 | Marketplace multi-vendor support seed (review submissions workflow, plugin sandbox) | V5-5 | NEW `internal/marketplace/plugin/` |
| 39 | Commission engine (per-vendor commission rates, settlement calculations) | V5-5 | `internal/marketplace/`, `internal/billing/` |
| 40 | Vendor onboarding workflow (Temporal workflow for vendor registration + KYC + store setup) | V5-5 | NEW `internal/workflow/vendor_onboarding.go` (8th Temporal workflow) |

**v4.8.1 QA** focus:

- Flutter widget tests + integration tests (Chromium + iOS simulator)
- Mobile E2E (login → alerts → margin dashboard → returns approval)
- Coaching feedback loop validation (operator override → agent behavior change within N cycles)
- Marketplace vendor isolation (tenant A vendor cannot access tenant B data)
- Commission calculation edge cases (multi-currency, tiered rates)

---

#### Sprint Pair 9: v4.9.0 MVP / v4.9.1 QA — Data Residency + Compliance + Polish

**v4.9.0 MVP** (5 stories):

| # | Story | Source | Extends |
|---|-------|--------|---------|
| 41 | Per-tenant data residency: region-pinning for Postgres + storage (AU/CN/EU residency manifest in tenant aggregate) | V5-4 | `internal/tenant/`, NEW `migrations/002x_data_residency.up.sql` |
| 42 | GDPR/CCPA compliance layer (data export, right-to-delete, consent management, audit trail) | V5-4 | NEW `internal/compliance/`, extends `internal/tenant/` |
| 43 | Python CCE sidecar evaluation (cce-bridge service, mirrors omniparser-bridge HTTP bridge pattern) | V5-2 | NEW `internal/cce/bridge.go` |
| 44 | k6 load matrix comprehensive re-run (500 RPS, 1000 tenants, all payment + channel + agent paths) | CF-6 | `test/k6/`, `monitoring/` |
| 45 | Lighthouse 6-page ≥ 90 re-capture (all pages: home, products, checkout, admin, operator-alerts, margin-dashboard) | CF-6 | `agentic-ecommerce-web` |

**v4.9.1 QA** focus:

- Compliance audit (GDPR Article 17 right-to-delete E2E, CCPA data export)
- Data residency validation (migration 002x+, region-pinned queries, storage bucket routing)
- 1000-tenant EXPLAIN ANALYZE on critical hot-path queries (GMV, ROI, channel content, operator alerts)
- CCE sidecar health check + latency benchmark (CCE vs Go-native LLM calls)
- Memory footprint validation (CCE sidecar under sustained load)
- Final `govulncheck` + race test suite + sentrux gate

---

#### Sprint Pair 10: v5.0.0 Release Coordination

**v5.0.0 Release** (5 activities):

| # | Activity | Description |
|---|----------|-------------|
| R1 | Full validation matrix | k6 load (1000 RPS, p99 < 200ms), Lighthouse 6-page (all ≥ 90), race tests, govulncheck, sentrux `complex_fn=4` gate |
| R2 | 1000-tenant EXPLAIN ANALYZE | Critical queries: GMV rollup, ROI heatmap, channel content, operator alerts, payment saga, marketplace vendor |
| R3 | CHANGELOG v5.0.0 | Comprehensive entry covering v4.1.0 through v4.9.1 (9 MVP sprints, 45 stories) |
| R4 | ADR-031 draft | v5.0.0 release decisions + v6 candidate set (BNPL expansion, Alipay+ Global, replay harness production capture, self-testing Temporal expansion) |
| R5 | Git tag v5.0.0 + GitHub releases | 32+ cross-compiled binaries + Flutter app bundle + frontend tarball + SHA256SUMS |

---

## Carry-Forward Resolution

### ADR-029 v4.1.x Carry-Forwards (Decision 3)

| CF | Description | Resolved In | Story # |
|----|-------------|-------------|---------|
| CF-1 | Live Stripe webhook + refund flow validation | **v4.1.0 MVP** | #4 |
| CF-2 | Live AusPost eParcel + DHL Express (production keys) | **v4.1.0 MVP** | #5 |
| CF-3 | 1688/Taobao live API production scaling | **v4.6.0 MVP** | #29 |
| CF-4 | Carrier webhook signing keys rotation policy | **v4.6.0 MVP** | #30 |
| CF-5 | Frontend v3.9.1 surfaces (onboarding wizard, alert centre, content analytics) | **v4.6.0 MVP** (wizard #28) + **v4.4.1 QA** (alert centre E2E via IC-4) | #28, IC-4 |
| CF-6 | chromedp dynamic-JS 1688 client | **Subsumed** into v4.6.0 1688/Taobao live API scaling (#29) | #29 |

### ADR-029 Beyond-v5 Carry-Forwards (Decision 4)

| Item | Description | Resolution |
|------|-------------|------------|
| D4-1 | Full MADRL multi-agent coordination | **Promoted** to v4.7.0 MVP (#31, #32) |
| D4-2 | Self-testing Temporal loop expansion | **Deferred** to v6 (ADR-031 candidate) |
| D4-3 | Per-tenant data residency | **Promoted** to v4.9.0 MVP (#41) |
| D4-4 | Real-time per-tenant observability | **Promoted** to v4.7.0 MVP (#33, #34) |
| D4-5 | Replay harness production capture | **Deferred** to v6 (ADR-031 candidate) |

### QA Improvement Candidates (IC-1 through IC-9)

| IC | Description | Priority | Resolved In |
|----|-------------|----------|-------------|
| IC-1 | RLS policies for migrations 0012–0025 | HIGH | **v4.1.0 MVP** (#1) |
| IC-2 | Backfill `IF NOT EXISTS` on 6 indexes in migration 0001 | LOW | **v4.1.1 QA** |
| IC-3 | Convert ~15 operational `fmt.Errorf` to sentinel errors with `%w` | LOW | **v4.1.1 QA** |
| IC-4 | Add `operator-alerts.spec.ts` E2E test | MEDIUM | **v4.4.1 QA** |
| IC-5 | Grafana dashboard panels for v3.5.0–v3.9.1 metrics | MEDIUM | **v4.2.1 QA** |
| IC-6 | Prometheus alert rules for OOM, pricing guardrails, CAPTCHA | MEDIUM | **v4.3.1 QA** |
| IC-7 | `context.WithTimeout` on Temporal HTTP activities | MEDIUM | **v4.1.0 MVP** (#2) |
| IC-8 | Postgres `statement_timeout` GUC | MEDIUM | **v4.1.0 MVP** (#3) |
| IC-9 | Expose Redis pool sizing as environment variables | LOW | **v4.1.1 QA** |

### ADR-029 v5 Candidates (Decision 5)

| V5 | Description | Resolved In | Story # |
|----|-------------|-------------|---------|
| V5-1 | Coaching bounded context | **v4.8.0 MVP** | #37 |
| V5-2 | Python CCE sidecar evaluation | **v4.9.0 MVP** | #43 |
| V5-3 | Flutter admin mobile app | **v4.8.0 MVP** | #36 |
| V5-4 | Per-tenant data residency | **v4.9.0 MVP** | #41 |
| V5-5 | Full marketplace developer ecosystem | **v4.8.0 MVP** | #38, #39, #40 |
| V5-6 | MADRL coordination expansion | **v4.7.0 MVP** | #31, #32 |
| V5-7 | Real-time per-tenant observability | **v4.7.0 MVP** | #33, #34 |

---

## Dependency Chain

```
v4.1.0 carry-forwards ──► v4.2.0 payment foundation
  (Stripe validation unblocks adapter architecture)

PaymentGateway port (v4.2.0) ──► All payment adapters (v4.3.0)
                              ──► Payment Saga (v4.2.0)
                              ──► AI Advisor (v4.3.0)

v4.4.0 GKE cluster ──► Helm charts ──► KEDA (v4.4.0)
                    ──► OTel Collector (v4.5.0)
                    ──► NetworkPolicy (v4.5.0)
                    ──► EKS DR (v4.7.0)

OTel SDK (v4.5.0) ──► OTel Collector ──► Cloud Trace

Go 1.26 (v4.5.0) ──► All Docker image rebuilds (v4.5.0+)

IG + Pinterest stubs (v3.9.1) ──► Full implementation (v4.6.0)

MADRL seed (v3.5.1) ──► Coordination expansion (v4.7.0)

v1 API (stable through v4.x) ──► Flutter admin app (v4.8.0)

Tenant aggregate ──► Data residency (v4.9.0)
                 ──► Compliance layer (v4.9.0)
```

---

## Consequences

### What this roadmap commits to

1. **45 MVP stories + 5 release activities** across 10 sprint pairs
   (20 sprints), maintaining the proven cadence
2. **Two new bounded contexts**: `payment` (v4.2.0) and `coaching`
   (v4.8.0), plus promotion of `marketplace` (v4.8.0) from SDK
   lineage to full bounded context
3. **Multi-provider payment infrastructure**: Stripe, Alipay, WeChat
   Pay, PayPal adapters + Temporal Payment Saga + AI advisor
4. **Kubernetes-native runtime**: GKE Autopilot (primary) + EKS Auto
   Mode (DR) with Helm charts, KEDA, and full OTel observability
5. **All 9 QA improvement candidates** resolved by v4.4.1
6. **All 6 ADR-029 carry-forwards** resolved by v4.6.0
7. **All 7 ADR-029 v5 candidates** landed by v4.9.0
8. **3 of 5 beyond-v5 items** promoted into v4.x scope
9. **Sentrux `complex_fn=4` HARD GATE** maintained through v5.0.0

### What this roadmap defers to v6 (ADR-031)

1. **BNPL expansion** (Afterpay/Klarna as standalone adapters beyond
   Stripe pass-through) — Phase 2 candidate #12
2. **Alipay+ Global SDK** (DANA/GCash/TrueMoney cross-border) —
   Phase 2 candidate #18
3. **Self-testing Temporal loop expansion** — ADR-029 D4-2
4. **Replay harness production capture pipeline** — ADR-029 D4-5
5. **GitOps CI/CD pipeline** (Artifact Registry → Helm deploy with
   staging → prod promotion) — Phase 2 candidate #27; manual
   `helm upgrade` suffices through v5.0.0
6. **Plugin marketplace storefront** — V5-5 ships SDK + sandbox +
   submission workflow; public storefront deferred

### New go.mod dependencies (3)

| Dependency | Version | Sprint | Justification |
|-----------|---------|--------|---------------|
| `github.com/stripe/stripe-go/v82` | latest | v4.2.0 | Production Stripe adapter |
| `github.com/go-pay/gopay` | latest | v4.2.0 | Alipay, WeChat Pay, PayPal via single SDK (5.5k stars, Apache 2.0) |
| `go.opentelemetry.io/otel` | v1.x | v4.5.0 | Distributed tracing + OTel metrics |

---

## Risks

| # | Risk | Likelihood | Impact | Mitigation |
|---|------|-----------|--------|------------|
| R1 | **Alipay/WeChat merchant account approval delay** (2–4 weeks) | High | High | Start application during v4.2.0 coding; sandbox-first development; adapter interface allows mock testing independent of provider approval |
| R2 | **gopay SDK stability** for production payment volumes | Medium | Medium | Pin version; wrap with circuit breaker from existing resilience pillar; fallback to raw HTTP if SDK proves unstable |
| R3 | **GKE Autopilot cold-start** for long-running agent-worker pods | Medium | Medium | `minReplicas=1` HPA to keep warm pod; KEDA ScaledObject with `idleReplicaCount: 1` |
| R4 | **Payment Saga complexity** vs `complex_fn=4` gate | Medium | Medium | Decompose saga into thin orchestrator + per-step activity functions; each < 75 LOC; reuse workerpool pattern |
| R5 | **Go 1.26 availability** (if delayed past v4.5.0 window) | Low | Low | v4.5.0 MVP story #23 is conditional; keep Go 1.25 if 1.26 is not GA; no blocking dependency |
| R6 | **Terraform state drift** between Cloud Run (staging) and GKE (production) | Medium | High | Separate state backends per root module; environment isolation via Terraform workspace |
| R7 | **Flutter SDK maturity** for admin dashboard use case | Low | Medium | Flutter admin consumes stable v1 API; no backend changes required; can downscope to PWA if Flutter proves inadequate |

---

## Dependencies

### External provider accounts

| Provider | Needed By | Lead Time | Status |
|----------|-----------|-----------|--------|
| Stripe production keys | v4.1.0 | 1–2 days | Sandbox exists |
| AusPost eParcel production | v4.1.0 | 1–2 weeks | Sandbox exists |
| DHL Express production | v4.1.0 | 1–2 weeks | Sandbox exists |
| Alipay merchant account | v4.2.0 | 2–4 weeks | Not started |
| WeChat Pay merchant account | v4.2.0 | 2–4 weeks | Not started |
| PayPal developer account | v4.3.0 | 1–2 days | Not started |
| GKE Autopilot quota | v4.4.0 | 1 week | GCP project exists |

### Toolchain

| Tool | Needed By | Availability |
|------|-----------|-------------|
| Go 1.26 | v4.5.0 | Conditional (GA date TBD) |
| Next.js 16 | v4.5.0 | Available (16.2.6) |
| Flutter SDK | v4.8.0 | Available (stable) |
| KEDA operator | v4.4.0 | Available (v2.x) |
| OTel Collector | v4.5.0 | Available (v0.x) |

---

## Summary Statistics

| Metric | Value |
|--------|-------|
| Total sprint pairs | **10** (20 sprints) |
| Total MVP stories | **45** |
| Total release activities | **5** |
| **Total items** | **50** |
| ADR-029 carry-forwards resolved | **6/6** (by v4.6.0) |
| QA improvement candidates resolved | **9/9** (by v4.4.1) |
| ADR-029 v5 candidates resolved | **7/7** (by v4.9.0) |
| ADR-029 beyond-v5 promoted | **3/5** |
| Items deferred to v6 | **6** |
| New bounded contexts | **3** (payment, coaching, marketplace) |
| New go.mod dependencies | **3** |
| New Temporal workflows | **2** (payment_saga, vendor_onboarding) |

---

*ADR-030 produced by Phase 3 of EC v4 QA + v5 Roadmap plan.
Pending operator approval before sprint execution begins.*
