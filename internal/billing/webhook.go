package billing

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

// MinWebhookSecretBytes is the minimum length we accept for the
// Stripe webhook signing secret. The same lower bound is enforced by
// internal/adapter/signedurl/issuer.go and the v2.3.0 license-key
// generator so the whole codebase has one number.
const MinWebhookSecretBytes = 32

// DefaultWebhookTolerance is the maximum age (now - timestamp) we
// accept for a Stripe webhook signature. Five minutes matches the
// Stripe SDK default and limits the replay window.
const DefaultWebhookTolerance = 5 * time.Minute

// ErrSecretTooShort is returned when NewWebhookVerifier is constructed
// with a secret shorter than MinWebhookSecretBytes.
var ErrSecretTooShort = errors.New("stripe webhook secret too short")

// ErrMissingSignature is returned when the Stripe-Signature header is
// missing from the request.
var ErrMissingSignature = errors.New("stripe webhook signature missing")

// ErrSignatureMalformed is returned when the Stripe-Signature header is
// present but cannot be parsed (missing t=, missing v1=, etc.).
var ErrSignatureMalformed = errors.New("stripe webhook signature malformed")

// ErrSignatureMismatch is returned when the computed HMAC does not
// match any of the v1 signatures supplied. We always use
// crypto/subtle.ConstantTimeCompare to avoid timing oracles; never
// bytes.Equal.
var ErrSignatureMismatch = errors.New("stripe webhook signature mismatch")

// ErrEventTooOld is returned when the timestamp from the
// Stripe-Signature header is more than tolerance seconds in the past.
var ErrEventTooOld = errors.New("stripe webhook event too old")

// WebhookVerifier verifies Stripe webhook signatures per
// https://stripe.com/docs/webhooks#verify-manually:
//   - parse "t=..., v1=..., v1=..." (multiple v1 entries possible)
//   - compute HMAC-SHA256(secret, t + "." + payload), hex
//   - constant-time compare against any v1 signature
//   - reject when timestamp is older than tolerance
type WebhookVerifier struct {
	secret    []byte
	tolerance time.Duration
	now       func() time.Time
}

// WebhookConfig configures a WebhookVerifier.
type WebhookConfig struct {
	Secret    []byte
	Tolerance time.Duration
	Now       func() time.Time
}

// NewWebhookVerifier validates the secret length (>= 32 bytes) and
// returns a verifier. Reject early at process boot so a misconfigured
// secret never silently lets a forged event through.
func NewWebhookVerifier(cfg WebhookConfig) (*WebhookVerifier, error) {
	if len(cfg.Secret) < MinWebhookSecretBytes {
		return nil, fmt.Errorf("%w: got %d bytes, need >= %d", ErrSecretTooShort, len(cfg.Secret), MinWebhookSecretBytes)
	}
	tolerance := cfg.Tolerance
	if tolerance <= 0 {
		tolerance = DefaultWebhookTolerance
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &WebhookVerifier{
		secret:    append([]byte(nil), cfg.Secret...),
		tolerance: tolerance,
		now:       now,
	}, nil
}

// Verify validates that signatureHeader is a valid Stripe v1 signature
// over payload. Returns nil only when the signature matches and the
// timestamp is within the tolerance window. The verify-then-parse
// order is enforced by callers — they must pass the *raw* request
// body.
func (v *WebhookVerifier) Verify(signatureHeader string, payload []byte) error {
	header := strings.TrimSpace(signatureHeader)
	if header == "" {
		return ErrMissingSignature
	}
	timestamp, signatures, err := parseStripeSignatureHeader(header)
	if err != nil {
		return err
	}
	if v.now().Sub(time.Unix(timestamp, 0)) > v.tolerance {
		return fmt.Errorf("%w: timestamp=%d", ErrEventTooOld, timestamp)
	}
	want := computeStripeSignature(v.secret, timestamp, payload)
	for _, sig := range signatures {
		if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) == 1 {
			return nil
		}
	}
	return ErrSignatureMismatch
}

// parseStripeSignatureHeader extracts t=<unix>, v1=<hex>... from the
// canonical Stripe-Signature header value.
func parseStripeSignatureHeader(header string) (int64, []string, error) {
	parts := strings.Split(header, ",")
	if len(parts) < 2 {
		return 0, nil, fmt.Errorf("%w: header=%q", ErrSignatureMalformed, header)
	}
	var timestamp int64 = -1
	var signatures []string
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			return 0, nil, fmt.Errorf("%w: malformed segment %q", ErrSignatureMalformed, part)
		}
		switch kv[0] {
		case "t":
			ts, err := strconv.ParseInt(kv[1], 10, 64)
			if err != nil {
				return 0, nil, fmt.Errorf("%w: bad timestamp %q", ErrSignatureMalformed, kv[1])
			}
			timestamp = ts
		case "v1":
			signatures = append(signatures, kv[1])
		}
	}
	if timestamp < 0 {
		return 0, nil, fmt.Errorf("%w: missing t=", ErrSignatureMalformed)
	}
	if len(signatures) == 0 {
		return 0, nil, fmt.Errorf("%w: missing v1=", ErrSignatureMalformed)
	}
	return timestamp, signatures, nil
}

// computeStripeSignature returns the canonical hex HMAC-SHA256 over
// "<timestamp>.<payload>" using the webhook secret.
func computeStripeSignature(secret []byte, timestamp int64, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
