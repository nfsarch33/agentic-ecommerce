# EC v9.0.0 Release Final Evidence

**Date**: 2026-05-14  
**Release**: `agentic-ecommerce` v9.0.0  
**Base**: v8.0.0 release tag  
**Head**: pending final v9 release merge commit

## Scope

v9.0.0 establishes the backend platform baseline for the post-v8 program:

- Controller-accurate release metadata, version policy, checklist, and ADR
  coverage guarded by `TestV900ReleaseMetadataAligned`.
- `win1/wsl1` as the merge-blocking `primary-testing` contract for backend
  integration, smoke, and cleanup evidence.
- GKE/GCP as the authoritative staging environment through
  `deploy/terraform/gke`, `deploy/helm/agentic-ecommerce`, and the checked-in
  observability surfaces under `deploy/otel`.
- Release-facing API, Temporal, and webhook docs aligned with the v9 current
  release baseline instead of the legacy v2.0.0 release label.
- Support-tool provenance, staging-smoke evidence, and cross-repo handoff docs
  captured before any semver-only `v9.0.0` tag is cut.

## Release Gates

Required backend release gates for this branch:

- `runx git diff --repo ecommerce -- --check`
- `runx docs check --repo ecommerce`
- `runx shell-leak-scan --repo ecommerce`
- `runx go test --repo ecommerce -- -race -p 1 -count=1 ./...`
- `runx make --repo ecommerce -- coverage-check`
- `runx make --repo ecommerce -- govulncheck-scan`
- `runx make --repo ecommerce -- build`
- `runx make --repo ecommerce -- contract-test`
- `runx make --repo ecommerce -- compose-temporal-config`
- `runx make --repo ecommerce -- n8n-workflows-validate`
- `runx make --repo ecommerce -- monitoring-validate`
- `runx make --repo ecommerce -- compose-config-prod`
- `runx make --repo ecommerce -- tf-fmt-check`
- `runx make --repo ecommerce -- tf-validate`
- `runx make --repo ecommerce -- tf-plan-contract`
- `runx worktree run --repo ecommerce --branch release/v9-platform-baseline -- helm lint deploy/helm/agentic-ecommerce`
- `runx sentrux gate --repo ecommerce`
- `runx test-lane run --lane staging-smoke --pool primary-testing`
- `runx test-lane run --lane staging-rollback --pool primary-testing`

Focused release metadata guard:

- `runx worktree run --repo ecommerce --branch release/v9-platform-baseline -- go test ./internal/qa -run TestV900ReleaseMetadataAligned -count=1`

## Carry-Forwards

- Frontend `v9.0.0` metadata and QA release must land before the stack-level
  global-kb release handoff is final.
- `secondary-testing` stays standby until `win2`, `win2-travel`, `wsl2`, and
  `wsl2-travel` all satisfy controller SSH, trust, cleanup, and resource-health
  gates.
- `EC_STAGING_BASE_URL`, controller provenance, and final release notes must be
  recorded before the `v9.0.0` tag is created.
- Live marketplace, payment, carrier, social, and remote vision execution
  remain operator-gated until external credentials, sandbox approvals, and
  remote-resource routing are available.
