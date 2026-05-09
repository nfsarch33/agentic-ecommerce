package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestMemCapAllowsBelowLimit(t *testing.T) {
	t.Parallel()
	handler := MemCap(MemCapConfig{MaxRequestBytes: 1024})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	body := bytes.NewBufferString(`{"hello":"world"}`)
	req := httptest.NewRequest(http.MethodPost, "/echo", body)
	req.ContentLength = int64(body.Len())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code=%d, want 200", rec.Code)
	}
}

func TestMemCapRejectsContentLengthOver(t *testing.T) {
	t.Parallel()
	handler := MemCap(MemCapConfig{MaxRequestBytes: 16})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(`12345678901234567890`))
	req.ContentLength = 20
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("Code=%d, want 413", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type=%q, want application/json", got)
	}
}

func TestMemCapZeroDisables(t *testing.T) {
	t.Parallel()
	handler := MemCap(MemCapConfig{MaxRequestBytes: 0})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("anything"))
	req.ContentLength = 1 << 30
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code=%d, want 200 (cap disabled)", rec.Code)
	}
}

func TestMemCapTenantOverride(t *testing.T) {
	t.Parallel()
	handler := MemCap(MemCapConfig{
		MaxRequestBytes: 10,
		TenantOverride: func(_ *http.Request) (int64, bool) {
			return 1 << 16, true
		},
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("0123456789ABCDEF"))
	req.ContentLength = 16
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code=%d, want 200 (override raised cap)", rec.Code)
	}
}

func TestMemCapBodyReaderEnforcesEvenWithoutContentLength(t *testing.T) {
	t.Parallel()
	handler := MemCap(MemCapConfig{MaxRequestBytes: 8})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 32)
		n, _ := r.Body.Read(buf)
		// best-effort: write what we read
		w.Header().Set("X-Read", strconv.Itoa(n))
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString("0123456789ABCDEF"))
	req.ContentLength = -1 // unknown body length
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Read"); got != "8" && got != "9" {
		t.Fatalf("X-Read=%q, want 8 or 9 (cap enforced via MaxBytesReader)", got)
	}
}
