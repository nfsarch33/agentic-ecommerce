// Fuzz harness for the signed-URL verifier. The contract is "must
// NEVER panic on attacker-controlled URL bytes; must always return an
// error rather than crash". Seed corpus includes valid URLs, malformed
// query strings, empty fields, percent-encoded garbage, and oversized
// payloads.

package signedurl

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

const fuzzIssuerSecret = "fuzz-secret-at-least-32-bytes-long-yes"

func newFuzzIssuer() *HMACIssuer {
	issuer, err := New(Config{
		BaseURL: "https://cdn.example.com/api/v1/digital-downloads",
		Secret:  []byte(fuzzIssuerSecret),
	})
	if err != nil {
		panic(err)
	}
	return issuer
}

func FuzzVerifySignedURL(f *testing.F) {
	issuer := newFuzzIssuer()
	issued, err := issuer.Issue(port.IssueDownloadRequest{
		TenantID:    "tenant-default",
		LicenseID:   uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		ProductID:   uuid.MustParse("66666666-7777-8888-9999-aaaaaaaaaaaa"),
		IssuedAt:    time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		TTL:         5 * time.Minute,
		UsesAllowed: 3,
	})
	if err != nil {
		f.Fatalf("Issue: %v", err)
	}

	f.Add(issued.URL)
	f.Add("")
	f.Add("not-a-url")
	f.Add("https://cdn.example.com/")
	f.Add("https://cdn.example.com/?sig=abc")
	f.Add("https://cdn.example.com/?tid=&lid=&pid=&exp=&uses=&sig=")
	f.Add("https://cdn.example.com/?tid=t&lid=not-a-uuid&pid=not-a-uuid&exp=abc&uses=-1&sig=zz")
	f.Add(strings.Repeat("https://cdn.example.com/?", 50))
	f.Add("https://cdn.example.com/?tid=%00%01&lid=%FF&sig=%FF")
	f.Add(issued.URL + "&extra=garbage")

	now := time.Date(2026, 5, 7, 12, 1, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = issuer.Verify(raw, now)
	})
}
