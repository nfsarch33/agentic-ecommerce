# v5.8.0 Security Audit

**Date:** 2026-05-11  
**Scope:** HMAC/crypto, JWT, webhook surfaces, input validation, SQL injection

---

## HMAC/Crypto Implementations

### Constant-Time Comparison (12 call sites)

All signature verification uses `crypto/subtle.ConstantTimeCompare` or `hmac.Equal`:

| File | Function | Method |
|------|----------|--------|
| `internal/security/token.go` | `verifyTokenSignature` | `subtle.ConstantTimeCompare` |
| `internal/billing/webhook.go` | `Verify` | `subtle.ConstantTimeCompare` |
| `internal/adapter/social/tiktok_shop_signing.go` | `VerifyTikTokSignature` | `subtle.ConstantTimeCompare` |
| `internal/adapter/social/tiktok_shop_signing.go` | `VerifyWebhook` | `subtle.ConstantTimeCompare` |
| `internal/adapter/social/facebook_shop_signing.go` | `VerifyFacebookWebhook` | `subtle.ConstantTimeCompare` |
| `internal/adapter/social/pinterest_shop.go` | `VerifyWebhook` | `subtle.ConstantTimeCompare` |
| `internal/registration/registration.go` | `Verify` | `subtle.ConstantTimeCompare` |
| `internal/domain/digital/license_key.go` | `Validate` | `subtle.ConstantTimeCompare` |
| `internal/adapter/signedurl/issuer.go` | `Verify` | `subtle.ConstantTimeCompare` |
| `internal/uiauto/ratelimit/limiter.go` | `verifyNonce` | `subtle.ConstantTimeCompare` |
| `cmd/mc-api/security.go` | token/hash compare | `subtle.ConstantTimeCompare` |
| `internal/webhook/verifier/verifier.go` | `Verify` (3 strategies) | `hmac.Equal` |
| `internal/adapter/carrier/auspost_client.go` | `VerifyAusPostHMAC` | `hmac.Equal` |
| `internal/adapter/woocommerce/webhook/handler.go` | `verifySignature` | `hmac.Equal` |

**Result:** PASS — no `bytes.Equal` or `==` used for signature comparison.

### Secret Source

All production secrets loaded from:
- Config structs populated from environment variables
- `os.Getenv("EC_*")` / `os.Getenv("STRIPE_*")` fallbacks
- No hardcoded secrets in non-test code

**Result:** PASS

---

## JWT Token Security

Implementation: `internal/security/token.go` (custom HS256-only, no third-party JWT library)

| Check | Line | Result |
|-------|------|--------|
| Algorithm pinning (HS256 only) | 162 | PASS |
| Rejects `alg: none` | 162 | PASS (any alg != HS256 rejected) |
| Expiry validation | 184 | PASS |
| Issuer validation | 173 | PASS |
| Audience validation | 173 | PASS |
| NotBefore validation | 181 | PASS |
| Minimum secret length (32 bytes) | 74 | PASS |
| Constant-time signature verify | 155 | PASS |
| Random JTI (unique token ID) | 115 | PASS (16-byte crypto/rand) |

**Result:** PASS

**Note:** Single-key architecture. Multi-key rotation (for zero-downtime key rotation) is a v6.x enhancement candidate.

---

## Webhook Verify-Then-Parse

All webhook handlers follow the pattern: read raw body → verify signature → parse/process.

| Handler | Verify Call | Parse After |
|---------|------------|-------------|
| `PaymentNormaliser.verifyAndEmit` | `adapter.VerifyWebhook(ctx, headers, body)` | Event processing only after nil error |
| `billing.WebhookVerifier.Verify` | Timestamp tolerance + HMAC check | Caller processes payload only on success |
| TikTok webhook verifier | `VerifyWebhook` signature check | Payload unmarshalled after verification |
| Facebook/Pinterest/WooCommerce | `VerifyWebhook` per adapter | Same pattern |

**Result:** PASS

---

## Input Validation

### Request Body Size Limits

All HTTP response body reads use `io.LimitReader`:
- Webhook bodies: `maxWebhookBodyBytes` (configurable, typically 1MB)
- API responses: 512B–8KB depending on endpoint
- Facebook batch: 4MB limit

**Result:** PASS

### SQL Injection Prevention

- **Query method:** All database queries use `pgxpool.Query/Exec/QueryRow` with `$N` placeholders
- **String interpolation in SQL:** 1 instance found — test-only (`rls_backfill_test.go` line 91) using table name (not user input)
- **Production code:** Zero string concatenation in SQL

**Result:** PASS

---

## Overall Verdict

| Category | Verdict |
|----------|---------|
| HMAC/Crypto | PASS |
| JWT Security | PASS |
| Webhook Verification | PASS |
| Input Validation | PASS |
| SQL Injection | PASS |

**Carry-forwards:**
1. JWT multi-key rotation support (v6.x)
