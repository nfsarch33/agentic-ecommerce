package oauth2_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/oauth2"
)

// --- PKCE tests ---

func TestGeneratePKCE_UniqueVerifiers(t *testing.T) {
	t.Parallel()
	p1, err := oauth2.GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	p2, err := oauth2.GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if p1.Verifier == p2.Verifier {
		t.Fatal("PKCE verifiers must be unique")
	}
}

func TestGeneratePKCE_S256Method(t *testing.T) {
	t.Parallel()
	p, err := oauth2.GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if p.Method != "S256" {
		t.Fatalf("want S256, got %q", p.Method)
	}
	if p.Challenge == p.Verifier {
		t.Fatal("challenge must differ from verifier")
	}
}

// --- StateStore tests ---

func TestStateStore_ConsumeValid(t *testing.T) {
	t.Parallel()
	store := oauth2.NewStateStore()
	pkce, _ := oauth2.GeneratePKCE()
	state, err := store.GenerateState(pkce)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Consume(state)
	if err != nil {
		t.Fatalf("consume valid state: %v", err)
	}
	if got.Verifier != pkce.Verifier {
		t.Fatal("PKCE verifier mismatch")
	}
}

func TestStateStore_ConsumeOnce(t *testing.T) {
	t.Parallel()
	store := oauth2.NewStateStore()
	pkce, _ := oauth2.GeneratePKCE()
	state, _ := store.GenerateState(pkce)
	_, _ = store.Consume(state)
	_, err := store.Consume(state)
	if err == nil {
		t.Fatal("second consume must fail")
	}
}

func TestStateStore_ConsumeUnknown(t *testing.T) {
	t.Parallel()
	store := oauth2.NewStateStore()
	_, err := store.Consume("bogus")
	if err == nil {
		t.Fatal("unknown state must return error")
	}
}

// --- Provider tests ---

func makeTokenServer(t *testing.T, resp interface{}, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestProvider_AuthURL_ContainsPKCE(t *testing.T) {
	t.Parallel()
	p := oauth2.NewProvider(oauth2.Config{
		ClientID:    "cid",
		RedirectURL: "http://localhost/cb",
		Scopes:      []string{"openid"},
		AuthURL:     "https://example.com/auth",
		TokenURL:    "https://example.com/token",
	}, nil)
	u := p.AuthURL("st", "challenge", "S256")
	if !strings.Contains(u, "code_challenge=challenge") {
		t.Fatalf("auth URL missing PKCE challenge: %s", u)
	}
	if !strings.Contains(u, "state=st") {
		t.Fatalf("auth URL missing state: %s", u)
	}
}

func TestProvider_Exchange_Success(t *testing.T) {
	t.Parallel()
	srv := makeTokenServer(t, map[string]interface{}{
		"access_token":  "at",
		"refresh_token": "rt",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid email",
	}, http.StatusOK)
	defer srv.Close()

	p := oauth2.NewProvider(oauth2.Config{
		ClientID:     "cid",
		ClientSecret: "sec",
		TokenURL:     srv.URL,
	}, srv.Client())

	tok, err := p.Exchange(context.Background(), "code123", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "at" {
		t.Fatalf("want at, got %q", tok.AccessToken)
	}
	if len(tok.Scopes) != 2 {
		t.Fatalf("want 2 scopes, got %v", tok.Scopes)
	}
}

func TestProvider_Exchange_ProviderError(t *testing.T) {
	t.Parallel()
	srv := makeTokenServer(t, map[string]interface{}{
		"error":             "invalid_grant",
		"error_description": "code expired",
	}, http.StatusBadRequest)
	defer srv.Close()

	p := oauth2.NewProvider(oauth2.Config{TokenURL: srv.URL}, srv.Client())
	_, err := p.Exchange(context.Background(), "bad", "")
	if err == nil {
		t.Fatal("expected error from provider error response")
	}
}

func TestProvider_Refresh_NoToken(t *testing.T) {
	t.Parallel()
	p := oauth2.NewProvider(oauth2.Config{TokenURL: "http://irrelevant"}, nil)
	_, err := p.Refresh(context.Background(), "")
	if err == nil {
		t.Fatal("expected ErrNoRefreshToken")
	}
}

// --- TokenStore tests ---

func TestTokenStore_SetGetDelete(t *testing.T) {
	t.Parallel()
	s := oauth2.NewTokenStore()
	tok := &oauth2.Token{AccessToken: "acc", ExpiresAt: time.Now().Add(time.Hour)}
	s.Set("u1", "google", tok)
	got := s.Get("u1", "google")
	if got == nil || got.AccessToken != "acc" {
		t.Fatal("expected stored token")
	}
	s.Delete("u1", "google")
	if s.Get("u1", "google") != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestToken_IsExpired(t *testing.T) {
	t.Parallel()
	expired := &oauth2.Token{ExpiresAt: time.Now().Add(-time.Hour)}
	if !expired.IsExpired() {
		t.Fatal("token should be expired")
	}
	fresh := &oauth2.Token{ExpiresAt: time.Now().Add(time.Hour)}
	if fresh.IsExpired() {
		t.Fatal("token should not be expired")
	}
}

// --- Manager tests ---

func TestManager_GetValidToken_RefreshesExpired(t *testing.T) {
	t.Parallel()
	srv := makeTokenServer(t, map[string]interface{}{
		"access_token":  "new-at",
		"token_type":    "Bearer",
		"expires_in":    3600,
	}, http.StatusOK)
	defer srv.Close()

	provider := oauth2.NewProvider(oauth2.Config{TokenURL: srv.URL}, srv.Client())
	store := oauth2.NewTokenStore()
	mgr := oauth2.NewManager(map[string]*oauth2.Provider{"google": provider}, store)

	// Store an expired token with a refresh token.
	store.Set("u1", "google", &oauth2.Token{
		AccessToken:  "old-at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})

	tok, err := mgr.GetValidToken(context.Background(), "u1", "google")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "new-at" {
		t.Fatalf("want new-at, got %q", tok.AccessToken)
	}
	// Refresh token must be preserved when omitted by server.
	if tok.RefreshToken != "rt" {
		t.Fatalf("refresh token should be preserved, got %q", tok.RefreshToken)
	}
}

func TestManager_GetValidToken_NoToken(t *testing.T) {
	t.Parallel()
	store := oauth2.NewTokenStore()
	mgr := oauth2.NewManager(nil, store)
	_, err := mgr.GetValidToken(context.Background(), "unknown", "google")
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
}
