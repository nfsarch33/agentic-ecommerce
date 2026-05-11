# ADR-033: v6.6.0 Release Decisions + v7 Preview

**Status**: Accepted
**Date**: 2026-05-11
**Supersedes**: ADR-032 carry-forward plan for v6.1.x
**Authors**: Jason Lian (nfsarch33)

## Context

ADR-032 shipped v6.0.0 with ten explicit carry-forwards. The v6.1.x -> v6.6.0
cycle executed as six compressed sprint pairs to drain the highest-risk quality,
performance, documentation, and self-improvement items before any v7 feature
work.

The release used the existing MVP -> QA/Retro cadence. Each completed pair wrote
a global-kb capsule and retro, kept the active plan read-only, and merged code
through runx-mediated PRs.

## Decision 1: Ship v6.6.0 as a minor cleanup release

v6.6.0 ships as a minor release because it changes quality gates, release
metadata, metrics, docs, and validation evidence without introducing a new
domain feature surface.

Completed carry-forwards:

- macOS heap-ceiling flake fixed.
- Postgres idempotency store added.
- `ErrInvalidFXRate` typed sentinel extracted.
- Agentrace production NDJSON adapter wired behind runx-alias transport guards.
- JWT key-version rotation support added.
- memwatch request budgets and adaptive RSS ceilings deepened.
- Postgres FAQ store added.
- Real Postgres benchmark distributions published.
- Full k6 matrix executed and documented.
- Temporal daily GMV refresh scheduled.
- `cursor-tools docsync` and `runx docs` shipped.
- Supplier-cost threshold semantics reconciled.
- Frontend Lighthouse Performance >=90 closed.
- Frontend dynamic-page SEO >=90 closed.
- Cross-cycle KPI dashboard added.

## Decision 2: Do not pretend the structural backend targets are closed

Three backend quality targets remain open and are explicitly carried to v7
planning rather than silently waived:

1. Sentrux Quality >7000. Latest durable backend quality remains around 6043.
   Closing this requires a structural package-boundary refactor, not release
   metadata work.
2. Coverage >=85%. Latest durable coverage remains 84.8%, roughly 0.2
   percentage points short.
3. `complex_fn <=4`. Latest durable count remains 5.

These are release notes and v7 backlog items, not hidden failures.

## Decision 3: External-account dependencies stay blocked

Live Alipay, WeChat Pay, AusPost, and DHL production accounts are still blocked
on external merchant/carrier provisioning. The release keeps the sandbox
adapters, readiness docs, and envelope tests, but does not claim live production
account validation.

## Decision 4: Live OmniParser/uiauto stays deferred to v7.x

The frontend uiauto-vs-Playwright comparison remains documentation-backed in
v6.6.0. Live OmniParser comparison is deferred until the bridge alias is
registered and remote inference routing is proven safe. No VLM workload should
run on the MacBook release host.

## v7 Preview Candidates

v7 planning should start only after v6.6.0 tags and handoff are complete. The
first 15 MVP/QA pairs should prioritize:

1. Backend package-boundary refactor for Sentrux Quality >7000.
2. Coverage closure for the remaining 85% gap.
3. `complex_fn` reduction from 5 to <=4.
4. Machine-readable KPI/metric emission for EvoMap and EvoLoop/DRL ingestion.
5. Route-matrix manifest shared by Lighthouse, Playwright, uiauto, and docs.
6. React warning hygiene and hydration mismatch failure reporting.
7. Remote uiauto/OmniParser execution through approved worker routing.
8. Cloud deployability hardening for AWS and GCP.
9. Resource-aware orchestration and autoscaling.
10. Marketplace sync and plugin lifecycle hardening.
11. Content/SEO automation and dynamic-page metadata expansion.
12. Pricing, fulfilment, and coordination-loop improvements.
13. Customer-service and analytics dashboards.
14. Security/compliance release hardening.
15. Data/BI and self-improvement feedback loops.

## Consequences

- v6.6.0 is honest about what closed and what remains.
- Frontend performance and SEO carry-forwards are closed with evidence.
- Backend structural quality work remains visible for v7 rather than being
  buried in release notes.
- Future releases should convert the Markdown KPI dashboard into NDJSON or
  Prometheus-ready artifacts so self-improvement loops do not parse prose.
