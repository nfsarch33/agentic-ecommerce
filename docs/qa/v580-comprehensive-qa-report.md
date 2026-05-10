# v5.8.0 Comprehensive QA Report

**Date:** 2026-05-11  
**Go version:** 1.26.3 darwin/arm64  
**Backend HEAD:** `329505d` (main)  
**Frontend HEAD:** `1e11010` (main)  
**Pair:** 8 of 10 (v5.1.x → v6.0.0 cycle)

---

## 1. govulncheck + Dependency Audit

### govulncheck Results

```
$ GOTOOLCHAIN=auto govulncheck ./...
No vulnerabilities found.
```

**Exit code:** 0  
**Findings:** ZERO vulnerabilities (critical: 0, high: 0, medium: 0, low: 0)  
**Status:** PASS — no fixes required.

### Dependency Inventory

- **Total modules:** 215
- **Key dependencies:** pgx, temporal-sdk, prometheus, protobuf, testcontainers, otel
- **No deprecated or unmaintained modules detected**

---

## 2. Triple-Run Flake Detection

### Results

| Run | Packages | Passed | Failed | Duration |
|-----|----------|--------|--------|----------|
| 1   | 100      | 100    | 0      | ~58s     |
| 2   | 100      | 100    | 0      | ~46s     |
| 3   | 100      | 100    | 0      | ~46s     |

**Flaky tests detected:** 0  
**Race conditions detected:** 0 (all runs used `-race`)  
**Status:** PASS — all 3 runs produced identical results.

---

## 3. Security Audit

### 3.1 HMAC/Crypto Implementations

| Check | Status | Details |
|-------|--------|---------|
| `subtle.ConstantTimeCompare` or `hmac.Equal` used | PASS | 12 call sites verified; zero `bytes.Equal` for signatures |
| No hardcoded secrets in production code | PASS | All secrets from config/env vars; test files use test-only literals |
| Minimum secret length enforced | PASS | 32-byte minimum on JWT secret, webhook secrets |
| Verify-then-parse pattern | PASS | `PaymentNormaliser.verifyAndEmit` calls `adapter.VerifyWebhook` before processing |

### 3.2 JWT Middleware

| Check | Status | Details |
|-------|--------|---------|
| Algorithm pinning (no `alg: none`) | PASS | `header.Algorithm != "HS256"` rejection at line 162 |
| Expiry check | PASS | `raw.ExpiresAt <= now` at line 184 |
| Issuer check | PASS | `raw.Issuer != m.issuer` at line 173 |
| Audience check | PASS | `raw.Audience != m.audience` at line 173 |
| NotBefore check | PASS | `raw.NotBefore > now` at line 181 |
| Constant-time signature verify | PASS | `subtle.ConstantTimeCompare` at line 155 |
| Secret key rotation support | NOTE | Single-key only; multi-key rotation is a v6.x carry-forward |

### 3.3 Input Validation

| Check | Status | Details |
|-------|--------|---------|
| Request body size limits | PASS | All body reads use `io.LimitReader` (4KB–8MB depending on endpoint) |
| SQL injection prevention | PASS | All queries use pgx parameterised queries ($1, $2...); zero string interpolation in SQL |
| Webhook body max size | PASS | `maxWebhookBodyBytes` constant enforced |

### 3.4 Audit Summary

**Overall:** PASS (all checks green)  
**Carry-forwards:** JWT secret key rotation support (v6.x)

---

## 4. Frontend E2E (Playwright)

### Pre-fix Results

- **Total specs:** 58
- **Passed:** 54
- **Failed:** 2 (`payments.spec.ts` — locator ambiguity)
- **Skipped:** 2

### Root Cause

The filter dropdowns (added in v4.x) introduced `<option>` elements with text matching table cell content. `getByText('stripe')` and `locator('text=succeeded')` resolved to hidden `<option>` elements instead of visible table cells.

### Fix Applied

Scoped locators to `page.locator("table")` to disambiguate table content from dropdown options.

### Post-fix Results

- **Total specs:** 58
- **Passed:** 56
- **Failed:** 0
- **Skipped:** 2
- **Duration:** 1.6 min (chromium, single worker, 2 retries)
- **Status:** PASS

### Frontend Unit Tests

- **Test files:** 223
- **Tests:** 1077 passed
- **Duration:** 25.6s
- **Status:** PASS

---

## 5. uiauto Comparison Harness

### Unit Tests (Mock Data)

All `internal/uiauto/compare/` package tests pass:
- `TestComparisonRunner_BothPass`
- `TestComparisonRunner_UIAutoFaster`
- `TestComparisonRunner_PlaywrightFaster`
- `TestComparisonRunner_OneFailsOtherPasses`
- `TestComparisonRunner_BothFail`
- `TestComparisonRunner_RunAll`
- `TestAggregate_*`, `TestDiff`, `TestSummarize`, etc.

### Live OmniParser Bridge

**Status:** DEFERRED  
**Reason:** OmniParser bridge runs on WSL GPU fleet; not available from MacBook overnight agent.  
**Carry-forward:** Run live comparison when WSL fleet is online.

---

## 6. k6 Load Test

### Script Validation

```
$ k6 inspect tests/load/k6/v490_comprehensive.js
✓ Script parsed successfully (7 scenarios, 2 min duration each)
```

**Scenarios:** payment_charge, webhook_normaliser, admin_mobile, coaching_tip, commission_report, tenant_dashboard, gmv_api

### Execution

**Status:** DEFERRED  
**Reason:** Requires running backend at localhost:8080. Backend not started for this QA run (testing only, not deployment).  
**Carry-forward:** Execute as part of pre-release v6.0.0 load validation with full stack running.

### Manual Execution Instructions

```bash
# Start backend
docker compose up -d
# Run k6
k6 run tests/load/k6/v490_comprehensive.js \
  --out json=tests/load/results/v490_$(date +%s).json \
  -e BASE_URL=http://localhost:8080 \
  --duration 30s --vus 10
```

---

## 7. Summary

| Area | Status | Notes |
|------|--------|-------|
| govulncheck | PASS | 0 vulnerabilities across 215 deps |
| Flake detection | PASS | 100 packages × 3 runs, 0 flaky |
| Security: HMAC | PASS | ConstantTimeCompare everywhere |
| Security: JWT | PASS | alg pinning + exp + iss + aud + nbf |
| Security: Input validation | PASS | LimitReader + parameterised SQL |
| Frontend unit tests | PASS | 1077/1077 |
| Frontend E2E | PASS | 56/56 (2 skipped, fix applied) |
| uiauto compare (mock) | PASS | All unit tests green |
| uiauto compare (live) | DEFERRED | OmniParser on WSL fleet |
| k6 load test | DEFERRED | No running backend |

### Carry-forwards for v6.x

1. JWT secret key rotation support (multi-key validation)
2. Live uiauto comparison via OmniParser bridge (WSL fleet)
3. k6 load test execution against full stack

### Code Changes in This Pair

- `e2e/payments.spec.ts` — fixed locator ambiguity (scoped to table)
