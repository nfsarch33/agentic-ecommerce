# EC v9.0.0 Publication Path

**Sprint**: v5026 (backend publication path)
**Date**: 2026-05-19
**Head**: ef15859bf46d73029fac404b83b05c7c0cf7b9de
**Status**: gate-ready; semver tag blocked on primary-testing run

## Overview

This runbook covers the steps from the merged v5025 pre-release HEAD to a cut
`v9.0.0` semver tag. The primary-testing lane on `wsl1/win1` is the only
blocking gate. No rc tags are created; the tag is cut once all gates below pass.

## Prerequisites

- Backend HEAD is `ef15859bf46d73029fac404b83b05c7c0cf7b9de` (v5025 merged).
- Frontend HEAD is `0b2fad90071785fd3a27c244cf9bc2c6c7066b0a` (v5025 web merged).
- `VERSION` file reads `9.0.0`.
- `api/openapi.yaml` version reads `9.0.0`.

## Backend Release Gate Sequence

Run these in order on the MacBook controller (read-only canaries) and on
`primary-testing` (integration lanes):

```bash
# 1. Diff clean
runx git diff --repo ecommerce -- --check

# 2. Shell-leak scan
runx shell-leak-scan --repo ecommerce

# 3. Full test suite with race detector
runx go test --repo ecommerce -- -race -p 1 -count=1 ./...

# 4. Coverage gate
runx make --repo ecommerce -- coverage-check

# 5. Vulnerability scan
runx make --repo ecommerce -- govulncheck-scan

# 6. Build
runx make --repo ecommerce -- build

# 7. Contract test
runx make --repo ecommerce -- contract-test

# 8. Config validation
runx make --repo ecommerce -- compose-temporal-config
runx make --repo ecommerce -- n8n-workflows-validate
runx make --repo ecommerce -- monitoring-validate
runx make --repo ecommerce -- compose-config-prod

# 9. Sentrux gate
runx sentrux gate --repo ecommerce

# 10. Primary-pool canaries
runx ssh exec --target wsl1-travel --cmd ssh-canary-wsl
runx ssh exec --target win1-travel --cmd ssh-canary-win

# 11. Primary-lane integration gates
runx test-lane run --lane backend-integration --pool primary-testing
runx test-lane run --lane full-stack-e2e --pool primary-testing
runx test-lane run --lane cleanup-testing --pool primary-testing
```

## Frontend Release Gate Sequence

The frontend v5027 QA sprint captures this evidence. Required before stack tag:

```bash
# On primary-testing (wsl1/win1)
runx test-lane run --lane frontend-playwright-stable --pool primary-testing
runx test-lane run --lane frontend-uiauto-compare --pool primary-testing
```

## Tag Cut Procedure

Once all gates above are green:

```bash
# Operator action -- run from a shell with personal identity active
git -C ~/agentic-ecommerce tag -a v9.0.0 -m "v9.0.0: backend platform baseline"
# Push via runx env personal-shell
runx env personal-shell --exec "runx git push --repo ecommerce --tags"
```

## Rollback

If a post-tag defect is found, do not delete the tag. Open a `v5028` defect
harvest sprint. Tags are immutable evidence once published.

## Non-Blocking Carry-Forwards

- `wsl2`, `win2`, `win2-travel` timeout from the controller -- non-blocking.
- Cloud-native deployment (`deploy/terraform`, `deploy/helm`) -- reference only.
- Live marketplace, payment, and external API execution -- operator-gated.
- Post-v9 defect harvest and v10.0.0 programme -- separate sprint series.
