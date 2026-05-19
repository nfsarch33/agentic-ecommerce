package signedurl

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/domain/digital"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

func newTestIssuer(t *testing.T) *HMACIssuer {
	t.Helper()
	iss, err := New(Config{
		BaseURL: "https://cdn.example.com/api/v1/digital-downloads",
		Secret:  []byte("test-secret-32-bytes-of-signed-url-1!"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return iss
}

func validIssueRequest() port.IssueDownloadRequest {
	return port.IssueDownloadRequest{
		TenantID:    "tenant-a",
		LicenseID:   uuid.MustParse("88888888-8888-8888-8888-888888888888"),
		ProductID:   uuid.MustParse("99999999-9999-9999-9999-999999999999"),
		IssuedAt:    time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		TTL:         5 * time.Minute,
		UsesAllowed: 2,
	}
}

func TestNewIssuerRejectsShortSecret(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{BaseURL: "https://x", Secret: []byte("short")}); !errors.Is(err, ErrSecretTooShort) {
		t.Fatalf("err = %v, want ErrSecretTooShort", err)
	}
}

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	iss := newTestIssuer(t)
	res, err := iss.Issue(validIssueRequest())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if res.URL == "" {
		t.Fatal("URL empty")
	}
	if res.Token.LicenseID() != validIssueRequest().LicenseID {
		t.Fatalf("token license mismatch")
	}
	now := validIssueRequest().IssuedAt.Add(time.Second)
	claims, err := iss.Verify(res.URL, now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.TenantID != "tenant-a" {
		t.Fatalf("tenant = %q", claims.TenantID)
	}
}

func TestVerifyExpiry(t *testing.T) {
	t.Parallel()
	iss := newTestIssuer(t)
	req := validIssueRequest()
	res, err := iss.Issue(req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Just before expiry: ok.
	if _, err := iss.Verify(res.URL, req.IssuedAt.Add(req.TTL).Add(-time.Second)); err != nil {
		t.Fatalf("Verify just-before-expiry: %v", err)
	}
	// Exactly at expiry: ErrTokenExpired (boundary).
	if _, err := iss.Verify(res.URL, req.IssuedAt.Add(req.TTL)); !errors.Is(err, digital.ErrTokenExpired) {
		t.Fatalf("Verify at-expiry: %v, want ErrTokenExpired", err)
	}
	// One second after expiry: ErrTokenExpired.
	if _, err := iss.Verify(res.URL, req.IssuedAt.Add(req.TTL).Add(time.Second)); !errors.Is(err, digital.ErrTokenExpired) {
		t.Fatalf("Verify after-expiry: %v, want ErrTokenExpired", err)
	}
}

func TestVerifyTamperedSignature(t *testing.T) {
	t.Parallel()
	iss := newTestIssuer(t)
	res, err := iss.Issue(validIssueRequest())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Replace the trailing sig= byte with something else.
	tampered := strings.Replace(res.URL, "sig=", "sig=AA", 1)
	if _, err := iss.Verify(tampered, validIssueRequest().IssuedAt.Add(time.Second)); !errors.Is(err, digital.ErrInvalidLicense) {
		t.Fatalf("tampered Verify err = %v, want ErrInvalidLicense", err)
	}
}

func TestVerifyCrossTenantAttempt(t *testing.T) {
	t.Parallel()
	iss := newTestIssuer(t)
	req := validIssueRequest()
	res, err := iss.Issue(req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Mutate tid in the URL but keep sig intact: signature must fail.
	parsed, _ := url.Parse(res.URL)
	q := parsed.Query()
	q.Set("tid", "tenant-b")
	parsed.RawQuery = q.Encode()
	if _, err := iss.Verify(parsed.String(), req.IssuedAt.Add(time.Second)); !errors.Is(err, digital.ErrInvalidLicense) {
		t.Fatalf("cross-tenant Verify err = %v, want ErrInvalidLicense", err)
	}
}

func TestVerifyMissingSignature(t *testing.T) {
	t.Parallel()
	iss := newTestIssuer(t)
	res, err := iss.Issue(validIssueRequest())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	parsed, _ := url.Parse(res.URL)
	q := parsed.Query()
	q.Del("sig")
	parsed.RawQuery = q.Encode()
	if _, err := iss.Verify(parsed.String(), validIssueRequest().IssuedAt.Add(time.Second)); !errors.Is(err, ErrSignatureMissing) {
		t.Fatalf("missing-sig err = %v, want ErrSignatureMissing", err)
	}
}

func TestVerifyMalformedURL(t *testing.T) {
	t.Parallel()
	iss := newTestIssuer(t)
	if _, err := iss.Verify("https://example.com/?lid=not-a-uuid&pid=x&tid=t&exp=1&uses=1&sig=abc", time.Now()); err == nil {
		t.Fatal("expected error on malformed lid")
	}
}

func TestVerifyMaxUsesIsCarriedThrough(t *testing.T) {
	t.Parallel()
	iss := newTestIssuer(t)
	req := validIssueRequest()
	req.UsesAllowed = 3
	res, err := iss.Issue(req)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if res.Token.UsesAllowed() != 3 {
		t.Fatalf("token UsesAllowed = %d, want 3", res.Token.UsesAllowed())
	}
}
