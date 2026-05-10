# ADR-032: v6.0.0 Release Decisions

**Status**: Accepted  
**Date**: 2026-05-11  
**Supersedes**: ADR-031  
**Authors**: Jason Lian (nfsarch33)

## Context

The v5.1.x to v6.0.0 cycle completed 10 sprint pairs focused exclusively
on quality, refactoring, performance, and documentation. No new domain
features were introduced. The cycle goal was to polish the v5.0.0
production stack into a hardened, well-documented, and performant release.

## Decision 1: Ship v6.0.0 as quality/refactoring/performance release

v6.0.0 ships with:
- Code deduplication (shared httpclient, webhook verifier, payment errors)
- Temporal workflow + eventbus consolidation (55 event types, 10 workflows)
- Performance gains (PG pool tuning, Redis 3.2x pipeline, HTTP/2)
- Frontend performance (bundle analysis, lazy loading, SWR caching)
- Comprehensive QA (0 vulns, 0 flaky tests, 56 E2E specs)
- Documentation (19 ops docs, 124 OpenAPI ops, 32 ADRs, CONTRIBUTING)
- EvoMap self-improvement analysis (38 capsules, 5 recommendations)

All 99 backend test packages pass with `-race`. All 1082 frontend tests
pass. Zero hook bypasses across all 10 pairs. Sentrux gate shows no
degradation.

## Decision 2: Carry-forwards locked for v6.1.x

The following items are explicitly deferred to v6.1.x and will not block
the v6.0.0 release:

1. **Sentrux Quality >7000 recovery** -- Currently 6035. Recovery requires
   architectural work (splitting large packages, reducing coupling further).
   The v5.x cycle improved coupling from 0.09 to 0.06 but quality score
   did not recover due to codebase growth.

2. **Coverage 85%+** -- Currently ~83.4%. The gap is in `cmd/*` main
   functions which are difficult to unit-test without integration
   infrastructure. Needs main-function refactoring or integration test
   harness.

3. **Live Alipay/WeChat merchant accounts** -- Sandbox adapters are
   validated and functional. Production requires merchant approval from
   Ant Group / Tencent which is an external dependency.

4. **Live carrier API integration** -- AusPost eParcel and DHL Express
   sandbox adapters work. Production accounts require business contracts.

5. **Agentrace production wiring** -- The adapter is built and tested but
   the hooks pipeline connection requires the IronClaw Agentrace service
   to be deployed and reachable.

6. **JWT secret key rotation** -- Current implementation uses static
   secrets. Rotation needs a key-versioning scheme and graceful rollover.

7. **Live uiauto comparison via OmniParser bridge** -- The compare
   harness works with mocked responses. Live comparison needs the
   OmniParser service running on the WSL GPU fleet.

8. **k6 full load test execution** -- Script is written and validated
   syntactically. Execution requires a running backend instance with
   populated test data.

9. **Lighthouse Performance >=90** -- Currently 68-95 range depending on
   page. Requires code-splitting optimization and server-side rendering
   improvements for heavy dashboard pages.

10. **Frontend SEO >=90 on inner pages** -- Currently 63 on dynamic
    pages. Requires metadata generation and structured data markup.

## Decision 3: v6.1.x preview candidates

From EvoMap self-improvement recommendations (Pair 7), the following are
candidates for v6.1.x:

1. Package splitting for Sentrux quality recovery (target: Quality >7000)
2. Integration test harness for cmd/* binaries (target: coverage 85%+)
3. Key rotation middleware for JWT secrets
4. Agentrace hook pipeline connection
5. Performance budget enforcement in CI (Lighthouse >=90)
6. SEO metadata generation for dynamic pages
7. Rate limiter Redis cluster mode support

## Consequences

- v6.0.0 ships as a stable, well-tested, documented release
- No functional regressions from v5.0.0 (all features preserved)
- Performance improvements are measurable (Redis 3.2x, pool tuning)
- Technical debt reduced (code dedup, consolidation)
- Clear roadmap for v6.1.x work items
- Zero hook bypasses demonstrates process maturity
