# ADR-035: v8.0.0 Release Decisions

**Status**: Accepted  
**Date**: 2026-05-13  
**Context**: v8 TDD implementation run after backend and frontend v7.5.1 publication.

## Context

The v8 run implemented ten MVP/QA pairs across the EC stack. The backend owns
marketplace sync core, Shopify and Shopee connectors, product image editing
contracts, Temporal orchestration, OOM observability, self-improvement evidence,
and final release-hardening metadata. Frontend media UX and tooling docsync work
are released from their owning repositories, then summarized in global-kb so the
stack can be resumed without losing cross-repo context.

## Decisions

1. Publish backend `v8.0.0` only after release metadata, ADR links, changelog,
   checklist, and final evidence docs are aligned and guarded by
   `TestV800ReleaseMetadataAligned`.
2. Keep OpenAPI v1 endpoints stable through host v8.x; v2 preview endpoints
   remain opt-in and explicitly unstable.
3. Keep Shopify and Shopee integrations behind shared marketplace ports and
   cassette/mock boundaries until operator credentials and sandbox approvals
   exist.
4. Route image editing, OmniParser, and VLM-heavy flows through approved remote
   aliases; do not run those workloads on the MacBook.
5. Treat Agenttrace, EvoMap, and EvoLoop/DRL reward artifacts as release
   evidence, not autonomous promotion authority. Promotion still requires
   reproducible tests and operator-reviewed evidence.
6. Seed v9 from the global-kb handoff after backend, frontend, tooling, and KB
   release sync PRs are merged and worktrees are clean.

## Consequences

- Release notes must identify which v8 pairs were backend-owned and which were
  frontend/tooling/global-kb-owned.
- Future release branches must start with RED metadata guard tests before
  changing release files.
- Live marketplace, payment, carrier, social, and remote vision execution remain
  credential/resource gated until explicit operator evidence exists.
