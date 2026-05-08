package registration

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// testRegistrationSecret is a deterministic 32-byte fixture; the
// kebab-case form deliberately avoids the high-entropy hex pattern
// gitleaks flags as a generic API key. gitleaks:allow
const testRegistrationSecret = "test-registration-hmac-secret32!"

func TestNewRequestRequiresEmail(t *testing.T) {
	t.Parallel()
	cases := []SubmitInput{
		{Email: "", SlugRequested: "tenant-a"},
		{Email: "  ", SlugRequested: "tenant-a"},
		{Email: "no-at-sign", SlugRequested: "tenant-a"},
		{Email: "missing-tld@ex", SlugRequested: "tenant-a"},
	}
	for i, in := range cases {
		_, err := NewRequest(in, time.Hour)
		if !errors.Is(err, ErrEmailRequired) {
			t.Fatalf("case %d expected ErrEmailRequired, got %v", i, err)
		}
	}
}

func TestNewRequestRequiresSlug(t *testing.T) {
	t.Parallel()
	cases := []SubmitInput{
		{Email: "alice@example.com", SlugRequested: ""},
		{Email: "alice@example.com", SlugRequested: "BAD-CASE"},
		{Email: "alice@example.com", SlugRequested: "x"},
		{Email: "alice@example.com", SlugRequested: "1starts-digit"},
		{Email: "alice@example.com", SlugRequested: "ends-with-dash-"},
	}
	for _, in := range cases {
		_, err := NewRequest(in, time.Hour)
		if !errors.Is(err, ErrSlugRequired) {
			t.Fatalf("expected ErrSlugRequired for %q, got %v", in.SlugRequested, err)
		}
	}
}

func TestNewRequestDefaultsPlan(t *testing.T) {
	t.Parallel()
	r, err := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, 0)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if r.PlanRequested != "free" {
		t.Fatalf("plan = %s, want free", r.PlanRequested)
	}
	if r.Status != StatusPendingEmailVerification {
		t.Fatalf("status = %s", r.Status)
	}
}

func TestRequestStateMachine(t *testing.T) {
	t.Parallel()
	r, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	verified, err := r.MarkVerified(time.Now())
	if err != nil {
		t.Fatalf("MarkVerified: %v", err)
	}
	if verified.Status != StatusEmailVerified {
		t.Fatalf("status after verify = %s", verified.Status)
	}
	if _, err := verified.MarkVerified(time.Now()); !errors.Is(err, ErrAlreadyVerified) {
		t.Fatalf("expected ErrAlreadyVerified, got %v", err)
	}
	onboarding, err := verified.MarkOnboarding("Acme Co", time.Now())
	if err != nil {
		t.Fatalf("MarkOnboarding: %v", err)
	}
	if onboarding.Status != StatusOnboarding {
		t.Fatalf("status onboarding = %s", onboarding.Status)
	}
	active, err := onboarding.MarkActive("tenant-a", time.Now())
	if err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	if active.Status != StatusActive {
		t.Fatalf("status active = %s", active.Status)
	}
	if _, err := active.MarkActive("tenant-a", time.Now()); !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("expected ErrAlreadyActive, got %v", err)
	}
	if !active.Status.IsTerminal() {
		t.Fatalf("active must be terminal")
	}
}

func TestNewIssuerRejectsShortSecret(t *testing.T) {
	t.Parallel()
	if _, err := NewIssuer([]byte("short")); !errors.Is(err, ErrSecretTooShort) {
		t.Fatalf("expected ErrSecretTooShort, got %v", err)
	}
}

func TestIssueAndVerifyToken(t *testing.T) {
	t.Parallel()
	issuer, err := NewIssuer([]byte(testRegistrationSecret))
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	r, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	token, err := issuer.IssueToken(r)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	id, err := issuer.VerifyToken(token, r.CreatedAt.Add(time.Minute), r.Email)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if id != r.ID {
		t.Fatalf("id mismatch")
	}
}

func TestVerifyTokenTampered(t *testing.T) {
	t.Parallel()
	issuer, _ := NewIssuer([]byte(testRegistrationSecret))
	r, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	token, _ := issuer.IssueToken(r)
	tampered := strings.TrimSuffix(token, "x") + "y"
	if _, err := issuer.VerifyToken(tampered, r.CreatedAt, r.Email); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestVerifyTokenWrongEmail(t *testing.T) {
	t.Parallel()
	issuer, _ := NewIssuer([]byte(testRegistrationSecret))
	r, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	token, _ := issuer.IssueToken(r)
	if _, err := issuer.VerifyToken(token, r.CreatedAt, "bob@example.com"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for wrong email, got %v", err)
	}
}

func TestVerifyTokenExpired(t *testing.T) {
	t.Parallel()
	issuer, _ := NewIssuer([]byte(testRegistrationSecret))
	r, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	token, _ := issuer.IssueToken(r)
	if _, err := issuer.VerifyToken(token, r.ExpiresAt.Add(time.Minute), r.Email); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestVerifyTokenMalformed(t *testing.T) {
	t.Parallel()
	issuer, _ := NewIssuer([]byte(testRegistrationSecret))
	if _, err := issuer.VerifyToken("not-a-token", time.Now(), "x"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}
