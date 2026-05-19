package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/api/middleware"
)

func versionHandler(v string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Api-Version", v)
		w.WriteHeader(http.StatusOK)
	})
}

func TestVersionRouter_RouteByHeader(t *testing.T) {
	t.Parallel()
	router := middleware.NewVersionRouter()
	router.Register("v1", versionHandler("v1"))
	router.Register("v2", versionHandler("v2"))

	req := httptest.NewRequest(http.MethodGet, "/products", nil)
	req.Header.Set("Accept", "application/vnd.ec.v2+json")
	rec := httptest.NewRecorder()
	router.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Api-Version") != "v2" {
		t.Fatalf("expected v2, got %s", rec.Header().Get("X-Api-Version"))
	}
}

func TestVersionRouter_FallbackToLatest(t *testing.T) {
	t.Parallel()
	router := middleware.NewVersionRouter()
	router.Register("v1", versionHandler("v1"))
	router.Register("v2", versionHandler("v2"))

	req := httptest.NewRequest(http.MethodGet, "/products", nil)
	// no Accept header -- should fallback to latest
	rec := httptest.NewRecorder()
	router.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Api-Version") != "v2" {
		t.Fatalf("expected latest v2, got %s", rec.Header().Get("X-Api-Version"))
	}
}

func TestVersionRouter_UnknownVersion_404(t *testing.T) {
	t.Parallel()
	router := middleware.NewVersionRouter()
	router.Register("v1", versionHandler("v1"))

	req := httptest.NewRequest(http.MethodGet, "/products", nil)
	req.Header.Set("Accept", "application/vnd.ec.v9+json")
	rec := httptest.NewRecorder()
	router.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown version, got %d", rec.Code)
	}
}

func TestVersionRouter_DeprecationWarning(t *testing.T) {
	t.Parallel()
	router := middleware.NewVersionRouter()
	router.Register("v1", versionHandler("v1"))
	router.Register("v2", versionHandler("v2"))
	router.Deprecate("v1")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "application/vnd.ec.v1+json")
	rec := httptest.NewRecorder()
	router.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("deprecated version should still work, got %d", rec.Code)
	}
	if rec.Header().Get("Deprecation") == "" {
		t.Fatal("expected Deprecation header on deprecated version")
	}
}

func TestVersionRouter_URLPathVersion(t *testing.T) {
	t.Parallel()
	router := middleware.NewVersionRouter()
	router.Register("v1", versionHandler("v1"))
	router.Register("v2", versionHandler("v2"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	router.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Api-Version") != "v1" {
		t.Fatalf("expected v1 from URL, got %s", rec.Header().Get("X-Api-Version"))
	}
}

func TestVersionRouter_Empty_Returns404(t *testing.T) {
	t.Parallel()
	router := middleware.NewVersionRouter()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty router should return 404, got %d", rec.Code)
	}
}
