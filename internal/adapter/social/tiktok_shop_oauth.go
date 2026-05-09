// File scope: v3.3.0 EC-3-1 TikTok Shop OAuth2 PKCE token manager.
//
// The token is operator-bootstrapped (we redirect the operator
// through TikTok's OAuth screen out-of-band, then they paste the
// authorization_code into the cmd/ec-cli bootstrap command). From
// there the manager handles refresh + persistence so the listing
// agent never sees an expired token.
//
// Storage today is a goroutine-safe in-memory map + an injectable
// Store interface so the v3.7.0 EC-10-1 OS-keychain backend can
// drop in without touching this file. The Store interface is the
// minimal contract -- two methods, both context-cancellable -- so
// the keychain adapter ships as a small composition root change.
//
// Cite skill: go-clean-architecture (the Store interface is a port;
// MemoryStore is the in-memory adapter; the keychain adapter is
// future v3.7.0 work).
package social

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// TikTokToken is the OAuth2 token bundle we persist per-tenant.
// AccessToken expires; the manager refreshes via RefreshToken
// before expiry. NEVER log the raw values: callers wanting
// observability should hash them via the v2.10 sentrux fingerprint
// helper.
type TikTokToken struct {
	TenantID     string
	ShopID       string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Scope        string
}

// IsExpired reports whether the access token is at or past expiry.
// Tokens within RefreshSkew of expiry are also considered expired
// so the agent layer never races the refresh window.
func (t TikTokToken) IsExpired(now time.Time, skew time.Duration) bool {
	if t.ExpiresAt.IsZero() {
		return true
	}
	return !now.Before(t.ExpiresAt.Add(-skew))
}

// TokenStore persists TikTokToken across cmd/* restarts. The
// v3.7.0 EC-10-1 OS-keychain backend implements the same
// interface so the composition root only swaps the constructor.
type TokenStore interface {
	Get(ctx context.Context, tenantID string) (TikTokToken, error)
	Put(ctx context.Context, token TikTokToken) error
}

// ErrTokenNotFound is returned by TokenStore.Get when no token has
// been bootstrapped for the requested tenant.
var ErrTokenNotFound = errors.New("tiktok: token not found")

// MemoryTokenStore is the v3.3.0 default; keychain backend lands in
// v3.7.0 EC-10-1.
type MemoryTokenStore struct {
	mu   sync.RWMutex
	data map[string]TikTokToken
}

// NewMemoryTokenStore returns an empty in-memory TokenStore.
func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{data: make(map[string]TikTokToken)}
}

// Get returns the token for tenantID or ErrTokenNotFound.
func (s *MemoryTokenStore) Get(_ context.Context, tenantID string) (TikTokToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tok, ok := s.data[tenantID]
	if !ok {
		return TikTokToken{}, ErrTokenNotFound
	}
	return tok, nil
}

// Put upserts a token by tenantID.
func (s *MemoryTokenStore) Put(_ context.Context, token TikTokToken) error {
	if token.TenantID == "" {
		return fmt.Errorf("%w: token.TenantID required", ErrTikTokUnconfigured)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[token.TenantID] = token
	return nil
}

// PKCEPair carries the verifier + S256 challenge the OAuth2
// authorization-code-with-PKCE flow needs. Verifier is kept on
// the local side and submitted on the token exchange.
type PKCEPair struct {
	Verifier  string
	Challenge string
}

// NewPKCEPair returns a fresh verifier + S256 challenge using
// crypto/rand. The 32-byte verifier maps to a 43-char URL-safe
// challenge, well above the spec's 43..128 floor.
func NewPKCEPair() (PKCEPair, error) {
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return PKCEPair{}, fmt.Errorf("tiktok: pkce verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return PKCEPair{Verifier: verifier, Challenge: challenge}, nil
}

// OAuthBootstrapRequest is the operator-supplied input on first
// run. AuthorizationCode is paste-from-browser. Verifier comes from
// the same NewPKCEPair the operator used to build the auth URL.
type OAuthBootstrapRequest struct {
	TenantID          string
	ShopID            string
	AuthorizationCode string
	Verifier          string
	Scope             string
}

// Validate enforces the bootstrap contract.
func (r OAuthBootstrapRequest) Validate() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrTikTokUnconfigured)
	}
	if strings.TrimSpace(r.AuthorizationCode) == "" {
		return fmt.Errorf("%w: AuthorizationCode required", ErrTikTokUnconfigured)
	}
	if strings.TrimSpace(r.Verifier) == "" {
		return fmt.Errorf("%w: PKCE Verifier required", ErrTikTokUnconfigured)
	}
	return nil
}

// TokenExchanger is the small port over the TikTok OAuth endpoint.
// The HTTP-backed implementation lives on *TikTokShopClient;
// tests use a fake to avoid the network.
type TokenExchanger interface {
	Exchange(ctx context.Context, req OAuthBootstrapRequest) (TikTokToken, error)
	Refresh(ctx context.Context, refreshToken, tenantID string) (TikTokToken, error)
}

// TokenManager wraps a TokenStore + TokenExchanger to surface a
// "give me a current token" API the request-signing path consumes.
// All token I/O honours ctx; refresh is single-flight per tenant
// via a per-tenant mutex.
type TokenManager struct {
	store     TokenStore
	exchanger TokenExchanger
	skew      time.Duration
	now       func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// TokenManagerConfig wires a TokenManager. RefreshSkew defaults to
// 60s -- the agent treats tokens within that window as expired.
type TokenManagerConfig struct {
	Store       TokenStore
	Exchanger   TokenExchanger
	RefreshSkew time.Duration
	Now         func() time.Time
}

// NewTokenManager constructs a TokenManager.
func NewTokenManager(cfg TokenManagerConfig) (*TokenManager, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: TokenStore required", ErrTikTokUnconfigured)
	}
	if cfg.Exchanger == nil {
		return nil, fmt.Errorf("%w: TokenExchanger required", ErrTikTokUnconfigured)
	}
	if cfg.RefreshSkew <= 0 {
		cfg.RefreshSkew = 60 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &TokenManager{
		store:     cfg.Store,
		exchanger: cfg.Exchanger,
		skew:      cfg.RefreshSkew,
		now:       cfg.Now,
		locks:     make(map[string]*sync.Mutex),
	}, nil
}

// Bootstrap exchanges an operator-supplied authorization_code for
// the initial access + refresh tokens, then persists them.
func (m *TokenManager) Bootstrap(ctx context.Context, req OAuthBootstrapRequest) (TikTokToken, error) {
	if err := req.Validate(); err != nil {
		return TikTokToken{}, err
	}
	tok, err := m.exchanger.Exchange(ctx, req)
	if err != nil {
		return TikTokToken{}, fmt.Errorf("tiktok: exchange: %w", err)
	}
	if err := m.store.Put(ctx, tok); err != nil {
		return TikTokToken{}, fmt.Errorf("tiktok: persist token: %w", err)
	}
	return tok, nil
}

// AccessToken returns a fresh access token for tenantID, refreshing
// when necessary. Refresh is single-flight per tenant.
func (m *TokenManager) AccessToken(ctx context.Context, tenantID string) (TikTokToken, error) {
	tok, err := m.store.Get(ctx, tenantID)
	if err != nil {
		return TikTokToken{}, err
	}
	if !tok.IsExpired(m.now(), m.skew) {
		return tok, nil
	}
	tenantLock := m.tenantLock(tenantID)
	tenantLock.Lock()
	defer tenantLock.Unlock()
	// Double-check under the lock so a concurrent refresh winner
	// is observed.
	tok, err = m.store.Get(ctx, tenantID)
	if err != nil {
		return TikTokToken{}, err
	}
	if !tok.IsExpired(m.now(), m.skew) {
		return tok, nil
	}
	refreshed, err := m.exchanger.Refresh(ctx, tok.RefreshToken, tenantID)
	if err != nil {
		return TikTokToken{}, fmt.Errorf("tiktok: refresh: %w", err)
	}
	if refreshed.TenantID == "" {
		refreshed.TenantID = tenantID
	}
	if err := m.store.Put(ctx, refreshed); err != nil {
		return TikTokToken{}, fmt.Errorf("tiktok: persist refreshed token: %w", err)
	}
	return refreshed, nil
}

// tenantLock returns (creating if absent) the per-tenant single-
// flight refresh mutex. Held inside m.mu only long enough to read
// or insert the inner mutex.
func (m *TokenManager) tenantLock(tenantID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lk, ok := m.locks[tenantID]
	if !ok {
		lk = &sync.Mutex{}
		m.locks[tenantID] = lk
	}
	return lk
}
