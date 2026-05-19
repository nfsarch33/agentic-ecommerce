package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/api/middleware"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRateLimiter_UnderLimit_Passes(t *testing.T) {
	t.Parallel()
	rl := middleware.NewRateLimiter(5, time.Minute)
	handler := rl.Middleware(okHandler())

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:9999"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}
}

func TestRateLimiter_OverLimit_Returns429(t *testing.T) {
	t.Parallel()
	rl := middleware.NewRateLimiter(3, time.Minute)
	handler := rl.Middleware(okHandler())

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:9999"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:9999"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}

func TestRateLimiter_DifferentClientsIsolated(t *testing.T) {
	t.Parallel()
	rl := middleware.NewRateLimiter(2, time.Minute)
	handler := rl.Middleware(okHandler())

	// exhaust client A
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.1.1.1:1"
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	// client B should still be allowed
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "2.2.2.2:2"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for new client, got %d", rec.Code)
	}
}

func TestRateLimiter_WindowReset(t *testing.T) {
	t.Parallel()
	rl := middleware.NewRateLimiter(2, 50*time.Millisecond)
	handler := rl.Middleware(okHandler())

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:1"
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	time.Sleep(60 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after window reset, got %d", rec.Code)
	}
}

func TestRateLimiter_ConcurrentAccess_RaceFree(t *testing.T) {
	t.Parallel()
	rl := middleware.NewRateLimiter(100, time.Minute)
	handler := rl.Middleware(okHandler())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "1.2.3.4:1"
			handler.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	wg.Wait()
}

func TestRateLimiter_RetryAfterHeader(t *testing.T) {
	t.Parallel()
	rl := middleware.NewRateLimiter(1, time.Minute)
	handler := rl.Middleware(okHandler())

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "1.2.3.4:1"
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "1.2.3.4:1"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429 response")
	}
}
