// Benchmarks for the access-token verifier hot path. These are gated
// out of the default `go test` run by `-bench=.` and exist primarily to
// detect regressions on the JWT signing/verification critical section.
// Hot-path because every authenticated mc-api request goes through
// VerifyAccessToken.

package security

import (
	"testing"
	"time"
)

func benchManagerAndToken(b *testing.B) (*TokenManager, string) {
	b.Helper()
	manager, err := NewTokenManager(TokenConfig{
		Secret:    "bench-secret-at-least-32-bytes-long-yes",
		Issuer:    "agentic-ecommerce",
		Audience:  "mc-api",
		AccessTTL: 15 * time.Minute,
		Now:       func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		b.Fatalf("NewTokenManager: %v", err)
	}
	token, err := manager.MintAccessToken(Principal{Subject: "admin@example.com", Role: RoleAdmin})
	if err != nil {
		b.Fatalf("MintAccessToken: %v", err)
	}
	return manager, token
}

func BenchmarkVerifyAccessToken(b *testing.B) {
	manager, token := benchManagerAndToken(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.VerifyAccessToken(token)
	}
}

func BenchmarkMintAccessToken(b *testing.B) {
	manager, _ := benchManagerAndToken(b)
	principal := Principal{Subject: "admin@example.com", Role: RoleAdmin}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.MintAccessToken(principal)
	}
}
