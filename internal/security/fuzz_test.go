// Fuzz harnesses for the JWT access-token verifier. The contract per
// go-security-review is "must NEVER panic on garbage input; must return
// an error not crash". The corpus seed is a mix of structurally valid
// HS256 tokens, missing dots, oversized base64, control bytes, and
// tampered claims so the fuzzer probes both the segment splitter and
// the JSON unmarshallers downstream of base64.

package security

import (
	"strings"
	"testing"
	"time"
)

func fuzzTokenManager() *TokenManager {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	manager, err := NewTokenManager(TokenConfig{
		Secret:    "fuzz-secret-at-least-32-bytes-long-yes",
		Issuer:    "agentic-ecommerce",
		Audience:  "mc-api",
		AccessTTL: 15 * time.Minute,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		panic(err)
	}
	return manager
}

func FuzzVerifyAccessToken(f *testing.F) {
	manager := fuzzTokenManager()

	good, err := manager.MintAccessToken(Principal{Subject: "admin@example.com", Role: RoleAdmin})
	if err != nil {
		f.Fatalf("MintAccessToken: %v", err)
	}
	f.Add(good)
	f.Add("")
	f.Add(".")
	f.Add("..")
	f.Add("a.b.c")
	f.Add("not-a-jwt")
	f.Add(strings.Repeat("A", 4096))
	f.Add(good + "tamper")
	f.Add("eyJhbGciOiJub25lIn0..")
	f.Add("\x00\x01\x02.\x03\x04\x05.\x06\x07\x08")

	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = manager.VerifyAccessToken(raw)
	})
}
