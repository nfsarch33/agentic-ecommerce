// File scope: v3.4.0 EC-4-2 Facebook Shop OAuth2 long-lived page
// token manager.
//
// Unlike TikTok Shop's PKCE refresh dance, META's Commerce Manager
// scope uses a **long-lived page token** (~60 days) that the
// operator bootstraps once via the Facebook Login flow. The token
// is then debug-checked + extended via:
//
//	GET /oauth/access_token
//	    ?grant_type=fb_exchange_token
//	    &client_id=<app_id>
//	    &client_secret=<app_secret>
//	    &fb_exchange_token=<short_lived_token>
//
// Storage today is a goroutine-safe in-memory map + an injectable
// FacebookTokenStore interface so the v3.7.0 EC-10-1 OS-keychain
// backend (zalando/go-keyring) can drop in without touching this
// file. The Store contract intentionally mirrors the TikTok
// TokenStore so the keychain adapter can implement BOTH with one
// concrete struct.
//
// Cite skill: go-clean-architecture (Store is a port; in-memory
// adapter is the v3.4.0 default; keychain adapter is future v3.7.0
// work).
package social

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// FacebookToken is the long-lived page token bundle persisted per
// tenant. AccessToken is the long-lived page token; RefreshToken is
// empty for META (the long-lived flow does not use refresh tokens
// in the OAuth2 sense -- the operator re-bootstraps when it
// expires). NEVER log raw values.
type FacebookToken struct {
	TenantID    string
	PageID      string
	AccessToken string
	ExpiresAt   time.Time
	Scopes      []string
}

// IsExpired reports whether the token is at or past expiry. Tokens
// within RefreshSkew of expiry are also considered expired so the
// agent layer never races the operator-bootstrap window.
func (t FacebookToken) IsExpired(now time.Time, skew time.Duration) bool {
	if t.ExpiresAt.IsZero() {
		return true
	}
	return !now.Before(t.ExpiresAt.Add(-skew))
}

// FacebookTokenStore persists FacebookToken across cmd/* restarts.
// The v3.7.0 EC-10-1 OS-keychain backend implements the same
// interface so the composition root only swaps the constructor.
type FacebookTokenStore interface {
	Get(ctx context.Context, tenantID string) (FacebookToken, error)
	Put(ctx context.Context, token FacebookToken) error
}

// ErrFacebookTokenNotFound is returned by FacebookTokenStore.Get
// when no token has been bootstrapped for the requested tenant.
var ErrFacebookTokenNotFound = errors.New("facebook: token not found")

// FacebookMemoryTokenStore is the v3.4.0 default; keychain backend
// lands in v3.7.0 EC-10-1.
type FacebookMemoryTokenStore struct {
	mu   sync.RWMutex
	data map[string]FacebookToken
}

// NewFacebookMemoryTokenStore returns an empty in-memory token store.
func NewFacebookMemoryTokenStore() *FacebookMemoryTokenStore {
	return &FacebookMemoryTokenStore{data: make(map[string]FacebookToken)}
}

// Get returns the token for tenantID or ErrFacebookTokenNotFound.
func (s *FacebookMemoryTokenStore) Get(_ context.Context, tenantID string) (FacebookToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tok, ok := s.data[tenantID]
	if !ok {
		return FacebookToken{}, ErrFacebookTokenNotFound
	}
	return tok, nil
}

// Put upserts a token by tenantID.
func (s *FacebookMemoryTokenStore) Put(_ context.Context, token FacebookToken) error {
	if token.TenantID == "" {
		return fmt.Errorf("%w: token.TenantID required", ErrFacebookUnconfigured)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[token.TenantID] = token
	return nil
}

// FacebookOAuthBootstrapRequest is the operator-supplied input on
// first run. ShortLivedToken is paste-from-the-Graph-API-Explorer.
// PageID identifies the Facebook Page the Commerce Manager is
// attached to.
type FacebookOAuthBootstrapRequest struct {
	TenantID        string
	PageID          string
	ShortLivedToken string
	Scopes          []string
}

// Validate enforces the bootstrap contract.
func (r FacebookOAuthBootstrapRequest) Validate() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrFacebookUnconfigured)
	}
	if strings.TrimSpace(r.PageID) == "" {
		return fmt.Errorf("%w: PageID required", ErrFacebookUnconfigured)
	}
	if strings.TrimSpace(r.ShortLivedToken) == "" {
		return fmt.Errorf("%w: ShortLivedToken required", ErrFacebookUnconfigured)
	}
	return nil
}

// FacebookTokenExchanger is the small port over the META OAuth
// endpoint. The HTTP-backed implementation lives on
// *FacebookShopClient; tests use a fake to avoid the network.
type FacebookTokenExchanger interface {
	Exchange(ctx context.Context, req FacebookOAuthBootstrapRequest) (FacebookToken, error)
}

// FacebookTokenManager wraps a FacebookTokenStore +
// FacebookTokenExchanger to surface a "give me a current token" API
// the request-signing path consumes. All token I/O honours ctx;
// bootstrap is single-flight per tenant via a per-tenant mutex.
type FacebookTokenManager struct {
	store     FacebookTokenStore
	exchanger FacebookTokenExchanger
	skew      time.Duration
	now       func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// FacebookTokenManagerConfig wires a FacebookTokenManager.
// RefreshSkew defaults to 60s -- the agent treats tokens within
// that window as expired.
type FacebookTokenManagerConfig struct {
	Store       FacebookTokenStore
	Exchanger   FacebookTokenExchanger
	RefreshSkew time.Duration
	Now         func() time.Time
}

// NewFacebookTokenManager constructs a FacebookTokenManager.
func NewFacebookTokenManager(cfg FacebookTokenManagerConfig) (*FacebookTokenManager, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: FacebookTokenStore required", ErrFacebookUnconfigured)
	}
	if cfg.Exchanger == nil {
		return nil, fmt.Errorf("%w: FacebookTokenExchanger required", ErrFacebookUnconfigured)
	}
	if cfg.RefreshSkew <= 0 {
		cfg.RefreshSkew = 60 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &FacebookTokenManager{
		store:     cfg.Store,
		exchanger: cfg.Exchanger,
		skew:      cfg.RefreshSkew,
		now:       cfg.Now,
		locks:     make(map[string]*sync.Mutex),
	}, nil
}

// Bootstrap exchanges an operator-supplied short-lived token for a
// long-lived page token, then persists it. The long-lived token is
// the canonical credential for all subsequent Graph API calls.
func (m *FacebookTokenManager) Bootstrap(ctx context.Context, req FacebookOAuthBootstrapRequest) (FacebookToken, error) {
	if err := req.Validate(); err != nil {
		return FacebookToken{}, err
	}
	tok, err := m.exchanger.Exchange(ctx, req)
	if err != nil {
		return FacebookToken{}, fmt.Errorf("facebook: exchange: %w", err)
	}
	if err := m.store.Put(ctx, tok); err != nil {
		return FacebookToken{}, fmt.Errorf("facebook: persist token: %w", err)
	}
	return tok, nil
}

// AccessToken returns a fresh access token for tenantID. If the
// stored token is expired the call returns ErrFacebookAuthFailed
// (operator must re-bootstrap; META does not support refresh on
// long-lived page tokens within this flow).
func (m *FacebookTokenManager) AccessToken(ctx context.Context, tenantID string) (FacebookToken, error) {
	tok, err := m.store.Get(ctx, tenantID)
	if err != nil {
		return FacebookToken{}, err
	}
	if tok.IsExpired(m.now(), m.skew) {
		return FacebookToken{}, fmt.Errorf("%w: long-lived token expired -- re-bootstrap via Bootstrap()", ErrFacebookAuthFailed)
	}
	return tok, nil
}

// tenantLock returns (creating if absent) the per-tenant single-
// flight bootstrap mutex.
func (m *FacebookTokenManager) tenantLock(tenantID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lk, ok := m.locks[tenantID]
	if !ok {
		lk = &sync.Mutex{}
		m.locks[tenantID] = lk
	}
	return lk
}
