// Benchmarks for the Stripe webhook signature verifier hot path.
// Stripe events are the primary public-facing untrusted input vector
// for the billing service so signature verification must stay fast and
// allocation-free. b.ReportAllocs() lets reviewers spot regressions.

package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

func benchVerifierAndPayload(b *testing.B) (*WebhookVerifier, string, []byte) {
	b.Helper()
	secret := []byte("bench-secret-at-least-32-bytes-long-yes")
	v, err := NewWebhookVerifier(WebhookConfig{
		Secret:    secret,
		Tolerance: 5 * time.Minute,
		Now:       func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		b.Fatalf("NewWebhookVerifier: %v", err)
	}
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC).Unix()
	payload := []byte(`{"id":"evt_1","type":"customer.subscription.created","data":{"object":{"id":"sub_1"}}}`)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(strconv.FormatInt(now, 10)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(payload)
	header := "t=" + strconv.FormatInt(now, 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	return v, header, payload
}

func BenchmarkWebhookVerify(b *testing.B) {
	v, header, payload := benchVerifierAndPayload(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Verify(header, payload)
	}
}
