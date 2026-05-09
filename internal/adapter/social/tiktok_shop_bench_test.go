package social

import (
	"strings"
	"testing"
	"time"
)

// BenchmarkComputeTikTokSignature measures the cost of the canonical
// HMAC-SHA256 over a representative payload + path. Used to track
// regressions in the v3.3.0 EC-3-1 sign hot path.
func BenchmarkComputeTikTokSignature(b *testing.B) {
	body := []byte(`{"sku":"benchmark","quantity":1,"unit_cents":4999,"currency":"AUD","tenant_id":"tenant-1"}`)
	req := TikTokSignRequest{
		Secret:    []byte(testTikTokSecret),
		Timestamp: time.Now().Unix(),
		Path:      "/api/products",
		Body:      body,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ComputeTikTokSignature(req); err != nil {
			b.Fatalf("ComputeTikTokSignature: %v", err)
		}
	}
}

// BenchmarkVerifyTikTokSignature measures the verify path cost.
func BenchmarkVerifyTikTokSignature(b *testing.B) {
	body := []byte(strings.Repeat("a", 1024))
	req := TikTokSignRequest{
		Secret:    []byte(testTikTokSecret),
		Timestamp: time.Now().Unix(),
		Path:      "/api/products/search",
		Body:      body,
	}
	want, err := ComputeTikTokSignature(req)
	if err != nil {
		b.Fatalf("ComputeTikTokSignature: %v", err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := VerifyTikTokSignature(req, want); err != nil {
			b.Fatalf("VerifyTikTokSignature: %v", err)
		}
	}
}

// BenchmarkTikTokWebhookVerifier_Verify measures the inbound webhook
// verify path under a realistic body size.
func BenchmarkTikTokWebhookVerifier_Verify(b *testing.B) {
	body := []byte(`{"order_id":"bench","items":[{"sku":"S","quantity":1,"unit_cents":4999}]}`)
	v, err := NewTikTokWebhookVerifier(TikTokWebhookConfig{
		Secret: []byte(testTikTokSecret),
		Now:    func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		b.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	header := v.SignWebhook(time.Date(2026, 5, 9, 11, 59, 30, 0, time.UTC).Unix(), body)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := v.Verify(header, body); err != nil {
			b.Fatalf("Verify: %v", err)
		}
	}
}
