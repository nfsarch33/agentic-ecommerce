package security

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

// File scope: targeted coverage for previously-uncovered token helpers
// (AccessTTL, randomID exhaustion, decodeSegment errors) and refresh
// session expiry semantics.

const tokenSecretFixture = "supersecret-32-bytes-test-secret-001"

func newFixtureManager(t *testing.T) *TokenManager {
	t.Helper()
	mgr, err := NewTokenManager(TokenConfig{
		Secret:    tokenSecretFixture,
		Issuer:    "agentic-test",
		Audience:  "mc-api-test",
		AccessTTL: 30 * time.Minute,
		Now:       func() time.Time { return time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	return mgr
}

func TestTokenManagerAccessTTLReturnsConfiguredValue(t *testing.T) {
	t.Parallel()

	mgr := newFixtureManager(t)
	if got := mgr.AccessTTL(); got != 30*time.Minute {
		t.Fatalf("AccessTTL = %s, want 30m", got)
	}

	if got := (*TokenManager)(nil).AccessTTL(); got != 0 {
		t.Fatalf("nil manager AccessTTL = %s, want 0", got)
	}
}

func TestNewTokenManagerRejectsShortSecret(t *testing.T) {
	t.Parallel()

	_, err := NewTokenManager(TokenConfig{Secret: "too-short"})
	if err == nil {
		t.Fatal("expected NewTokenManager to reject short secret")
	}
}

func TestNewTokenManagerAppliesDefaultsWhenFieldsBlank(t *testing.T) {
	t.Parallel()

	mgr, err := NewTokenManager(TokenConfig{Secret: tokenSecretFixture})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	if mgr.issuer != "agentic-ecommerce" || mgr.audience != "mc-api" {
		t.Fatalf("manager defaults issuer/audience = %q/%q", mgr.issuer, mgr.audience)
	}
	if mgr.accessTTL != 15*time.Minute {
		t.Fatalf("accessTTL = %s, want 15m", mgr.accessTTL)
	}
}

func TestDecodeSegmentRejectsInvalidBase64(t *testing.T) {
	t.Parallel()

	var dst map[string]any
	if err := decodeSegment("not%%valid%%", &dst); err == nil {
		t.Fatal("expected base64 decode error")
	}
}

func TestDecodeSegmentRejectsNonJSONPayload(t *testing.T) {
	t.Parallel()

	encoded := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
	var dst map[string]any
	if err := decodeSegment(encoded, &dst); err == nil {
		t.Fatal("expected JSON unmarshal error")
	}
}

func TestRandomIDProducesDeterministicLengthAndUnique(t *testing.T) {
	t.Parallel()

	a, err := randomID()
	if err != nil {
		t.Fatalf("randomID a: %v", err)
	}
	b, err := randomID()
	if err != nil {
		t.Fatalf("randomID b: %v", err)
	}

	if a == b {
		t.Fatal("two consecutive randomIDs collided; expected fresh entropy")
	}

	// 16 random bytes encoded with RawURLEncoding -> 22 chars.
	const wantLen = 22
	if len(a) != wantLen || len(b) != wantLen {
		t.Fatalf("randomID lengths = %d, %d, want %d each", len(a), len(b), wantLen)
	}
}

func TestVerifyAccessTokenRejectsExpiredToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	mgr, err := NewTokenManager(TokenConfig{
		Secret:    tokenSecretFixture,
		AccessTTL: time.Minute,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	token, err := mgr.MintAccessToken(Principal{Subject: "user-42", Role: RoleOperator, TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("MintAccessToken: %v", err)
	}

	mgr.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := mgr.VerifyAccessToken(token); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("err = %v, want ErrExpiredToken", err)
	}
}

func TestInMemoryRefreshSessionStoreReturnsNotFoundAfterExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	store := NewInMemoryRefreshSessionStore(func() time.Time { return now })

	expired := RefreshSession{
		TokenHash: "hash-expired",
		Subject:   "user",
		Role:      RoleOperator,
		ExpiresAt: now.Add(-time.Minute),
		CreatedAt: now.Add(-time.Hour),
	}
	if err := store.Save(context.Background(), expired); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := store.Get(context.Background(), "hash-expired"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Get expired = %v, want ErrSessionNotFound", err)
	}
}

func TestInMemoryRefreshSessionStoreReturnsNotFoundWhenMissing(t *testing.T) {
	t.Parallel()

	store := NewInMemoryRefreshSessionStore(nil)
	if _, err := store.Get(context.Background(), "no-such-hash"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestNewRefreshTokenReturnsHashThatMatchesHashRefreshToken(t *testing.T) {
	t.Parallel()

	raw, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	if HashRefreshToken(raw) != hash {
		t.Fatalf("derived hash mismatch")
	}
}

func TestParseRoleAcceptsKnownValuesAndRejectsUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    Role
		wantErr bool
	}{
		{name: "operator", input: "operator", want: RoleOperator},
		{name: "admin", input: "admin", want: RoleAdmin},
		{name: "viewer", input: "viewer", want: RoleViewer},
		{name: "uppercase trims case", input: "ADMIN", want: RoleAdmin},
		{name: "unknown rejected", input: "ghost", wantErr: true},
		{name: "empty rejected", input: "", wantErr: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRole(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseRole(%q) returned %v, want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRole(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParseRole(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestRoleAllowsRespectsHierarchy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		holder, required Role
		want             bool
	}{
		{holder: RoleAdmin, required: RoleViewer, want: true},
		{holder: RoleAdmin, required: RoleOperator, want: true},
		{holder: RoleAdmin, required: RoleAdmin, want: true},
		{holder: RoleOperator, required: RoleViewer, want: true},
		{holder: RoleOperator, required: RoleAdmin, want: false},
		{holder: RoleViewer, required: RoleOperator, want: false},
	}
	for _, tc := range tests {
		tc := tc
		name := string(tc.holder) + ">=" + string(tc.required)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.holder.Allows(tc.required); got != tc.want {
				t.Fatalf("%q.Allows(%q) = %v, want %v", tc.holder, tc.required, got, tc.want)
			}
		})
	}
}
