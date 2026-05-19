// Benchmarks for the signed-URL hot path. Issue + Verify run on every
// digital download attempt; their CPU and allocation profile is part of
// the user-visible SLO.

package signedurl

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

func benchIssuerFixture(b *testing.B) (*HMACIssuer, port.IssueDownloadRequest, string) {
	b.Helper()
	issuer, err := New(Config{
		BaseURL: "https://cdn.example.com/api/v1/digital-downloads",
		Secret:  []byte("bench-secret-at-least-32-bytes-long-yes"),
	})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	req := port.IssueDownloadRequest{
		TenantID:    "tenant-default",
		LicenseID:   uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		ProductID:   uuid.MustParse("66666666-7777-8888-9999-aaaaaaaaaaaa"),
		IssuedAt:    time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		TTL:         5 * time.Minute,
		UsesAllowed: 3,
	}
	resp, err := issuer.Issue(req)
	if err != nil {
		b.Fatalf("Issue: %v", err)
	}
	return issuer, req, resp.URL
}

func BenchmarkIssueSignedURL(b *testing.B) {
	issuer, req, _ := benchIssuerFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = issuer.Issue(req)
	}
}

func BenchmarkVerifySignedURL(b *testing.B) {
	issuer, _, url := benchIssuerFixture(b)
	now := time.Date(2026, 5, 7, 12, 1, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = issuer.Verify(url, now)
	}
}
