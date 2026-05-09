// File scope: v3.4.0 EC-4-2 Facebook Shop request signing + webhook
// verification.
//
// META Graph API uses two distinct HMAC mechanisms:
//
//  1. **Outbound `appsecret_proof`**: every Graph API call is
//     accompanied by an `appsecret_proof` query parameter computed
//     as `hex(HMAC-SHA256(access_token, app_secret))`. This pins
//     the call to the holder of the app secret so a stolen token
//     alone cannot be replayed against another app.
//
//  2. **Inbound webhook X-Hub-Signature-256**: every webhook
//     delivery includes `X-Hub-Signature-256: sha256=<hex>` where
//     hex is `HMAC-SHA256(raw-body, app_secret)`. The shape mirrors
//     GitHub's own webhook signature so reviewers comparing the
//     two surfaces see the same scheme.
//
// Decomposition discipline: this file owns the *pure* signing
// primitives. The HTTP client (facebook_shop_client.go) calls these
// helpers, the OAuth flow (facebook_shop_oauth.go) signs token
// debug calls, and the webhook handler reuses VerifyFacebookWebhook
// so we never re-implement HMAC. NEVER bytes.Equal -- always
// crypto/subtle.ConstantTimeCompare. This keeps cyclomatic
// complexity per-function under the v3.1.0 sentrux ceiling of 4
// complex_fn (HARD GATE).
package social

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// FacebookAppSecretProofPrefix is empty (META does not use a prefix);
// the constant exists for symmetry with the WebhookSignaturePrefix.
const FacebookAppSecretProofPrefix = ""

// FacebookWebhookSignaturePrefix is the literal prefix META prepends
// to the X-Hub-Signature-256 header value: "sha256=".
const FacebookWebhookSignaturePrefix = "sha256="

// ComputeFacebookAppSecretProof returns the canonical
// `appsecret_proof` for a given page/user access token. Per the META
// docs the proof is `hex(HMAC-SHA256(access_token, app_secret))`.
// Returns ErrFacebookSecretTooShort when the secret is below
// MinFacebookSecretBytes.
func ComputeFacebookAppSecretProof(appSecret []byte, accessToken string) (string, error) {
	if len(appSecret) < MinFacebookSecretBytes {
		return "", fmt.Errorf("%w: got %d bytes, need >= %d", ErrFacebookSecretTooShort, len(appSecret), MinFacebookSecretBytes)
	}
	if accessToken == "" {
		return "", fmt.Errorf("%w: access_token required", ErrFacebookUnconfigured)
	}
	mac := hmac.New(sha256.New, appSecret)
	_, _ = mac.Write([]byte(accessToken))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// ComputeFacebookWebhookSignature returns the canonical hex
// HMAC-SHA256 over `payload` using `appSecret`. Caller is
// responsible for prepending the "sha256=" prefix when building
// a header value (use SignFacebookWebhook for that).
func ComputeFacebookWebhookSignature(appSecret, payload []byte) (string, error) {
	if len(appSecret) < MinFacebookSecretBytes {
		return "", fmt.Errorf("%w: got %d bytes, need >= %d", ErrFacebookSecretTooShort, len(appSecret), MinFacebookSecretBytes)
	}
	mac := hmac.New(sha256.New, appSecret)
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// SignFacebookWebhook returns a complete header value
// `sha256=<hex>` ready to set on X-Hub-Signature-256. Useful in
// integration tests + replay fixtures that forge headers without
// re-implementing the canonical form.
func SignFacebookWebhook(appSecret, payload []byte) (string, error) {
	hexSig, err := ComputeFacebookWebhookSignature(appSecret, payload)
	if err != nil {
		return "", err
	}
	return FacebookWebhookSignaturePrefix + hexSig, nil
}

// VerifyFacebookWebhook constant-time compares the supplied
// `X-Hub-Signature-256` header against the recomputed HMAC over
// payload. Returns nil only when the signatures match. Wrapped via
// errors.Is(err, ErrFacebookSignatureMismatch) so callers branch on
// category, not message.
func VerifyFacebookWebhook(appSecret []byte, header string, payload []byte) error {
	header = strings.TrimSpace(header)
	if header == "" {
		return fmt.Errorf("%w: header empty", ErrFacebookSignatureMismatch)
	}
	if !strings.HasPrefix(header, FacebookWebhookSignaturePrefix) {
		return fmt.Errorf("%w: missing %q prefix", ErrFacebookSignatureMismatch, FacebookWebhookSignaturePrefix)
	}
	suppliedHex := strings.TrimPrefix(header, FacebookWebhookSignaturePrefix)
	wantHex, err := ComputeFacebookWebhookSignature(appSecret, payload)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(wantHex), []byte(suppliedHex)) == 1 {
		return nil
	}
	return ErrFacebookSignatureMismatch
}

// ensureFacebookSecret is the small guard helper consumed by the
// OAuth and signing paths. Returns the typed sentinel when
// len(secret) < MinFacebookSecretBytes.
func ensureFacebookSecret(secret []byte) error {
	if len(secret) < MinFacebookSecretBytes {
		return fmt.Errorf("%w: got %d bytes, need >= %d", ErrFacebookSecretTooShort, len(secret), MinFacebookSecretBytes)
	}
	return nil
}

// errFacebookUnwrap allows tests to verify the wrapping chain
// without constructing the package-internal fmt.Errorf return
// values.
func errFacebookUnwrap(err error) error { return errors.Unwrap(err) }
