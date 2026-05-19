//go:build v4171_smoke

package v4171

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mem0client "github.com/nfsarch33/helixon-ec/internal/adapter/mem0"
	"github.com/nfsarch33/helixon-ec/internal/metrics"
	"github.com/nfsarch33/helixon-ec/internal/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

type mockMem0 struct {
	mu      sync.Mutex
	entries map[string]string
	healthy bool
}

func newMockMem0() *mockMem0 {
	return &mockMem0{entries: make(map[string]string), healthy: true}
}

func (m *mockMem0) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	body, _ := io.ReadAll(r.Body)
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/memories/":
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		uid, _ := req["user_id"].(string)
		msgs, _ := req["messages"].([]any)
		if len(msgs) > 0 {
			msg, _ := msgs[0].(map[string]any)
			m.entries[uid], _ = msg["content"].(string)
		}
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodPost && r.URL.Path == "/v1/memories/search/":
		var results []mem0client.MemoryResult
		for k, v := range m.entries {
			results = append(results, mem0client.MemoryResult{
				ID: k, Memory: v, Score: 0.95,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(results)

	case r.Method == http.MethodDelete:
		key := r.URL.Path[len("/v1/memories/"):]
		key = key[:len(key)-1] // strip trailing slash
		delete(m.entries, key)
		w.WriteHeader(http.StatusOK)

	case r.URL.Path == "/health":
		w.WriteHeader(http.StatusOK)

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newBreaker(nowFn func() time.Time) *resilience.CircuitBreaker {
	return resilience.NewCircuitBreaker(testLogger(), resilience.CBConfig{
		Name:             "mem0-e2e",
		FailureThreshold: 3,
		SuccessThreshold: 1,
		CooldownDuration: 300 * time.Millisecond,
		NowFunc:          nowFn,
	})
}

func TestE2E_StoreSearchFound(t *testing.T) {
	t.Parallel()
	mock := newMockMem0()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	reg := metrics.NewRegistry("e2e")
	cb := newBreaker(time.Now)
	c := mem0client.NewClient(testLogger(), mem0client.Config{
		Endpoint:       srv.URL,
		TimeoutSeconds: 5,
		Enabled:        true,
	}, cb, reg)

	ctx := context.Background()
	err := c.Store(ctx, "user-42", "EC product memory", nil)
	require.NoError(t, err)

	results, err := c.Search(ctx, "product", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "EC product memory", results[0].Memory)
}

func TestE2E_StoreDeleteSearchNotFound(t *testing.T) {
	t.Parallel()
	mock := newMockMem0()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	cb := newBreaker(time.Now)
	c := mem0client.NewClient(testLogger(), mem0client.Config{
		Endpoint:       srv.URL,
		TimeoutSeconds: 5,
		Enabled:        true,
	}, cb, nil)

	ctx := context.Background()
	err := c.Store(ctx, "temp-key", "ephemeral data", nil)
	require.NoError(t, err)

	err = c.Delete(ctx, "temp-key")
	require.NoError(t, err)

	results, err := c.Search(ctx, "ephemeral", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestE2E_Mem0DownCircuitBreakerGracefulDegradation(t *testing.T) {
	t.Parallel()
	mock := newMockMem0()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	now := time.Now()
	cb := newBreaker(func() time.Time { return now })
	c := mem0client.NewClient(testLogger(), mem0client.Config{
		Endpoint:       srv.URL,
		TimeoutSeconds: 5,
		Enabled:        true,
	}, cb, nil)

	ctx := context.Background()

	mock.mu.Lock()
	mock.healthy = false
	mock.mu.Unlock()

	for i := 0; i < 3; i++ {
		_ = c.Store(ctx, "k", "v", nil)
	}
	assert.Equal(t, resilience.StateOpen, cb.State())

	results, err := c.Search(ctx, "anything", 5)
	assert.NoError(t, err, "search degrades to empty results")
	assert.Empty(t, results)

	err = c.Store(ctx, "k2", "v2", nil)
	assert.NoError(t, err, "store degrades to no-op")
}

func TestE2E_TimeoutCircuitBreakerRecovery(t *testing.T) {
	t.Parallel()
	var slowMode bool
	var mu sync.Mutex
	mock := newMockMem0()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		slow := slowMode
		mu.Unlock()
		if slow {
			time.Sleep(3 * time.Second)
		}
		mock.ServeHTTP(w, r)
	}))
	defer srv.Close()

	now := time.Now()
	nowMu := sync.Mutex{}
	getNow := func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}
	cb := newBreaker(getNow)
	c := mem0client.NewClient(testLogger(), mem0client.Config{
		Endpoint:       srv.URL,
		TimeoutSeconds: 1,
		Enabled:        true,
	}, cb, nil)

	ctx := context.Background()

	mu.Lock()
	slowMode = true
	mu.Unlock()

	for i := 0; i < 3; i++ {
		_ = c.Store(ctx, "k", "v", nil)
	}
	assert.Equal(t, resilience.StateOpen, cb.State())

	mu.Lock()
	slowMode = false
	mu.Unlock()

	nowMu.Lock()
	now = now.Add(500 * time.Millisecond)
	nowMu.Unlock()

	assert.Equal(t, resilience.StateHalfOpen, cb.State())

	err := c.Store(ctx, "recovery-key", "recovered", nil)
	require.NoError(t, err)
	assert.Equal(t, resilience.StateClosed, cb.State())

	results, err := c.Search(ctx, "recovered", 10)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	var found bool
	for _, r := range results {
		if r.Memory == "recovered" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected to find 'recovered' memory after circuit recovery")
}
