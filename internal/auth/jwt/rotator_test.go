package jwt

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	secretV0 = "v0-secret-32-bytes-padding-aaaaaa"
	secretV1 = "v1-secret-32-bytes-padding-bbbbbb"
	secretV2 = "v2-secret-32-bytes-padding-cccccc"
)

func newRotatorForTest(t *testing.T, now time.Time) *Rotator {
	t.Helper()
	r, err := NewRotator(Config{
		Keys: []Key{
			{Version: "v1", Secret: []byte(secretV1)},
		},
		ActiveVersion: "v1",
		Issuer:        "agentic-ecommerce",
		Audience:      "mc-api",
		AccessTTL:     5 * time.Minute,
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	return r
}

func TestRotator_RejectsShortSecret(t *testing.T) {
	t.Parallel()
	_, err := NewRotator(Config{
		Keys:          []Key{{Version: "v1", Secret: []byte("too-short")}},
		ActiveVersion: "v1",
	})
	if !errors.Is(err, ErrRotatorBadInput) {
		t.Fatalf("err=%v want ErrRotatorBadInput", err)
	}
}

func TestRotator_RejectsMissingActive(t *testing.T) {
	t.Parallel()
	_, err := NewRotator(Config{
		Keys:          []Key{{Version: "v1", Secret: []byte(secretV1)}},
		ActiveVersion: "v9",
	})
	if !errors.Is(err, ErrRotatorBadInput) {
		t.Fatalf("err=%v want ErrRotatorBadInput", err)
	}
}

func TestRotator_MintAndVerifyHappyPath(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC)
	r := newRotatorForTest(t, now)
	tok, err := r.Mint(MintRequest{Subject: "ops@example.com"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	claims, err := r.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "ops@example.com" {
		t.Fatalf("Subject=%q", claims.Subject)
	}
	if claims.Version != "v1" {
		t.Fatalf("Version=%q want v1", claims.Version)
	}
	if !claims.ExpiresAt.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("ExpiresAt=%s", claims.ExpiresAt)
	}
}

func TestRotator_V1TokenSurvivesRotationToV2WithinGrace(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC)
	current := now
	r, err := NewRotator(Config{
		Keys: []Key{
			{Version: "v1", Secret: []byte(secretV1), NotAfter: now.Add(2 * time.Hour)},
			{Version: "v2", Secret: []byte(secretV2)},
		},
		ActiveVersion: "v1",
		AccessTTL:     30 * time.Minute,
		Now:           func() time.Time { return current },
	})
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	tok, err := r.Mint(MintRequest{Subject: "ops@example.com"})
	if err != nil {
		t.Fatalf("Mint v1: %v", err)
	}
	if err := r.SetActive("v2"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	current = now.Add(15 * time.Minute) // inside the v1 NotAfter grace window
	claims, err := r.Verify(tok)
	if err != nil {
		t.Fatalf("Verify within grace: %v", err)
	}
	if claims.Version != "v1" {
		t.Fatalf("Version=%q want v1 (kid preserved)", claims.Version)
	}
	tok2, err := r.Mint(MintRequest{Subject: "ops@example.com"})
	if err != nil {
		t.Fatalf("Mint v2: %v", err)
	}
	claims2, err := r.Verify(tok2)
	if err != nil {
		t.Fatalf("Verify v2: %v", err)
	}
	if claims2.Version != "v2" {
		t.Fatalf("Version=%q want v2", claims2.Version)
	}
}

func TestRotator_V0RejectedAfterRetirement(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC)
	current := now
	r, err := NewRotator(Config{
		Keys: []Key{
			// v0 has already expired -- token signed with this key
			// must be rejected even if signature matches.
			{Version: "v0", Secret: []byte(secretV0), NotAfter: now.Add(-time.Second)},
			{Version: "v1", Secret: []byte(secretV1)},
		},
		ActiveVersion: "v1",
		AccessTTL:     30 * time.Minute,
		Now:           func() time.Time { return current },
	})
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	// Sign manually using v0 by temporarily swapping active to v0,
	// then flip back so v0 is the retired key.
	if err := r.SetActive("v0"); err != nil {
		t.Fatalf("SetActive v0: %v", err)
	}
	v0Tok, err := r.Mint(MintRequest{Subject: "legacy@example.com"})
	if err != nil {
		t.Fatalf("Mint v0: %v", err)
	}
	if err := r.SetActive("v1"); err != nil {
		t.Fatalf("SetActive v1: %v", err)
	}
	current = now.Add(time.Minute)
	if _, err := r.Verify(v0Tok); !errors.Is(err, ErrExpiredKey) {
		t.Fatalf("Verify v0 err=%v want ErrExpiredKey", err)
	}
}

func TestRotator_RejectsTamperedSignature(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC)
	r := newRotatorForTest(t, now)
	tok, err := r.Mint(MintRequest{Subject: "ops@example.com"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token shape = %q", tok)
	}
	parts[2] = flipFirstByte(parts[2])
	tampered := strings.Join(parts, ".")
	if _, err := r.Verify(tampered); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify tampered err=%v want ErrInvalidToken", err)
	}
}

func TestRotator_VerifyExpiredTokenReturnsExpiredSentinel(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC)
	current := now
	r, err := NewRotator(Config{
		Keys:          []Key{{Version: "v1", Secret: []byte(secretV1)}},
		ActiveVersion: "v1",
		AccessTTL:     time.Minute,
		Now:           func() time.Time { return current },
	})
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	tok, err := r.Mint(MintRequest{Subject: "ops@example.com"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	current = now.Add(2 * time.Minute)
	if _, err := r.Verify(tok); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Verify expired err=%v want ErrExpiredToken", err)
	}
}

func TestRotator_VerifyUnknownKid(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC)
	r := newRotatorForTest(t, now)
	tok, err := r.Mint(MintRequest{Subject: "ops@example.com"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	// Manually rewrite header to claim an unknown kid.
	parts := strings.Split(tok, ".")
	parts[0] = encodeUnknownKidHeader(t)
	rebuilt := parts[0] + "." + parts[1] + "." + parts[2]
	if _, err := r.Verify(rebuilt); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Verify unknown kid err=%v want ErrUnknownKey", err)
	}
}

func TestRotator_ExtraClaimsRoundtrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC)
	r := newRotatorForTest(t, now)
	tok, err := r.Mint(MintRequest{
		Subject: "ops@example.com",
		Extra:   map[string]string{"role": "operator", "tenant_id": "t-1"},
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	claims, err := r.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Extra["role"] != "operator" || claims.Extra["tenant_id"] != "t-1" {
		t.Fatalf("Extra=%v", claims.Extra)
	}
}

func TestRotator_AddKeyAndVersions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC)
	r := newRotatorForTest(t, now)
	if err := r.AddKey(Key{Version: "v2", Secret: []byte(secretV2)}); err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	versions := r.Versions()
	if len(versions) != 2 || versions[0] != "v1" || versions[1] != "v2" {
		t.Fatalf("Versions=%v want [v1 v2]", versions)
	}
	if err := r.SetActive("v2"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if got := r.ActiveVersion(); got != "v2" {
		t.Fatalf("ActiveVersion=%q", got)
	}
}

// --- helpers --------------------------------------------------------------

func flipFirstByte(s string) string {
	if s == "" {
		return s
	}
	if s[0] == 'a' {
		return "b" + s[1:]
	}
	return "a" + s[1:]
}

func encodeUnknownKidHeader(t *testing.T) string {
	t.Helper()
	header := jwtHeader{Algorithm: "HS256", Type: "JWT", KeyID: "v9"}
	return mustEncodeHeader(t, header)
}

func mustEncodeHeader(t *testing.T, h jwtHeader) string {
	t.Helper()
	out, err := signJWT(h, jwtBody{Subject: "x", Issuer: "y", Audience: "z", IssuedAt: 1, NotBefore: 1, ExpiresAt: 2, ID: "id"}, []byte(secretV1))
	if err != nil {
		t.Fatalf("encode header helper: %v", err)
	}
	parts := strings.Split(out, ".")
	return parts[0]
}
