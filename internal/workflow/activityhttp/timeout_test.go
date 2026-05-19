package activityhttp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/workflow/activityhttp"
)

func TestTimeoutDefaultIs30s(t *testing.T) {
	t.Setenv("EC_ACTIVITY_HTTP_TIMEOUT_SECONDS", "")
	got := activityhttp.Timeout()
	if got != 30*time.Second {
		t.Fatalf("default timeout = %v, want 30s", got)
	}
}

func TestTimeoutFromEnv(t *testing.T) {
	t.Setenv("EC_ACTIVITY_HTTP_TIMEOUT_SECONDS", "10")
	got := activityhttp.Timeout()
	if got != 10*time.Second {
		t.Fatalf("env timeout = %v, want 10s", got)
	}
}

func TestTimeoutInvalidEnvFallsBack(t *testing.T) {
	t.Setenv("EC_ACTIVITY_HTTP_TIMEOUT_SECONDS", "notanumber")
	got := activityhttp.Timeout()
	if got != 30*time.Second {
		t.Fatalf("invalid env timeout = %v, want 30s fallback", got)
	}
}

func TestWithTimeoutCancelsSlowHTTPCall(t *testing.T) {
	t.Setenv("EC_ACTIVITY_HTTP_TIMEOUT_SECONDS", "1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := activityhttp.WithTimeout(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	start := time.Now()
	_, err = http.DefaultClient.Do(req)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context deadline exceeded error, got nil")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("request took %v; timeout should have fired within ~1s", elapsed)
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("ctx.Err() = %v, want DeadlineExceeded", ctx.Err())
	}
}

func TestWithTimeoutAllowsFastHTTPCall(t *testing.T) {
	t.Setenv("EC_ACTIVITY_HTTP_TIMEOUT_SECONDS", "5")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	ctx, cancel := activityhttp.WithTimeout(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
