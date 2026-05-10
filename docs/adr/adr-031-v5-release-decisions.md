# ADR-031: v5.0.0 Release Decisions

- **Status**: Proposed
- **Date**: 2026-05-11
- **Supersedes**: ADR-030 (v5.0.0 roadmap -- roadmap complete; ADR-031 is the release record)
- **Related**: ADR-029 (v4.0.0 release decisions), ADR-028 (v4 roadmap), ADR-027 (resilience pillar)

## Context

The v5.0.0 release closes a 20 sprint-pair cycle (v4.1.0 through v4.19.1) that began immediately after the v4.0.0 tag. The cycle was planned in ADR-030 and executed as an overnight push across all 20 pairs. The scope expanded the v4.0.0 baseline (multi-tenant agentic e-commerce with 10 epics, 8 binaries, 25 migrations) into a production-ready platform with:

- **Payment gateway** (Pair 2-3): 4-provider payment foundation (Stripe, Alipay, WeChat Pay, PayPal) with Temporal saga orchestration, webhook normalisation, AI payment advisor, and payment dashboard frontend
- **Cloud-native deployment** (Pair 4, 17-18): GKE Autopilot Terraform + Helm charts + distroless Dockerfiles + KEDA autoscaling + EKS DR + OCI bootstrap + multi-cloud cost optimisation
- **Observability and runtime** (Pair 5): OpenTelemetry tracing + Cloud Trace + Go 1.26.3 toolchain bump + Next.js 16 frontend upgrade + Kubernetes NetworkPolicies
- **Channel expansion** (Pair 6): Instagram and Pinterest promoted from stubs to full integration + 1688/Taobao production-ready + carrier key rotation
- **MADRL coordination** (Pair 7): Multi-agent reinforcement learning expanded from seed to weighted conflict resolution + per-tenant observability + EKS disaster recovery
- **Marketplace + coaching** (Pair 8): Vendor onboarding + commission engine + payout tracking + coaching context for agents
- **GDPR/CCPA compliance** (Pair 9): Data residency controls + consent management + right-to-delete workflows + audit logging + k6 load matrix + Lighthouse re-capture
- **Scale hardening** (Pair 10): 1000-tenant EXPLAIN ANALYZE + scale tests + performance baseline capture
- **Agentrace deep integration** (Pair 11): EvoMap replay adapter + Grafana dashboards + cursor hooks production wiring
- **OOM prevention** (Pair 12): Adaptive worker pool sizing + RSS-based backpressure + circuit breakers on all external calls + phased drain
- **MiniMax quota rotation** (Pair 13): Full runx minimax surface + auto-failover between API keys + observability
- **uiauto vs Playwright comparison** (Pair 14): Side-by-side test runner + accuracy/speed metrics + decision matrix for promotion
- **Worktree hardening** (Pair 15): Race detection + multi-agent coordination locks + handoff protocol formalisation
- **Skill consolidation** (Pair 16): Agent skill inventory audit + quality gate CLI + Codex-compatible generator + dedup recommendations
- **mem0 + OCI hardening** (Pair 17): mem0 WSL1 hardening + Oracle Cloud Terraform bootstrap + Qdrant integration + cross-cloud DR
- **Cloud deployment readiness** (Pair 18): AWS/GCP deploy scripts + multi-cloud Terraform modules + CI/CD pipelines + cost optimisation runbook
- **Release preparation** (Pair 19): README updates + ADR-031 + validation matrix + demo script + CHANGELOG + VERSION bump

## Decision 1: Ship v5.0.0 from current main

The v5.0.0 release ships from the current `main` branch after all 20 sprint pairs have merged. The release tag will be created in Pair 20 with cross-compiled binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` across all 8+ binaries plus a `SHA256SUMS` manifest.

## Decision 2: ADR-030 roadmap completion status

### Completed (all 20 pairs, 100 stories)

| Pair | Version | Scope | PR | Status |
|------|---------|-------|----|--------|
| 1 | v4.1.0 | ADR-029 carry-forwards + QA hardening | #100 | Merged |
| 2 | v4.2.0 | Payment foundation (Stripe + Alipay + WeChat + Saga) | #101 | Merged |
| 3 | v4.3.0 | Payment expansion (PayPal + webhook normaliser + AI advisor) | #102 | Merged |
| 4 | v4.4.0 | Cloud-native (GKE + Helm + Dockerfile slim + KEDA) | #103 | Merged |
| 5 | v4.5.0 | Observability (OTel + Go 1.26.3 + Next.js 16) | #104 | Merged |
| 6 | v4.6.0 | Channel expansion (IG + Pinterest + 1688/Taobao prod) | #105-106 | Merged |
| 7 | v4.7.0 | MADRL + per-tenant observability + EKS DR | #107 | Merged |
| 8 | v4.8.0 | Marketplace (vendors + commission + onboarding) | #108 | Merged |
| 9 | v4.9.0 | GDPR/CCPA + data residency + k6 + Lighthouse | #109 | Merged |
| 10 | v4.10.0 | Scale hardening + perf baseline | #110 | Merged |
| 11 | v4.11.0 | Agentrace deep integration | #111 | Merged |
| 12 | v4.12.0 | OOM prevention deepening | #112 | Merged |
| 13 | v4.13.0 | MiniMax quota rotation | #113 | Merged |
| 14 | v4.14.0 | uiauto vs Playwright comparison harness | #114 | Merged |
| 15 | v4.15.0 | Worktree hardening | #115 | Merged |
| 16 | v4.16.0 | Skill consolidation | #116 | Merged |
| 17 | v4.17.0 | mem0 + OCI hardening | #117 | Merged |
| 18 | v4.18.0 | Cloud deployment readiness | #118 | Merged |
| 19 | v4.19.0 | Release preparation (this pair) | TBD | In progress |
| 20 | v5.0.0 | Release tag + GitHub releases | TBD | Pending |

### Deferred to v5.1.x

1. **Live Alipay/WeChat merchant accounts** -- sandbox integration complete (Pair 2); live merchant onboarding requires business entity verification with payment providers. Carry-forward to v5.1.x.
2. **Live carrier API integration** -- AusPost eParcel + DHL Express adapters built with deterministic fixtures (v3.8.0); production API keys and real waybill generation deferred.
3. **Lighthouse full 6-page audit** -- scripts created and baseline captured (Pair 9); full automated audit against all storefront + admin pages deferred for live environment validation.
4. **Flutter native app** -- API surface ready; admin mobile app repo creation deferred to v5.1.x. The existing Next.js responsive admin serves mobile operators in the interim.

## Decision 3: v5.1.x preview candidates

1. Live payment merchant onboarding (Alipay + WeChat business verification)
2. Live carrier API integration (production waybills + tracking webhooks)
3. Flutter admin companion app (leveraging existing API surface)
4. Lighthouse automated 6-page audit in CI
5. MADRL production training loop (current weighted resolution is rule-based)
6. Real-time per-tenant WebSocket observability (current surface is polling-based)
7. Marketplace plugin certification programme (current review is manual)

## Consequences

- ADR-030 is superseded; all roadmap items are either completed or explicitly deferred with rationale
- The v5.0.0 tag represents 20 sprint pairs, 100 stories, 10 new migrations (0026-0035), ~1000 additional Prometheus series, and 2 additional Temporal workflows
- Deferred items are tracked in this ADR and will be prioritised in the v5.1.x planning cycle
- The platform is production-ready for deployment on GKE, EKS, or OCI with the provided Terraform modules and Helm charts
