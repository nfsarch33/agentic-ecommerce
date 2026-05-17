# EC v9.0.0 Release Final Evidence

**Date**: 2026-05-16
**Release**: `agentic-ecommerce` v9.0.0
**Base**: v8.0.0 release tag
**Head**: 17eab44dfdfc807e34fbf4ee05b6bd9f2322e41c
Frontend SHA: `7682367be3b5af728649a9730a625aa54c46d87b`
OpenAPI contract path: `api/openapi.yaml`
**Status**: pre-release freeze captured; semver tag still blocked

## Scope

v9.0.0 establishes the backend platform baseline for the post-v8 program:

- Controller-accurate release metadata, version policy, checklist, and ADR
  coverage guarded by `TestV900ReleaseMetadataAligned`.
- `primary-testing` is the only release blocker for backend integration,
  full-stack E2E, cleanup, frontend stable Playwright, and UIAuto evidence.
- `secondary-testing` remains evidence and overflow capacity, but it is not the
  blocking release lane in the current EC programme.
- Current fleet truth is release-relevant: `wsl1-travel`, `win1-travel`, and
  `wsl2-travel` are green from the controller; `wsl2`, `win2`, and
  `win2-travel` still time out and therefore keep the semver tag uncut.
- Cloud-native deployment material under `deploy/terraform`, `deploy/helm`, and
  `deploy/otel` remains reference-only for `v9.0.0`; it is maintained but no
  longer blocks the semver tag.
- Pair 5 bootstrap is landed behind `EC_AGENT_RUNTIME_MODE`: backend now pins
  `github.com/cloudwego/eino v0.8.13`, exposes an internal EINO adapter/runtime
  seam, and keeps `legacy` as the default business path while `shadow` and
  `primary` remain explicit opt-ins.
- Release-facing API, Temporal, and webhook docs aligned with the v9 current
  release baseline instead of the legacy v2.0.0 release label.
- Support-tool provenance, primary-lane evidence, optional secondary-pool
  carry-forward notes, and cross-repo handoff docs captured before any
  semver-only `v9.0.0` tag is cut.

## Current Blocking Status

- Latest controller-side canaries: `wsl1-travel` PASS, `win1-travel` PASS,
  `wsl2-travel` PASS, `wsl2` FAIL (timeout), `win2` FAIL (timeout),
  `win2-travel` FAIL (timeout).
- The semver tag `v9.0.0` remains uncut until `primary-testing` closes its
  lane-level runtime gaps and the blocking release lanes pass on the primary
  pool.
- `secondary-testing` remains non-blocking carry-forward work until the EC
  programme explicitly re-promotes it.
- External-provider live execution remains operator-gated even after the
  self-hosted release gates are green.

Current blockers:

- `wsl2`, `win2`, and `win2-travel` still time out from the controller.
- Blocking `primary-testing` release lanes have not yet all been re-run on the
  frozen backend/frontend SHAs above.
- Stack-level publication remains blocked on the frontend stable Playwright and
  UIAuto evidence captured on `primary-testing`.

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
- `runx ssh exec --target wsl1-travel --cmd ssh-canary-wsl`
- `runx ssh exec --target win1-travel --cmd ssh-canary-win`
- `runx test-lane run --lane backend-integration --pool primary-testing`
- `runx test-lane run --lane full-stack-e2e --pool primary-testing`
- `runx test-lane run --lane cleanup-testing --pool primary-testing`

Cross-repo release dependency:

- Frontend `frontend-playwright-stable` and `frontend-uiauto-compare` must pass
  on `primary-testing` before the stack tag is cut. Secondary-pool UIAuto
  evidence is useful follow-up, but it is not the release blocker here.

Focused release metadata guard:

- `runx worktree run --repo ecommerce --branch release/v9-platform-baseline -- go test ./internal/qa -run TestV900ReleaseMetadataAligned -count=1`

## Carry-Forwards

- Frontend `v9.0.0` metadata and QA release must land before the stack-level
  global-kb release handoff is final.
- Cloud-native deployment docs and manifests remain reference-only; they do not
  block `v9.0.0` while the primary self-hosted release path is being hardened.
- Live marketplace, payment, carrier, social, and remote vision execution
  remain operator-gated until external credentials, sandbox approvals, and
  remote-resource routing are available.
- Deeper runtime hardening, observability expansion, and post-release defect
  harvest work roll forward into the `v10.0.0` program after the
  `primary-testing` `v9.0.0` gate closes.
