package captcha

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type denyAuth struct{}

func (denyAuth) Authenticate(_ *http.Request) (string, error) {
	return "", errors.New("nope")
}

func TestExtractEventID_Roundtrip(t *testing.T) {
	t.Parallel()
	id, ok := extractEventID("/api/v1/uiauto/captcha/abc-123/resolved")
	if !ok || id != "abc-123" {
		t.Fatalf("want abc-123, got %q ok=%v", id, ok)
	}
	if _, ok := extractEventID("/api/v1/uiauto/captcha//resolved"); ok {
		t.Fatalf("empty id should fail")
	}
	if _, ok := extractEventID("/some/other/path"); ok {
		t.Fatalf("non-prefix should fail")
	}
	if _, ok := extractEventID("/api/v1/uiauto/captcha/abc-123/other"); ok {
		t.Fatalf("missing /resolved suffix should fail")
	}
}

func TestHandler_RequiresPOST(t *testing.T) {
	t.Parallel()
	h := mustHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/uiauto/captcha/x/resolved", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestHandler_RequiresAuth(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, time.Hour)
	defer d.Close(context.Background())
	h, err := NewHandler(HandlerConfig{Detector: d, Authenticator: denyAuth{}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uiauto/captcha/x/resolved", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestHandler_ResolvesPending(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, time.Hour)
	defer d.Close(context.Background())
	id := d.PausePipeline("tenant-a", "tiktok")
	h, err := NewHandler(HandlerConfig{Detector: d, Authenticator: AllowAllAuthenticator{}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uiauto/captcha/"+id+"/resolved", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "resolved") {
		t.Fatalf("response missing resolved key: %s", rec.Body.String())
	}
}

func TestHandler_UnknownEventReturns404(t *testing.T) {
	t.Parallel()
	h := mustHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uiauto/captcha/missing/resolved", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestHandler_BadPathReturns400(t *testing.T) {
	t.Parallel()
	h := mustHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uiauto/captcha//resolved", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestNewHandler_RejectsNilDeps(t *testing.T) {
	t.Parallel()
	if _, err := NewHandler(HandlerConfig{}); err == nil {
		t.Fatalf("want err, got nil")
	}
	d := New(Config{})
	if _, err := NewHandler(HandlerConfig{Detector: d}); err == nil {
		t.Fatalf("want err on nil authenticator")
	}
}

func mustHandler(t *testing.T) *Handler {
	t.Helper()
	d := newDet(t, nil, nil, time.Hour)
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	h, err := NewHandler(HandlerConfig{Detector: d, Authenticator: AllowAllAuthenticator{}})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return h
}
