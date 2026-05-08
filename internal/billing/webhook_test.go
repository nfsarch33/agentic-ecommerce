package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"
)

// testSecret values are deterministic test fixtures. Avoid the
// hex-only pattern that gitleaks flags as generic-api-key.
const (
	testSecretShort = "shortsecret"
	testSecret      = "test-stripe-webhook-secret-32by!" // 32 bytes; gitleaks:allow
)

func TestNewWebhookVerifierRejectsShortSecret(t *testing.T) {
	t.Parallel()
	_, err := NewWebhookVerifier(WebhookConfig{Secret: []byte(testSecretShort)})
	if !errors.Is(err, ErrSecretTooShort) {
		t.Fatalf("expected ErrSecretTooShort, got %v", err)
	}
}

func TestNewWebhookVerifierAcceptsExactly32(t *testing.T) {
	t.Parallel()
	v, err := NewWebhookVerifier(WebhookConfig{Secret: []byte(testSecret)})
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if v == nil {
		t.Fatalf("verifier nil")
	}
}

func TestVerifyMissingHeader(t *testing.T) {
	t.Parallel()
	v := mustVerifier(t)
	if err := v.Verify("", []byte("payload")); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("expected ErrMissingSignature, got %v", err)
	}
	if err := v.Verify("   ", []byte("payload")); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("expected ErrMissingSignature for blank, got %v", err)
	}
}

func TestVerifyMalformedHeader(t *testing.T) {
	t.Parallel()
	v := mustVerifier(t)
	cases := []string{
		"not-a-header",
		"t=,v1=abc",
		"v1=onlyv1",
		"t=notanumber,v1=abc",
		"t=1700000000",
	}
	for _, header := range cases {
		t.Run(header, func(t *testing.T) {
			t.Parallel()
			err := v.Verify(header, []byte("payload"))
			if !errors.Is(err, ErrSignatureMalformed) && !errors.Is(err, ErrMissingSignature) {
				t.Fatalf("expected ErrSignatureMalformed for %q, got %v", header, err)
			}
		})
	}
}

func TestVerifyMismatch(t *testing.T) {
	t.Parallel()
	v := mustVerifier(t)
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	v.now = func() time.Time { return now }
	timestamp := now.Add(-time.Minute).Unix()
	header := "t=" + strconv.FormatInt(timestamp, 10) + ",v1=deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	err := v.Verify(header, []byte("payload"))
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch, got %v", err)
	}
}

func TestVerifyReplayProtection(t *testing.T) {
	t.Parallel()
	v := mustVerifier(t)
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	v.now = func() time.Time { return now }
	old := now.Add(-DefaultWebhookTolerance - time.Minute).Unix()
	header := buildSignedHeader(t, []byte(testSecret), old, []byte("payload"))
	err := v.Verify(header, []byte("payload"))
	if !errors.Is(err, ErrEventTooOld) {
		t.Fatalf("expected ErrEventTooOld, got %v", err)
	}
}

func TestVerifyValidSignature(t *testing.T) {
	t.Parallel()
	v := mustVerifier(t)
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	v.now = func() time.Time { return now }
	timestamp := now.Add(-time.Minute).Unix()
	header := buildSignedHeader(t, []byte(testSecret), timestamp, []byte("hello world"))
	if err := v.Verify(header, []byte("hello world")); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestVerifyMultipleV1Entries(t *testing.T) {
	t.Parallel()
	v := mustVerifier(t)
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	v.now = func() time.Time { return now }
	timestamp := now.Add(-time.Minute).Unix()
	good := computeStripeSignature([]byte(testSecret), timestamp, []byte("hello"))
	header := "t=" + strconv.FormatInt(timestamp, 10) + ",v1=garbage,v1=" + good
	if err := v.Verify(header, []byte("hello")); err != nil {
		t.Fatalf("expected ok with rolled secret, got %v", err)
	}
}

// Helpers.

func mustVerifier(t *testing.T) *WebhookVerifier {
	t.Helper()
	v, err := NewWebhookVerifier(WebhookConfig{Secret: []byte(testSecret)})
	if err != nil {
		t.Fatalf("NewWebhookVerifier: %v", err)
	}
	return v
}

func buildSignedHeader(t *testing.T, secret []byte, timestamp int64, payload []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "t=" + strconv.FormatInt(timestamp, 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
