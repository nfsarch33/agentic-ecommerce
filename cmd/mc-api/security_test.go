package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/security"
)

func secureTestServer(t *testing.T, logs *bytes.Buffer) *server {
	t.Helper()
	t.Setenv("ECOMMERCE_JWT_SECRET", "test-secret-at-least-32-bytes-long")
	t.Setenv("ECOMMERCE_ADMIN_USERNAME", "admin@example.com")
	t.Setenv("ECOMMERCE_ADMIN_PASSWORD", "correct-horse-battery-staple")
	t.Setenv("ECOMMERCE_RATE_LIMIT_CAPACITY", "100")
	t.Setenv("ECOMMERCE_RATE_LIMIT_REFILL", "1s")
	t.Setenv("ECOMMERCE_REDIS_ADDR", "")
	if logs == nil {
		logs = &bytes.Buffer{}
	}
	return newServer(
		slog.New(slog.NewJSONHandler(logs, nil)),
		inmemory.NewProductRepository(),
		inmemory.NewOrderRepository(),
		inmemory.NewCartRepository(),
	)
}

func TestLoginMintsJWTAndRefreshToken(t *testing.T) {
	srv := secureTestServer(t, nil)
	defer srv.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin@example.com","password":"correct-horse-battery-staple"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if got.AccessToken == "" || got.RefreshToken == "" || got.TokenType != "Bearer" {
		t.Fatalf("login response missing tokens: %+v", got)
	}
	if got.Role != string(security.RoleAdmin) {
		t.Fatalf("role = %q, want admin", got.Role)
	}
	if _, err := srv.tokenManager.VerifyAccessToken(got.AccessToken); err != nil {
		t.Fatalf("minted access token did not verify: %v", err)
	}
}

func TestRefreshRotatesRefreshToken(t *testing.T) {
	srv := secureTestServer(t, nil)
	defer srv.Close()

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin@example.com","password":"correct-horse-battery-staple"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body=%s", loginRec.Code, loginRec.Body.String())
	}
	var login loginResponse
	if err := json.NewDecoder(loginRec.Body).Decode(&login); err != nil {
		t.Fatalf("decode login: %v", err)
	}

	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{"refresh_token":"`+login.RefreshToken+`"}`))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(refreshRec, refreshReq)
	if refreshRec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200; body=%s", refreshRec.Code, refreshRec.Body.String())
	}
	var refreshed loginResponse
	if err := json.NewDecoder(refreshRec.Body).Decode(&refreshed); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	if refreshed.RefreshToken == "" || refreshed.RefreshToken == login.RefreshToken {
		t.Fatalf("refresh token was not rotated: old=%q new=%q", login.RefreshToken, refreshed.RefreshToken)
	}

	reuseReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{"refresh_token":"`+login.RefreshToken+`"}`))
	reuseRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(reuseRec, reuseReq)
	if reuseRec.Code != http.StatusUnauthorized {
		t.Fatalf("reused refresh status = %d, want 401", reuseRec.Code)
	}
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	srv := secureTestServer(t, nil)
	defer srv.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin@example.com","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRBACAllowsOperatorAndDeniesViewerForProductMutation(t *testing.T) {
	srv := secureTestServer(t, nil)
	defer srv.Close()

	viewerToken := mintTestAccessToken(t, srv, "viewer@example.com", security.RoleViewer)
	operatorToken := mintTestAccessToken(t, srv, "operator@example.com", security.RoleOperator)

	body := `{"sku":"BAND-SEC","title":"Security Band","price":{"amount":4995,"currency":"AUD"},"stock":12}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/products", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+operatorToken)
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("operator status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPublicEndpointsRemainUnauthenticated(t *testing.T) {
	srv := secureTestServer(t, nil)
	defer srv.Close()

	for _, path := range []string{"/healthz", "/readyz", "/metrics", "/api/v1/products"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Fatalf("%s status = %d, want public access", path, rec.Code)
		}
	}
}

func TestRateLimitReturnsTooManyRequests(t *testing.T) {
	srv := secureTestServer(t, nil)
	defer srv.Close()
	srv.rateLimiter = security.NewInMemoryTokenBucket(security.TokenBucketConfig{
		Capacity:       1,
		RefillInterval: time.Hour,
		Now:            func() time.Time { return time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC) },
	})

	for i, want := range []int{http.StatusOK, http.StatusTooManyRequests} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
		req.RemoteAddr = "203.0.113.10:4000"
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("request %d status = %d, want %d; body=%s", i+1, rec.Code, want, rec.Body.String())
		}
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	srv := secureTestServer(t, nil)
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	for key, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "DENY",
	} {
		if got := rec.Header().Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("Content-Security-Policy = %q, want frame-ancestors", got)
	}
}

func TestAuditLogRecordsMutationWithoutSecrets(t *testing.T) {
	var logs bytes.Buffer
	srv := secureTestServer(t, &logs)
	defer srv.Close()
	operatorToken := mintTestAccessToken(t, srv, "operator@example.com", security.RoleOperator)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", strings.NewReader(`{
		"sku":"AUDIT-001",
		"title":"Audit Product",
		"price":{"amount":1999,"currency":"AUD"},
		"stock":4,
		"password":"should-not-appear",
		"token":"also-secret"
	}`))
	req.Header.Set("Authorization", "Bearer "+operatorToken)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	logLine := logs.String()
	for _, want := range []string{
		`"msg":"audit.event"`,
		`"actor":"operator@example.com"`,
		`"role":"operator"`,
		`"action":"product.create"`,
		`"status":201`,
	} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("audit log missing %q:\n%s", want, logLine)
		}
	}
	for _, forbidden := range []string{"should-not-appear", "also-secret", "test-secret-at-least-32-bytes-long"} {
		if strings.Contains(logLine, forbidden) {
			t.Fatalf("audit log leaked %q:\n%s", forbidden, logLine)
		}
	}
}

func mintTestAccessToken(t *testing.T, srv *server, subject string, role security.Role) string {
	t.Helper()
	token, err := srv.tokenManager.MintAccessToken(security.Principal{Subject: subject, Role: role})
	if err != nil {
		t.Fatalf("mint test token: %v", err)
	}
	return token
}
