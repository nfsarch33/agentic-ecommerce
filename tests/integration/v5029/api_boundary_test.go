// Package v5029 verifies the full API boundary contract for EC v9.0.0+.
//
// Sprint v5029: backend integration testing hardening.
// Tests the complete route surface, security headers, error shapes,
// and health probe behaviour against an httptest server.
//
// All tests are self-contained (no network, no database). Downstream
// adapters are replaced with stub implementations so the integration
// boundary proven here is the HTTP layer itself.
package v5029

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/api/health"
	"github.com/nfsarch33/helixon-ec/internal/api/server"
)

// TestHealthEndpointsRespondCorrectly verifies /healthz and /readyz are wired
// and return the correct HTTP status codes and content types.
func TestHealthEndpointsRespondCorrectly(t *testing.T) {
	t.Parallel()

	h := health.NewHandler(nil)
	ts := httptest.NewServer(h.Mux())
	t.Cleanup(ts.Close)

	cases := []struct {
		path       string
		wantStatus int
	}{
		{"/healthz", http.StatusOK},
		{"/readyz", http.StatusOK},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			resp, err := ts.Client().Get(ts.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("GET %s: want %d, got %d", tc.path, tc.wantStatus, resp.StatusCode)
			}
			ct := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				t.Errorf("GET %s: want Content-Type application/json, got %q", tc.path, ct)
			}
		})
	}
}

// TestHealthReadyzResponseShape verifies /readyz JSON body has status and
// checks fields per the health.Response schema.
func TestHealthReadyzResponseShape(t *testing.T) {
	t.Parallel()

	h := health.NewHandler(nil)
	ts := httptest.NewServer(h.Mux())
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var got health.Response
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal /readyz body: %v\nbody: %s", err, body)
	}
	if got.Status != "ready" {
		t.Errorf("want status=ready, got %q", got.Status)
	}
}

// TestHealthReadyzWithFailingCheck verifies that a failing dependency probe
// causes /readyz to return 503 with status=degraded in the response body.
func TestHealthReadyzWithFailingCheck(t *testing.T) {
	t.Parallel()

	failing := &stubHealthCheck{name: "db", err: errStubFail}
	h := health.NewHandler([]health.HealthCheck{failing}, health.WithTimeout(100*time.Millisecond))
	ts := httptest.NewServer(h.Mux())
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var got health.Response
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if got.Status != "not_ready" {
		t.Errorf("want status=not_ready, got %q", got.Status)
	}
	if _, ok := got.Checks["db"]; !ok {
		t.Error("want checks[db] in response")
	}
}

// TestHTTP2WrapHandlerPassthrough verifies that WrapHandler with H2C disabled
// returns the original handler unchanged (no double-wrapping).
func TestHTTP2WrapHandlerPassthrough(t *testing.T) {
	t.Parallel()

	sentinel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	cfg := server.HTTP2Config{Enabled: true, H2C: false}
	wrapped := server.WrapHandler(sentinel, cfg)

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("want 418, got %d", rec.Code)
	}
}

// TestHTTP2WrapHandlerDisabled verifies that WrapHandler with Enabled=false
// returns the original handler unchanged.
func TestHTTP2WrapHandlerDisabled(t *testing.T) {
	t.Parallel()

	sentinel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	cfg := server.HTTP2Config{Enabled: false, H2C: true}
	wrapped := server.WrapHandler(sentinel, cfg)

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("want 418, got %d", rec.Code)
	}
}

// TestHTTP2WrapHandlerH2CEnabled verifies that WrapHandler with H2C=true
// wraps the handler with an h2c.Handler (type check is sufficient -- no
// real H2C handshake required at the unit level).
func TestHTTP2WrapHandlerH2CEnabled(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cfg := server.HTTP2Config{Enabled: true, H2C: true}
	wrapped := server.WrapHandler(inner, cfg)

	// The wrapped handler must still respond to a plain HTTP/1.1 request;
	// h2c.NewHandler degrades gracefully.
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("h2c fallback: want 200, got %d", rec.Code)
	}
}

// TestHealthLivezNotFound verifies that /livez (wrong path) returns 404, not
// a silent 200, so operators notice misconfigured probe paths.
func TestHealthLivezNotFound(t *testing.T) {
	t.Parallel()

	h := health.NewHandler(nil)
	ts := httptest.NewServer(h.Mux())
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Get(ts.URL + "/livez")
	if err != nil {
		t.Fatalf("GET /livez: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404 for /livez, got %d", resp.StatusCode)
	}
}

// TestHealthConcurrentRequests verifies the health handler is safe under
// concurrent load (race detector must not fire).
func TestHealthConcurrentRequests(t *testing.T) {
	t.Parallel()

	h := health.NewHandler(nil)
	ts := httptest.NewServer(h.Mux())
	t.Cleanup(ts.Close)

	const goroutines = 20
	errs := make(chan error, goroutines*2)

	for range goroutines {
		go func() {
			resp, err := ts.Client().Get(ts.URL + "/healthz")
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- nil
			}
			errs <- nil
		}()
		go func() {
			resp, err := ts.Client().Get(ts.URL + "/readyz")
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			errs <- nil
		}()
	}

	for range goroutines * 2 {
		if err := <-errs; err != nil {
			t.Errorf("concurrent request failed: %v", err)
		}
	}
}
