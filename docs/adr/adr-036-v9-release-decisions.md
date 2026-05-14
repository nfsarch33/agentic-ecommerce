# ADR-036: v9.0.0 Release Decisions

**Status**: Accepted  
**Date**: 2026-05-14  
**Context**: v9 platform-baseline release work after backend and frontend v8.0.0 publication.

## Context

The post-v8 program promotes Agentic Ecommerce from a feature-heavy v8 release
into a platform-baseline release path. The backend owns the current-release
metadata guard, OpenAPI version policy, release checklist, release-final
evidence chain, and the release-facing API, Temporal, and webhook contract
docs. The broader program also standardizes `win1/wsl1` as `primary-testing`,
keeps `win2/wsl2` in standby until controller activation gates pass, and treats
GKE/GCP as the staging environment while AWS stays parity-only.

## Decisions

1. Publish backend `v9.0.0` only after release metadata, ADR links, changelog,
   checklist, and final evidence docs are aligned and guarded by
   `TestV900ReleaseMetadataAligned`.
2. Keep OpenAPI v1 endpoints stable through host v9.x; v2 preview endpoints
   remain opt-in and explicitly unstable.
3. Treat `win1/wsl1` as the merge-blocking `primary-testing` environment for
   backend integration, release smoke, and cleanup evidence until another pool
   satisfies the controller activation gates.
4. Treat GKE/GCP as the authoritative staging environment for v9 release
   validation. Keep AWS parity-only and dry-run only until GKE staging is
   stable, observable, and rollback-tested.
5. Treat controller provenance, staging-smoke evidence, and release handoff
   artifacts as release-gating evidence, not optional documentation.
6. Seed v10 only after backend, frontend, tooling, and global-kb v9 release
   sync work is merged and the release worktrees are clean.

## Consequences

- Release notes must identify the primary-testing contract, the staging
  environment, and any secondary-testing carry-forwards.
- Future release branches must start with a RED metadata guard test before
  current-release docs or version files change.
- Live marketplace, payment, carrier, social, and remote-vision execution
  remain credential or resource gated until explicit operator evidence exists.
