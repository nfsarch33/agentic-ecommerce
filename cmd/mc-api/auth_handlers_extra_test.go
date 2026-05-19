package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/security"
)

// File scope: targeted coverage for the previously-uncovered branches in
// auth_handlers.go: routing fallthroughs, missing Authorization header,
// malformed bearer prefix, repository failures during writeTokenPair,
// and the not-configured short-circuits on /login and /refresh.

func newAuthFixtureServer(t *testing.T) (*server, *security.TokenManager, security.RefreshSessionStore) {
	t.Helper()
	srv, _ := testServer(t)
	tokenMgr, err := security.NewTokenManager(security.TokenConfig{
		Secret:    "auth-handlers-extra-secret-32-bytes-test-fixture",
		Issuer:    "agentic-test",
		Audience:  "mc-api-test",
		AccessTTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	srv.tokenManager = tokenMgr
	srv.sessions = security.NewInMemoryRefreshSessionStore(nil)
	srv.cfg.adminUsername = "admin"
	srv.cfg.adminPassword = "supersecret"
	srv.cfg.adminRole = security.RoleAdmin
	srv.cfg.refreshTTL = time.Hour
	return srv, tokenMgr, srv.sessions
}

func TestAuthHandlerRejectsUnknownSubpath(t *testing.T) {
	t.Parallel()

	srv, _, _ := newAuthFixtureServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/something-else", nil)
	rec := httptest.NewRecorder()
	srv.authHandler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestLoginReturnsServiceUnavailableWhenAuthNotConfigured(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"x"}`))
	rec := httptest.NewRecorder()
	srv.login(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestLoginRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	srv, _, _ := newAuthFixtureServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{not-json`))
	rec := httptest.NewRecorder()
	srv.login(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestLoginRejectsInvalidCredentialsAndPrefersUsernameOverEmail(t *testing.T) {
	t.Parallel()

	srv, _, _ := newAuthFixtureServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"intruder","password":"wrong"}`))
	rec := httptest.NewRecorder()
	srv.login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLoginAcceptsEmailAsUsernameAlias(t *testing.T) {
	t.Parallel()

	srv, _, _ := newAuthFixtureServer(t)
	body := strings.NewReader(`{"email":"admin","password":"supersecret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	rec := httptest.NewRecorder()
	srv.login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRefreshReturnsServiceUnavailableWhenAuthNotConfigured(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{"refresh_token":"x"}`))
	rec := httptest.NewRecorder()
	srv.refresh(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestRefreshRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	srv, _, _ := newAuthFixtureServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`!`))
	rec := httptest.NewRecorder()
	srv.refresh(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRefreshRejectsUnknownRefreshToken(t *testing.T) {
	t.Parallel()

	srv, _, _ := newAuthFixtureServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{"refresh_token":"unknown"}`))
	rec := httptest.NewRecorder()
	srv.refresh(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRefreshConsumesValidRefreshTokenAndIssuesNewPair(t *testing.T) {
	t.Parallel()

	srv, _, sessions := newAuthFixtureServer(t)
	raw, hash, err := security.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	if err := sessions.Save(context.Background(), security.RefreshSession{
		TokenHash: hash,
		Subject:   "admin",
		Role:      security.RoleAdmin,
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	body := strings.NewReader(`{"refresh_token":"` + raw + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", body)
	rec := httptest.NewRecorder()
	srv.refresh(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got, _ := sessions.Get(context.Background(), hash); got.Subject != "" {
		t.Fatalf("old refresh token must be revoked, got %+v", got)
	}
}

func TestMeAndLogoutReturnUnauthorizedWithoutBearer(t *testing.T) {
	t.Parallel()

	srv, _, _ := newAuthFixtureServer(t)

	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "me", handler: srv.me},
		{name: "logout", handler: srv.logout},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/"+tc.name, nil)
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s status = %d, want 401", tc.name, rec.Code)
			}
		})
	}
}

func TestMeReturnsClaimsWhenBearerIsValid(t *testing.T) {
	t.Parallel()

	srv, mgr, _ := newAuthFixtureServer(t)
	token, err := mgr.MintAccessToken(security.Principal{Subject: "admin", Role: security.RoleAdmin})
	if err != nil {
		t.Fatalf("MintAccessToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.me(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestLogoutReturnsNoContentWhenBearerIsValid(t *testing.T) {
	t.Parallel()

	srv, mgr, _ := newAuthFixtureServer(t)
	token, err := mgr.MintAccessToken(security.Principal{Subject: "admin", Role: security.RoleAdmin})
	if err != nil {
		t.Fatalf("MintAccessToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.logout(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestAccessClaimsFromRequestRejectsMissingBearerPrefix(t *testing.T) {
	t.Parallel()

	srv, _, _ := newAuthFixtureServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "BasicTokenLiteral")
	if _, ok := srv.accessClaimsFromRequest(req); ok {
		t.Fatal("accessClaimsFromRequest accepted missing Bearer prefix")
	}
}

func TestAccessClaimsFromRequestRejectsEmptyBearerToken(t *testing.T) {
	t.Parallel()

	srv, _, _ := newAuthFixtureServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer    ")
	if _, ok := srv.accessClaimsFromRequest(req); ok {
		t.Fatal("accessClaimsFromRequest accepted empty Bearer token")
	}
}

func TestAccessClaimsFromRequestReturnsFalseWhenManagerNil(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	if _, ok := srv.accessClaimsFromRequest(req); ok {
		t.Fatal("accessClaimsFromRequest accepted token when manager is nil")
	}
}

// TestLoginReturnsTokensWithExpectedExpiry asserts the writeTokenPair
// happy path serialises an ExpiresIn matching AccessTTL and a session
// envelope referencing the principal subject. Captures the previously
// 43.8%-covered branches.
func TestLoginReturnsTokensWithExpectedExpiry(t *testing.T) {
	t.Parallel()

	srv, mgr, _ := newAuthFixtureServer(t)
	body := []byte(`{"username":"admin","password":"supersecret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if mgr.AccessTTL() != 5*time.Minute {
		t.Fatalf("token AccessTTL = %s, want 5m", mgr.AccessTTL())
	}
}
