// Package jwt is the v6.2.0 JWT secret-key rotation seam (closes
// ADR-032 CF #6).
//
// The package owns one responsibility: hold a versioned set of HMAC
// signing keys (current + previous valid for a grace period) and
// expose Mint / Verify with the same shape as
// internal/security.TokenManager so the composition root can swap
// the singleton signer for the Rotator without touching call sites.
//
// Design notes:
//
//   - HS256 only. Asymmetric (RS256/ES256) key rotation is
//     out-of-scope for v6.2.0 (deferred to v7.x; tracked in ADR-032
//     review).
//   - Keys carry a Version (kid) and a NotAfter timestamp. The
//     `kid` is embedded in the JWT header so verifiers can resolve
//     the right key without trial-and-error HMAC.
//   - `Mint` always uses the configured ActiveVersion. `Verify`
//     resolves the version from the header, then enforces the
//     NotAfter grace window. Tokens issued by a key whose grace
//     window has expired are rejected with ErrExpiredKey.
//   - Migration 0038_jwt_key_versions.sql persists the key catalogue
//     so multiple replicas converge on the same active version.
//
// Decomposition discipline (HARD GATE: complex_fn must stay <= 5):
//
//   - Rotator.Mint                  (cyclomatic 4)
//   - Rotator.Verify                (cyclomatic 5)
//   - Rotator.SetActive             (cyclomatic 3)
//   - keyRing.add / lookup          (cyclomatic 3 each)
//
// Reuse evidence:
//   - JWT encode/decode helpers mirror internal/security/token.go.
//   - Versioned-key + grace-window pattern mirrors the v5.5.0
//     internal/billing webhook rotation note.
package jwt
