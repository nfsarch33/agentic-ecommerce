// Package oauth2 provides multi-provider OAuth2 with PKCE and automatic token refresh.
package oauth2

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrTokenExpired is returned when the stored token has expired and refresh fails.
var ErrTokenExpired = errors.New("oauth2: token expired")

// ErrNoRefreshToken is returned when no refresh token is available.
var ErrNoRefreshToken = errors.New("oauth2: no refresh token")

// ErrInvalidState is returned when the state parameter does not match.
var ErrInvalidState = errors.New("oauth2: invalid state")

// Provider names.
const (
	ProviderGoogle   = "google"
	ProviderGitHub   = "github"
	ProviderMicrosoft = "microsoft"
)

// Config holds provider-specific OAuth2 configuration.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	AuthURL      string
	TokenURL     string
}

// Token represents an OAuth2 token response.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scopes       []string  `json:"scopes,omitempty"`
}

// IsExpired reports whether the token has expired (with a 30-second buffer).
func (t *Token) IsExpired() bool {
	return time.Now().Add(30 * time.Second).After(t.ExpiresAt)
}

// tokenResponse is the raw JSON from the token endpoint.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// PKCEChallenge holds a PKCE verifier/challenge pair.
type PKCEChallenge struct {
	Verifier  string
	Challenge string
	Method    string
}

// GeneratePKCE creates a new PKCE verifier and S256 challenge.
func GeneratePKCE() (PKCEChallenge, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return PKCEChallenge{}, fmt.Errorf("oauth2: generate PKCE verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return PKCEChallenge{
		Verifier:  verifier,
		Challenge: challenge,
		Method:    "S256",
	}, nil
}

// StateStore manages short-lived OAuth2 state parameters.
type StateStore struct {
	mu     sync.Mutex
	states map[string]pkceEntry
}

type pkceEntry struct {
	pkce      PKCEChallenge
	expiresAt time.Time
}

// NewStateStore returns a StateStore.
func NewStateStore() *StateStore {
	return &StateStore{states: make(map[string]pkceEntry)}
}

// GenerateState creates a random state token, stores its PKCE entry, and returns the state.
func (s *StateStore) GenerateState(pkce PKCEChallenge) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth2: generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	s.states[state] = pkceEntry{pkce: pkce, expiresAt: time.Now().Add(10 * time.Minute)}
	s.mu.Unlock()
	return state, nil
}

// Consume retrieves and removes the PKCE entry for state, validating expiry.
func (s *StateStore) Consume(state string) (PKCEChallenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.states[state]
	if !ok {
		return PKCEChallenge{}, ErrInvalidState
	}
	delete(s.states, state)
	if time.Now().After(entry.expiresAt) {
		return PKCEChallenge{}, ErrInvalidState
	}
	return entry.pkce, nil
}

// HTTPClient is the interface used for token exchange and refresh calls.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Provider encapsulates one OAuth2 provider's config and token exchange logic.
type Provider struct {
	cfg    Config
	client HTTPClient
}

// NewProvider constructs a Provider with an optional custom HTTPClient (nil = http.DefaultClient).
func NewProvider(cfg Config, client HTTPClient) *Provider {
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{cfg: cfg, client: client}
}

// AuthURL builds the authorization URL including PKCE and state parameters.
func (p *Provider) AuthURL(state, codeChallenge, challengeMethod string) string {
	q := url.Values{}
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURL)
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("scope", strings.Join(p.cfg.Scopes, " "))
	if codeChallenge != "" {
		q.Set("code_challenge", codeChallenge)
		q.Set("code_challenge_method", challengeMethod)
	}
	return p.cfg.AuthURL + "?" + q.Encode()
}

// Exchange trades an authorization code for a Token.
func (p *Provider) Exchange(ctx context.Context, code, verifier string) (*Token, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", p.cfg.ClientID)
	data.Set("client_secret", p.cfg.ClientSecret)
	data.Set("redirect_uri", p.cfg.RedirectURL)
	data.Set("code", code)
	if verifier != "" {
		data.Set("code_verifier", verifier)
	}
	return p.doTokenRequest(ctx, data)
}

// Refresh exchanges a refresh token for a new access token.
func (p *Provider) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	if refreshToken == "" {
		return nil, ErrNoRefreshToken
	}
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", p.cfg.ClientID)
	data.Set("client_secret", p.cfg.ClientSecret)
	data.Set("refresh_token", refreshToken)
	return p.doTokenRequest(ctx, data)
}

func (p *Provider) doTokenRequest(ctx context.Context, data url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenURL,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth2: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth2: token request: %w", err)
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("oauth2: decode response: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("oauth2: provider error %s: %s", tr.Error, tr.ErrorDesc)
	}

	tok := &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}
	if tr.Scope != "" {
		tok.Scopes = strings.Split(tr.Scope, " ")
	}
	return tok, nil
}

// TokenStore is a thread-safe in-memory store for tokens keyed by userID+provider.
type TokenStore struct {
	mu     sync.RWMutex
	tokens map[string]*Token
}

// NewTokenStore returns an empty TokenStore.
func NewTokenStore() *TokenStore {
	return &TokenStore{tokens: make(map[string]*Token)}
}

func tokenKey(userID, provider string) string { return userID + ":" + provider }

// Set stores a token.
func (s *TokenStore) Set(userID, provider string, tok *Token) {
	s.mu.Lock()
	s.tokens[tokenKey(userID, provider)] = tok
	s.mu.Unlock()
}

// Get returns the token for userID+provider, or nil if absent.
func (s *TokenStore) Get(userID, provider string) *Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tokens[tokenKey(userID, provider)]
}

// Delete removes a token.
func (s *TokenStore) Delete(userID, provider string) {
	s.mu.Lock()
	delete(s.tokens, tokenKey(userID, provider))
	s.mu.Unlock()
}

// Manager orchestrates providers, token storage, and automatic refresh.
type Manager struct {
	providers map[string]*Provider
	store     *TokenStore
}

// NewManager returns a Manager with the given providers.
func NewManager(providers map[string]*Provider, store *TokenStore) *Manager {
	return &Manager{providers: providers, store: store}
}

// GetValidToken returns a valid token for userID+provider, refreshing if needed.
func (m *Manager) GetValidToken(ctx context.Context, userID, provider string) (*Token, error) {
	tok := m.store.Get(userID, provider)
	if tok == nil {
		return nil, ErrTokenExpired
	}
	if !tok.IsExpired() {
		return tok, nil
	}
	if tok.RefreshToken == "" {
		return nil, ErrNoRefreshToken
	}
	p, ok := m.providers[provider]
	if !ok {
		return nil, fmt.Errorf("oauth2: unknown provider %q", provider)
	}
	newTok, err := p.Refresh(ctx, tok.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("oauth2: refresh for %s/%s: %w", userID, provider, err)
	}
	// Preserve refresh token when provider omits it in response.
	if newTok.RefreshToken == "" {
		newTok.RefreshToken = tok.RefreshToken
	}
	m.store.Set(userID, provider, newTok)
	return newTok, nil
}
