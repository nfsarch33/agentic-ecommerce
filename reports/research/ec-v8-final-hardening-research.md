# EC v8 Final Hardening Research

> Date: 2026-05-13  
> Branch: `release/v8-final-hardening`  
> Scope: backend v8 release metadata, ADR/changelog/release checklist, release
> evidence, and release guard tests.

## Sources Reviewed

- Active roadmap:
  - `global-kb:backlog/ec-v8-10-pair-roadmap.md`
  - `global-kb:handoff/2026-05-13-ec-v8-p09-self-improvement-handoff.md`
- Backend release state:
  - latest backend tag: `v7.5.1`
  - latest backend main: `6a31e5b`
  - no backend `v8*` tag exists
  - backend metadata still says `7.5.1`
- Frontend release state:
  - latest frontend tag: `v7.5.1`
  - latest frontend main includes v8 media UX PRs #69 and #70
  - frontend package/release checklist metadata is stale and must be repaired
    in the frontend release branch before final v8 publication

## Backend v8 Scope

The backend v8 release includes ten executed v8 pairs:

1. Marketplace sync core
2. Shopify adapter
3. Shopee adapter
4. Product image editing
5. Frontend UX media (frontend repo)
6. Temporal orchestration
7. Docsync automation (cursor-tools)
8. OOM observability
9. Self-improvement
10. Final hardening and release

## Decisions

1. Public backend release target is `v8.0.0`.
2. Add an in-repo release metadata guard so stale VERSION/OpenAPI/README/
   checklist/ADR links fail in future release branches.
3. Keep OpenAPI v1 stable through host v8.x; v2 preview remains opt-in.
4. Publish frontend `v8.0.0` only after frontend metadata repair and QA pass.
5. Keep Git KB as durable truth while Mem0 remains degraded.

## RED Target

`TestV800ReleaseMetadataAligned` should fail until backend release metadata
points at `8.0.0`, ADR-035 exists, and the release checklist references the v8
release evidence.
