package security

import (
	"strings"
	"testing"
	"time"
)

func TestTokenManagerMintsAndVerifiesAccessToken(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	manager, err := NewTokenManager(TokenConfig{
		Secret:    "test-secret-at-least-32-bytes-long",
		Issuer:    "agentic-ecommerce",
		Audience:  "mc-api",
		AccessTTL: 15 * time.Minute,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	token, err := manager.MintAccessToken(Principal{Subject: "admin@example.com", Role: RoleAdmin})
	if err != nil {
		t.Fatalf("MintAccessToken: %v", err)
	}

	claims, err := manager.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if claims.Subject != "admin@example.com" || claims.Role != RoleAdmin {
		t.Fatalf("claims principal = subject %q role %q", claims.Subject, claims.Role)
	}
	if !claims.ExpiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expires_at = %s, want %s", claims.ExpiresAt, now.Add(15*time.Minute))
	}
}

func TestTokenManagerRejectsTamperedAccessToken(t *testing.T) {
	t.Parallel()
	manager, err := NewTokenManager(TokenConfig{
		Secret:    "test-secret-at-least-32-bytes-long",
		Issuer:    "agentic-ecommerce",
		Audience:  "mc-api",
		AccessTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	token, err := manager.MintAccessToken(Principal{Subject: "operator@example.com", Role: RoleOperator})
	if err != nil {
		t.Fatalf("MintAccessToken: %v", err)
	}

	tampered := token[:len(token)-1] + differentTokenByte(token[len(token)-1])
	if _, err := manager.VerifyAccessToken(tampered); err == nil {
		t.Fatal("VerifyAccessToken accepted a tampered token")
	}
}

func differentTokenByte(current byte) string {
	if current == 'x' {
		return "y"
	}
	return "x"
}

func TestTokenManagerRejectsExpiredAccessToken(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	current := now
	manager, err := NewTokenManager(TokenConfig{
		Secret:    "test-secret-at-least-32-bytes-long",
		Issuer:    "agentic-ecommerce",
		Audience:  "mc-api",
		AccessTTL: time.Minute,
		Now:       func() time.Time { return current },
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	token, err := manager.MintAccessToken(Principal{Subject: "viewer@example.com", Role: RoleViewer})
	if err != nil {
		t.Fatalf("MintAccessToken: %v", err)
	}

	current = now.Add(2 * time.Minute)
	if _, err := manager.VerifyAccessToken(token); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("VerifyAccessToken error = %v, want expired", err)
	}
}

func TestRoleAllowsHierarchy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		role     Role
		required Role
		want     bool
	}{
		{name: "admin can operate", role: RoleAdmin, required: RoleOperator, want: true},
		{name: "operator can view", role: RoleOperator, required: RoleViewer, want: true},
		{name: "viewer cannot operate", role: RoleViewer, required: RoleOperator, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.role.Allows(tt.required); got != tt.want {
				t.Fatalf("%s.Allows(%s) = %v, want %v", tt.role, tt.required, got, tt.want)
			}
		})
	}
}
