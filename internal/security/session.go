package security

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var ErrSessionNotFound = errors.New("refresh session not found")

type RefreshSession struct {
	TokenHash string
	Subject   string
	Role      Role
	ExpiresAt time.Time
	CreatedAt time.Time
}

type RefreshSessionStore interface {
	Save(ctx context.Context, session RefreshSession) error
	Get(ctx context.Context, tokenHash string) (RefreshSession, error)
	Revoke(ctx context.Context, tokenHash string) error
}

type InMemoryRefreshSessionStore struct {
	mu       sync.Mutex
	sessions map[string]RefreshSession
	now      func() time.Time
}

func NewInMemoryRefreshSessionStore(now func() time.Time) *InMemoryRefreshSessionStore {
	if now == nil {
		now = time.Now
	}
	return &InMemoryRefreshSessionStore{sessions: make(map[string]RefreshSession), now: now}
}

func (s *InMemoryRefreshSessionStore) Save(_ context.Context, session RefreshSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.TokenHash] = session
	return nil
}

func (s *InMemoryRefreshSessionStore) Get(_ context.Context, tokenHash string) (RefreshSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[tokenHash]
	if !ok || !session.ExpiresAt.After(s.now()) {
		return RefreshSession{}, ErrSessionNotFound
	}
	return session, nil
}

func (s *InMemoryRefreshSessionStore) Revoke(_ context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, tokenHash)
	return nil
}

func NewRefreshToken() (raw string, hash string, err error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf[:])
	return raw, HashRefreshToken(raw), nil
}

func HashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
