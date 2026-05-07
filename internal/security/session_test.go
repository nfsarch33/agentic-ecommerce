package security

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryRefreshSessionStoreSavesGetsAndRevokes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	store := NewInMemoryRefreshSessionStore(func() time.Time { return now })
	session := RefreshSession{
		TokenHash: "hash",
		Subject:   "admin@example.com",
		Role:      RoleAdmin,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}

	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Get(context.Background(), "hash")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Subject != session.Subject || got.Role != session.Role {
		t.Fatalf("session = %+v, want %+v", got, session)
	}
	if err := store.Revoke(context.Background(), "hash"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := store.Get(context.Background(), "hash"); err != ErrSessionNotFound {
		t.Fatalf("Get revoked error = %v, want ErrSessionNotFound", err)
	}
}

func TestInMemoryRefreshSessionStoreRejectsExpired(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	store := NewInMemoryRefreshSessionStore(func() time.Time { return now })
	if err := store.Save(context.Background(), RefreshSession{
		TokenHash: "hash",
		Subject:   "viewer@example.com",
		Role:      RoleViewer,
		CreatedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.Get(context.Background(), "hash"); err != ErrSessionNotFound {
		t.Fatalf("Get expired error = %v, want ErrSessionNotFound", err)
	}
}

func TestNewRefreshTokenHashesOpaqueValue(t *testing.T) {
	t.Parallel()
	raw, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	if raw == "" || hash == "" || raw == hash {
		t.Fatalf("raw/hash = %q/%q", raw, hash)
	}
	if got := HashRefreshToken(raw); got != hash {
		t.Fatalf("HashRefreshToken = %q, want %q", got, hash)
	}
}
