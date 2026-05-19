# EC v5028 -- v5027 Frontend Publication Path QA Evidence Lock

**Sprint**: v5028  
**Date**: 2026-05-19  
**Gate**: QA lock for v5027 frontend publication path deliverables  
**Status**: LOCKED -- all gates GREEN

## Scope

v5028 is the QA pair for v5027. It validates all frontend publication path
artefacts produced in v5027 and locks cross-stack evidence before the v9.0.0
semver tag is cut.

## Go Cross-Stack Tests (agentic-ecommerce/internal/qa)

Test file: `internal/qa/v5027_frontend_publication_path_test.go`

```
GOENV=off GOROOT= go -C /Users/jason.lian/agentic-ecommerce test -race ./internal/qa/... -run TestV5027
```

| Test | Result |
|------|--------|
| TestV5027FrontendVersionIs9 | PASS |
| TestV5027FrontendChangelogDocumentsV9 | PASS |
| TestV5027FrontendReleaseChecklistExists | PASS |
| TestV5027FrontendReleaseFinalDocumentsCrossStackEvidence | PASS |

Path fix applied: `webRepoPath()` helper resolves via `os.UserHomeDir()` +
`Code/personal/agentic-ecommerce-web/` instead of broken relative path.

## Full QA Package (all tests)

```
ok   github.com/nfsarch33/agentic-ecommerce/internal/qa  1.243s
```

All tests PASS, race detector clean, `go vet` clean.

## Frontend Artefact Verification

| Artefact | Path | Status |
|----------|------|--------|
| package.json version 9.0.0 | `Code/personal/agentic-ecommerce-web/package.json` | CONFIRMED |
| CHANGELOG [9.0.0] entry | `Code/personal/agentic-ecommerce-web/CHANGELOG.md` | CONFIRMED |
| README v9.0.0 reference | `Code/personal/agentic-ecommerce-web/README.md` | CONFIRMED |
| v9-frontend-release-checklist.md | `Code/personal/agentic-ecommerce-web/docs/` | CONFIRMED |
| v9-frontend-release-final.md | `Code/personal/agentic-ecommerce-web/docs/` | CONFIRMED |

## Backend Cross-Stack Evidence

- Backend v5026 gate PASSED: all backend publication path tests GREEN.
- Backend VERSION: `9.0.0` (commit `c334951`).
- Backend primary-testing: `wsl1-travel` PASS, `win1-travel` PASS (v5026 evidence).
- Backend QA package: all tests PASS with race detector.

## Carry-Forwards (deferred, not blocking)

- UIAuto E2E comparison: deferred to v5038/v5039.
- Playwright stable run: deferred until deployment environment available.
- Lighthouse performance audit: deferred to v5042/v5043.

## Semver Tag Readiness

Frontend v9.0.0 is GATE-READY. Operator steps before tagging:

1. Confirm no in-flight feature branches on `agentic-ecommerce-web`.
2. Push `agentic-ecommerce-web` main (v5027 commits).
3. Push `agentic-ecommerce` main (v5026 + v5027 QA commits, 2 ahead).
4. `git tag -a v9.0.0 -m "v9.0.0: EC v9 frontend publication"` on `agentic-ecommerce-web`.
5. `git push origin v9.0.0` on `agentic-ecommerce-web`.
