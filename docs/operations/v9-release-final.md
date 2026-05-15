# EC v9.0.0 Release Final Evidence

**Date**: 2026-05-16  
**Release**: `agentic-ecommerce` v9.0.0  
**Base**: v8.0.0 release tag  
**Head**: pending final v9 release merge commit  
**Status**: RC metadata landed; semver tag still blocked

## Scope

v9.0.0 establishes the backend platform baseline for the post-v8 program:

- Controller-accurate release metadata, version policy, checklist, and ADR
  coverage guarded by `TestV900ReleaseMetadataAligned`.
- `primary-testing and secondary-testing` now form the mirrored self-hosted
  release contract for backend integration, full-stack E2E, cleanup, frontend
  stable Playwright, and UIAuto evidence.
- Current fleet truth is release-relevant: `node-a-travel`, `host-a-travel`, and
  `node-b-travel` are green from the controller; `node-b`, `host-b`, and
  `host-b-travel` still time out and therefore keep the stack in RC state.
- Cloud-native deployment material under `deploy/terraform`, `deploy/helm`, and
  `deploy/otel` remains reference-only for `v9.0.0`; it is maintained but no
  longer blocks the semver tag.
- Pair 5 bootstrap is landed behind `EC_AGENT_RUNTIME_MODE`: backend now pins
  `github.com/cloudwego/eino v0.8.13`, exposes an internal EINO adapter/runtime
  seam, and keeps `legacy` as the default business path while `shadow` and
  `primary` remain explicit opt-ins.
- Release-facing API, Temporal, and webhook docs aligned with the v9 current
  release baseline instead of the legacy v2.0.0 release label.
- Support-tool provenance, mirrored pool evidence, and cross-repo handoff docs
  captured before any semver-only `v9.0.0` tag is cut.

## Current Blocking Status

- Latest controller-side canaries: `node-a-travel` PASS, `host-a-travel` PASS,
  `node-b-travel` PASS, `node-b` FAIL (timeout), `host-b` FAIL (timeout),
  `host-b-travel` FAIL (timeout).
- `v9.0.0` remains RC-only until the secondary pool closes the direct and
  Windows SSH gaps and the mirrored release lanes pass on both pools.
- External-provider live execution remains operator-gated even after the
  self-hosted release gates are green.

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
- `runx sentrux gate --repo ecommerce`
- `runx ssh exec --target node-a-travel --cmd ssh-canary-wsl`
- `runx ssh exec --target host-a-travel --cmd ssh-canary-win`
- `runx ssh exec --target node-b-travel --cmd ssh-canary-wsl`
- `runx ssh exec --target node-b --cmd ssh-canary-wsl`
- `runx ssh exec --target host-b --cmd ssh-canary-win`
- `runx ssh exec --target host-b-travel --cmd ssh-canary-win`
- `runx test-lane run --lane backend-integration --pool primary-testing`
- `runx test-lane run --lane backend-integration --pool secondary-testing`
- `runx test-lane run --lane full-stack-e2e --pool primary-testing`
- `runx test-lane run --lane full-stack-e2e --pool secondary-testing`
- `runx test-lane run --lane cleanup-testing --pool primary-testing`
- `runx test-lane run --lane cleanup-testing --pool secondary-testing`

Cross-repo release dependency:

- Frontend `frontend-playwright-stable` and `frontend-uiauto-compare` must pass
  on both pools before the stack tag is cut. UIAuto evidence is mirrored and no
  longer advisory for `v9.0.0`.

Focused release metadata guard:

- `runx worktree run --repo ecommerce --branch release/v9-platform-baseline -- go test ./internal/qa -run TestV900ReleaseMetadataAligned -count=1`

## Carry-Forwards

- Frontend `v9.0.0` metadata and QA release must land before the stack-level
  global-kb release handoff is final.
- Cloud-native deployment docs and manifests remain reference-only; they do not
  block `v9.0.0` while the self-hosted release path is being hardened.
- Live marketplace, payment, carrier, social, and remote vision execution
  remain operator-gated until external credentials, sandbox approvals, and
  remote-resource routing are available.
- Deeper runtime hardening, observability expansion, and post-release defect
  harvest work roll forward into the `v10.0.0` program after the mirrored
  `v9.0.0` gate closes.
