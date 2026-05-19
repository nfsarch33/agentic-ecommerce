package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/api/middleware"
)

func TestJWTValidator_ValidToken(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")
	validator := middleware.NewJWTValidator(secret)

	claims := middleware.Claims{
		UserID:    "u1",
		Email:     "alice@example.com",
		Roles:     []string{"admin"},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	token, err := validator.Sign(claims)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	parsed, err := validator.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if parsed.UserID != "u1" {
		t.Fatalf("UserID = %q, want u1", parsed.UserID)
	}
}

func TestJWTValidator_ExpiredToken(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")
	validator := middleware.NewJWTValidator(secret)

	claims := middleware.Claims{
		UserID:    "u1",
		Email:     "alice@example.com",
		Roles:     []string{"user"},
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	token, _ := validator.Sign(claims)
	_, err := validator.Validate(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestAuthMiddleware_MissingToken_Returns401(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")
	auth := middleware.NewAuthMiddleware(secret)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	auth.Authenticate(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_ValidToken_Passes(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")
	auth := middleware.NewAuthMiddleware(secret)
	validator := middleware.NewJWTValidator(secret)

	claims := middleware.Claims{
		UserID:    "u1",
		Email:     "alice@example.com",
		Roles:     []string{"user"},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	token, _ := validator.Sign(claims)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	auth.Authenticate(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRBACEnforcer_InsufficientRole_Returns403(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")
	auth := middleware.NewAuthMiddleware(secret)
	validator := middleware.NewJWTValidator(secret)

	claims := middleware.Claims{
		UserID:    "u1",
		Email:     "alice@example.com",
		Roles:     []string{"user"},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	token, _ := validator.Sign(claims)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler := auth.Authenticate(auth.Require("admin")(okHandler()))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRBACEnforcer_HasRole_Passes(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32-bytes-long!!!")
	auth := middleware.NewAuthMiddleware(secret)
	validator := middleware.NewJWTValidator(secret)

	claims := middleware.Claims{
		UserID:    "u2",
		Email:     "bob@example.com",
		Roles:     []string{"user", "admin"},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	token, _ := validator.Sign(claims)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler := auth.Authenticate(auth.Require("admin")(okHandler()))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestJWTValidator_InvalidSignature(t *testing.T) {
	t.Parallel()
	secret1 := []byte("test-secret-key-32-bytes-long!!!")
	secret2 := []byte("different-secret-key-32-bytes!!!!")
	v1 := middleware.NewJWTValidator(secret1)
	v2 := middleware.NewJWTValidator(secret2)

	claims := middleware.Claims{UserID: "u1", Email: "x@x.com", ExpiresAt: time.Now().Add(time.Hour)}
	token, _ := v1.Sign(claims)
	_, err := v2.Validate(token)
	if err == nil {
		t.Fatal("expected signature mismatch error")
	}
}
