# ADR-034: v7.5.1 Release Decisions

**Status**: Accepted  
**Date**: 2026-05-12  
**Context**: v7 Pair 1 through Pair 6 QA shipped on backend `main` after the v6.6.0 cleanup release.

## Context

The backend has shipped six v7 MVP/QA pairs after `v6.6.0`:

- Pair 1: quality foundation and structural regression QA.
- Pair 2: coverage harness and Temporal activity tracing QA.
- Pair 3: observability spine and EvoMap/Agentrace replay QA.
- Pair 4: resource-aware orchestration and OOM/leak QA.
- Pair 5: cloud deployability and Terraform plan/rollback QA.
- Pair 6: adapter hardening and sandbox boundary QA.

No public `v7.0.0` tag was created before Pair 6 QA. Releasing the current
backend head as `v7.5.1` is more honest than back-tagging an older commit as
`v7.0.0` after additional v7 work has already shipped.

## Decisions

1. Publish backend `v7.5.1` from current `main` after release metadata and
   final release gates pass.
2. Keep this release backend-focused. The frontend receives a separate
   metadata/QA sync release because it has no post-`v6.6.0` feature delta.
3. Keep v1 OpenAPI endpoints stable through host v7.x; preview v2 surfaces
   remain opt-in.
4. Treat live payment, carrier, social, and OmniParser execution as
   operator-gated until external credentials and remote resource routing exist.
5. Continue the remaining v7 roadmap from Pair 7 onward using explicit pair
   branch labels rather than ambiguous pseudo-semver names for Pair 10+.

## Consequences

- Release notes must explain that `v7.5.1` includes v7 Pair 1 through Pair 6
  QA and supersedes the untagged internal v7.0.0-v7.5.0 pair labels.
- The next active implementation starts at Pair 7 Marketplace and sync after
  the `v7.5.1` release sync lands in global-kb.
- Future major release planning should tag public releases before the internal
  pair labels move beyond the intended public version.
