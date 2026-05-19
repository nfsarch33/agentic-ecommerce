package httpclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/httpclient"
	"github.com/nfsarch33/helixon-ec/internal/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestNew_RequiresBaseURL(t *testing.T) {
	t.Parallel()
	_, err := httpclient.New(httpclient.Config{BaseURL: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base URL required")
}

func TestDo_RespectTimeout(t *testing.T) {
	t.Parallel()
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	})
	c, err := httpclient.New(httpclient.Config{
		BaseURL: srv.URL,
		Timeout: 50 * time.Millisecond,
	})
	require.NoError(t, err)
	_, _, err = c.Do(context.Background(), http.MethodGet, "/slow", nil)
	require.Error(t, err)
}

func TestDo_RetriesOnServerError(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	c, err := httpclient.New(httpclient.Config{
		BaseURL:    srv.URL,
		MaxRetries: 3,
		RetryDelay: 10 * time.Millisecond,
	})
	require.NoError(t, err)
	body, status, err := c.Do(context.Background(), http.MethodGet, "/retry", nil)
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, string(body), "ok")
	assert.Equal(t, int32(3), calls.Load())
}

func TestDo_CircuitBreakerDelegation(t *testing.T) {
	t.Parallel()
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	cb := resilience.NewCircuitBreaker(nil, resilience.CBConfig{
		Name:             "test-cb",
		FailureThreshold: 5,
		CooldownDuration: time.Minute,
	})
	c, err := httpclient.New(httpclient.Config{
		BaseURL: srv.URL,
		Breaker: cb,
	})
	require.NoError(t, err)
	body, status, err := c.Do(context.Background(), http.MethodGet, "/health", nil)
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, string(body), "ok")
}

func TestPostJSON_Success(t *testing.T) {
	t.Parallel()
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "123"})
	})
	c, err := httpclient.New(httpclient.Config{
		BaseURL:      srv.URL,
		RequestHooks: []httpclient.RequestHook{httpclient.JSONRequestHook()},
	})
	require.NoError(t, err)
	body, status, err := c.PostJSON(context.Background(), "/items", map[string]string{"name": "test"})
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Contains(t, string(body), "123")
}

func TestRequestHook_BearerAuth(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	})
	c, err := httpclient.New(httpclient.Config{
		BaseURL: srv.URL,
		RequestHooks: []httpclient.RequestHook{
			httpclient.BearerAuthHook(func() string { return "tok_abc" }),
		},
	})
	require.NoError(t, err)
	_, _, err = c.Do(context.Background(), http.MethodGet, "/secure", nil)
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok_abc", gotAuth)
}

func TestResponseHook_Called(t *testing.T) {
	t.Parallel()
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-Id", "rid-999")
		w.WriteHeader(200)
	})
	var gotRID string
	c, err := httpclient.New(httpclient.Config{
		BaseURL: srv.URL,
		ResponseHooks: []httpclient.ResponseHook{
			func(resp *http.Response) error {
				gotRID = resp.Header.Get("X-Request-Id")
				return nil
			},
		},
	})
	require.NoError(t, err)
	_, _, err = c.Do(context.Background(), http.MethodGet, "/tracked", nil)
	require.NoError(t, err)
	assert.Equal(t, "rid-999", gotRID)
}

func TestGetJSON_Success(t *testing.T) {
	t.Parallel()
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"key":"value"}`))
	})
	c, err := httpclient.New(httpclient.Config{BaseURL: srv.URL})
	require.NoError(t, err)
	body, status, err := c.GetJSON(context.Background(), "/data")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, string(body), "value")
}
