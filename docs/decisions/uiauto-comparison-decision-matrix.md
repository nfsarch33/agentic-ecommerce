# Decision Matrix: uiauto vs Playwright Comparison Framework (v4.14.0)

Date: 2026-05-11
Status: Active
Owner: nfsarch33
Sprint: v4.14.0 MVP (Pair 14 of 20)
Implements: ADR-030 v5 roadmap "Pair 14: uiauto vs Playwright comparison harness"
Depends on: `docs/decisions/uiauto-tier-2-promotion.md` (v3.7.1 Tier 2 GO decision)

## Purpose

This decision matrix formalises the methodology for comparing the uiauto
stack (OmniParser bridge + self-healing MemberAgent) against Playwright
(deterministic selector-based E2E) to inform Tier 2 promotion decisions.

The framework established in v4.14.0 (`internal/uiauto/compare/`) runs
both tools against identical scenarios and produces quantitative metrics
that feed into this matrix.

## Weight Factors

| Factor | Weight | Rationale |
|---|---:|---|
| **Accuracy** | 40% | Correct assertions are the primary quality signal. A tool that fails assertions cannot be trusted in production CI. |
| **Speed** | 30% | Faster feedback loops improve developer productivity. uiauto's VLM inference adds latency vs Playwright's direct DOM access. |
| **Maintenance burden** | 20% | Selector drift, self-heal complexity, and infrastructure requirements affect long-term ownership cost. |
| **CI compatibility** | 10% | Hermetic CI (no Docker, no Chrome, no LLM) is required for the default `make test` path; gated builds are acceptable for extended validation. |

## Scoring Rubric (1-5 per factor)

| Score | Accuracy | Speed | Maintenance | CI Compat |
|---|---|---|---|---|
| **5** | >98% assertion pass rate | <500ms avg scenario | Zero-touch selectors (self-healing covers all drift) | Runs in default `make test` with no infra deps |
| **4** | 95-98% pass rate | 500-1000ms avg | Quarterly selector review only | Runs with build tag gating only |
| **3** | 90-95% pass rate | 1-2s avg | Monthly selector review | Requires Docker but no external LLM |
| **2** | 80-90% pass rate | 2-5s avg | Weekly selector review | Requires external LLM endpoint |
| **1** | <80% pass rate | >5s avg | Daily selector fixes | Cannot run in CI without manual setup |

## Current Scores (based on v3.7.0-v4.13.0 evidence)

### Playwright

| Factor | Score | Evidence |
|---|---:|---|
| Accuracy | **5** | 100% pass rate on all 22 E2E specs in `agentic-ecommerce-web/e2e/`. Deterministic selectors; no flake in last 13 sprints. |
| Speed | **4** | Avg 800ms per scenario (Chromium headless). Sub-500ms for simple navigation; 1-2s for SSE/WebSocket scenarios. |
| Maintenance | **3** | Selector updates required when frontend components change. Monthly effort ~2h across 22 specs. |
| CI compatibility | **5** | Runs in default CI via `npx playwright test`. Docker optional (Chromium bundled). No LLM dependency. |

**Playwright weighted score**: (5×0.40) + (4×0.30) + (3×0.20) + (5×0.10) = 2.0 + 1.2 + 0.6 + 0.5 = **4.30**

### uiauto (OmniParser bridge + MemberAgent v3)

| Factor | Score | Evidence |
|---|---:|---|
| Accuracy | **4** | 95%+ pass rate on replay cassettes (rednote, tiktok, captcha). Self-heal covers tier-light drift; tier-smart handles structural changes. Some false positives on dynamic content. |
| Speed | **3** | Avg 1.2s per scenario via OmniParser bridge (VLM inference adds ~400ms over Playwright). Batch-5 scenarios at 4-concurrent cap. |
| Maintenance | **4** | Self-healing reduces selector maintenance to quarterly review. Replay cassettes (YAML) need update only when scenarios change, not when DOM drifts. |
| CI compatibility | **2** | Requires OmniParser bridge (`EC_OMNIPARSER_BRIDGE_URL`) on WSL fleet. Build-tag gated (`v4140_uiauto_compare`). Cannot run in default `make test`. |

**uiauto weighted score**: (4×0.40) + (3×0.30) + (4×0.20) + (2×0.10) = 1.6 + 0.9 + 0.8 + 0.2 = **3.50**

## Decision Threshold

The promotion threshold is:

> If uiauto scores >= 80% of Playwright's weighted score, promote to Tier 2 default.

Current ratio: **3.50 / 4.30 = 81.4%** -- **ABOVE threshold**.

## Sample Data (from existing replay cassettes)

| Scenario | uiauto Result | Playwright Result | uiauto (ms) | PW (ms) | Agreement |
|---|---|---|---:|---:|:---:|
| RedNote post creation | pass | pass | 1450 | 920 | yes |
| TikTok video upload | pass | pass | 1680 | 1100 | yes |
| CAPTCHA encounter | pass (self-heal) | pass | 2100 | 850 | yes |

Self-heal events in the CAPTCHA scenario: 2 tier promotions (light→smart→vlm).

## Recommendation

**Proceed with phased Tier 2 integration**, consistent with the v3.7.1
Tier 2 GO decision. The comparison framework established in v4.14.0
provides ongoing quantitative evidence to validate this decision.

### Action items

1. **v4.14.0** (this sprint): Ship the comparison runner, metrics, and
   5 scenario definitions. Establish Prometheus + Grafana monitoring.
2. **v4.15.0+**: Run comparison scenarios in CI (gated build) on every
   PR. Track agreement rate trend via the Grafana dashboard.
3. **v5.0.0 release gate**: Agreement rate must be >= 90% across all
   5 scenarios. Speed ratio must be <= 2.0 (uiauto no more than 2x
   slower than Playwright).
4. **Re-evaluate quarterly**: Re-run the decision matrix with updated
   scores. If uiauto drops below 80% of Playwright's weighted score,
   pause Tier 2 promotion and investigate.

## Methodology for Future Re-evaluation

1. Run all 5 comparison scenarios via `go test -tags v4140_uiauto_compare`.
2. Collect `AggregateMetrics` from the test output.
3. Map aggregate metrics to the scoring rubric above.
4. Recompute weighted scores and ratio.
5. Update this document with the new scores and date.

## References

- `docs/decisions/uiauto-tier-2-promotion.md` -- v3.7.1 Tier 2 GO decision
- `internal/uiauto/compare/` -- comparison framework source
- `monitoring/grafana/uiauto-comparison.json` -- Grafana dashboard
- `tests/uiauto/compare/` -- scenario definitions
- ADR-030 v5 roadmap (Pair 14 scope)
