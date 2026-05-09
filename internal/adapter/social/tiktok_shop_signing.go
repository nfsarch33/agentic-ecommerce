// File scope: v3.3.0 EC-3-1 TikTok Shop request signing + webhook
// verification.
//
// TikTok Shop Open API requires every request to be signed with
// HMAC-SHA256 over a canonical form so the signature is independent
// of header ordering and proxy injections. The canonical form per
// the official spec is:
//
//	"<timestamp>\n<path>\n<sha256-hex(body)>"
//
// On the inbound (webhook) side TikTok ships a header of the form:
//
//	"t=<unix-seconds>,s=<hex-sha256-hmac>"
//
// where the HMAC body is computed over "<timestamp>.<raw-payload>".
// The format mirrors Stripe deliberately so we can reuse the
// internal/billing.WebhookVerifier mental model and ship one
// constant-time-compare path. NEVER bytes.Equal -- always
// crypto/subtle.ConstantTimeCompare.
//
// Decomposition discipline: this file owns the *pure* signing
// primitives. The HTTP client (tiktok_shop_client.go) calls these
// helpers, the OAuth flow (tiktok_shop_oauth.go) signs token
// exchanges, and the webhook handler (internal/webhook/tiktok_order.go)
// reuses VerifyTikTokWebhook so we never re-implement HMAC. This
// keeps cyclomatic complexity per-function under the v3.1.0 sentrux
// ceiling of 4 complex_fn.
package social

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TikTokSignRequest packages the inputs to ComputeTikTokSignature so
// callers don't pass four args in a row. Every field is required.
type TikTokSignRequest struct {
	Secret    []byte
	Timestamp int64
	Path      string
	Body      []byte
}

// ComputeTikTokSignature returns the canonical hex HMAC-SHA256 the
// official TikTok Shop spec defines for outbound requests. The body
// hash is included in the canonical form (NOT the raw body) so the
// signature is stable across transports that may rewrite the body
// stream.
//
// Returns ErrTikTokSecretTooShort when the secret is below
// MinTikTokSecretBytes.
func ComputeTikTokSignature(req TikTokSignRequest) (string, error) {
	if len(req.Secret) < MinTikTokSecretBytes {
		return "", fmt.Errorf("%w: got %d bytes, need >= %d", ErrTikTokSecretTooShort, len(req.Secret), MinTikTokSecretBytes)
	}
	if req.Timestamp <= 0 {
		return "", fmt.Errorf("%w: timestamp must be positive", ErrTikTokSignatureMalformed)
	}
	bodyHash := sha256.Sum256(req.Body)
	canonical := strings.Join([]string{
		strconv.FormatInt(req.Timestamp, 10),
		req.Path,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")
	mac := hmac.New(sha256.New, req.Secret)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyTikTokSignature constant-time compares an expected signature
// against ComputeTikTokSignature(req). Returns nil only when the
// signatures match. Wrapped via errors.Is(err, ErrTikTokSignatureMismatch)
// so callers branch on category, not message.
func VerifyTikTokSignature(req TikTokSignRequest, supplied string) error {
	want, err := ComputeTikTokSignature(req)
	if err != nil {
		return err
	}
	supplied = strings.TrimSpace(supplied)
	if supplied == "" {
		return ErrTikTokMissingSignature
	}
	if subtle.ConstantTimeCompare([]byte(want), []byte(supplied)) == 1 {
		return nil
	}
	return ErrTikTokSignatureMismatch
}

// TikTokWebhookVerifier verifies inbound webhook signatures. Mirrors
// internal/billing.WebhookVerifier (verify-then-parse, replay
// tolerance, constant-time compare) so the security review pattern
// stays uniform across bounded contexts.
type TikTokWebhookVerifier struct {
	secret    []byte
	tolerance time.Duration
	now       func() time.Time
}

// TikTokWebhookConfig configures a TikTokWebhookVerifier.
type TikTokWebhookConfig struct {
	Secret    []byte
	Tolerance time.Duration
	Now       func() time.Time
}

// NewTikTokWebhookVerifier validates the secret length and returns a
// verifier. Reject early at boot so a misconfigured secret never
// silently lets a forged event through.
func NewTikTokWebhookVerifier(cfg TikTokWebhookConfig) (*TikTokWebhookVerifier, error) {
	if len(cfg.Secret) < MinTikTokSecretBytes {
		return nil, fmt.Errorf("%w: got %d bytes, need >= %d", ErrTikTokSecretTooShort, len(cfg.Secret), MinTikTokSecretBytes)
	}
	tolerance := cfg.Tolerance
	if tolerance <= 0 {
		tolerance = DefaultTikTokWebhookTolerance
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &TikTokWebhookVerifier{
		secret:    append([]byte(nil), cfg.Secret...),
		tolerance: tolerance,
		now:       now,
	}, nil
}

// Verify validates that signatureHeader is a valid TikTok Shop
// webhook signature over payload. Returns nil only when the
// signature matches and the timestamp is within the tolerance
// window. Verify-then-parse: callers MUST pass the *raw* request
// body before json.Decode.
func (v *TikTokWebhookVerifier) Verify(signatureHeader string, payload []byte) error {
	header := strings.TrimSpace(signatureHeader)
	if header == "" {
		return ErrTikTokMissingSignature
	}
	timestamp, signature, err := parseTikTokWebhookHeader(header)
	if err != nil {
		return err
	}
	if v.now().Sub(time.Unix(timestamp, 0)) > v.tolerance {
		return fmt.Errorf("%w: timestamp=%d", ErrTikTokEventTooOld, timestamp)
	}
	want := computeTikTokWebhookSignature(v.secret, timestamp, payload)
	if subtle.ConstantTimeCompare([]byte(signature), []byte(want)) == 1 {
		return nil
	}
	return ErrTikTokSignatureMismatch
}

// SignWebhook is the inverse of Verify; tests + integration
// fixtures use it to forge headers without re-implementing the
// canonical form. Returns the canonical "t=<ts>,s=<hex>" header.
func (v *TikTokWebhookVerifier) SignWebhook(timestamp int64, payload []byte) string {
	signature := computeTikTokWebhookSignature(v.secret, timestamp, payload)
	return fmt.Sprintf("t=%d,s=%s", timestamp, signature)
}

// parseTikTokWebhookHeader extracts t=<unix>,s=<hex> from the
// canonical header value. Returns ErrTikTokSignatureMalformed on
// any deviation.
func parseTikTokWebhookHeader(header string) (int64, string, error) {
	parts := strings.Split(header, ",")
	if len(parts) < 2 {
		return 0, "", fmt.Errorf("%w: header=%q", ErrTikTokSignatureMalformed, header)
	}
	var (
		timestamp = int64(-1)
		signature string
	)
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			return 0, "", fmt.Errorf("%w: malformed segment %q", ErrTikTokSignatureMalformed, part)
		}
		switch kv[0] {
		case "t":
			ts, err := strconv.ParseInt(kv[1], 10, 64)
			if err != nil {
				return 0, "", fmt.Errorf("%w: bad timestamp %q", ErrTikTokSignatureMalformed, kv[1])
			}
			timestamp = ts
		case "s":
			signature = kv[1]
		}
	}
	if timestamp < 0 {
		return 0, "", fmt.Errorf("%w: missing t=", ErrTikTokSignatureMalformed)
	}
	if signature == "" {
		return 0, "", fmt.Errorf("%w: missing s=", ErrTikTokSignatureMalformed)
	}
	return timestamp, signature, nil
}

// computeTikTokWebhookSignature returns the hex HMAC-SHA256 over
// "<timestamp>.<payload>" using the shop webhook secret. The
// timestamp.payload form mirrors Stripe so reviewers comparing the
// two webhook surfaces see the same shape.
func computeTikTokWebhookSignature(secret []byte, timestamp int64, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// canonicalRequestPath strips the query string from a path so a
// proxy that re-orders ?a=1&b=2 to ?b=2&a=1 does not break the
// signature. TikTok Shop signs path-only; query parameters are
// canonicalised separately by the caller per spec when needed.
func canonicalRequestPath(rawPath string) string {
	if i := strings.IndexByte(rawPath, '?'); i >= 0 {
		return rawPath[:i]
	}
	return rawPath
}

// ensureSecret is the small guard helper consumed by the OAuth and
// signing paths. Returns the typed sentinel when len(secret) < min.
func ensureSecret(secret []byte) error {
	if len(secret) < MinTikTokSecretBytes {
		return fmt.Errorf("%w: got %d bytes, need >= %d", ErrTikTokSecretTooShort, len(secret), MinTikTokSecretBytes)
	}
	return nil
}

// errSentinel allows tests to verify the wrapping chain without
// constructing the package-internal fmt.Errorf return values.
func errSentinel(err error) error { return errors.Unwrap(err) }
