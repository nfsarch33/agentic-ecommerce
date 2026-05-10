package mem0

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func newTestBreaker(nowFn func() time.Time) *resilience.CircuitBreaker {
	return resilience.NewCircuitBreaker(testLogger(), resilience.CBConfig{
		Name:             "mem0-test",
		FailureThreshold: 3,
		SuccessThreshold: 1,
		CooldownDuration: 200 * time.Millisecond,
		NowFunc:          nowFn,
	})
}

func TestStoreAndSearchRoundTrip(t *testing.T) {
	t.Parallel()
	stored := make(map[string]string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/memories/":
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			uid, _ := req["user_id"].(string)
			msgs, _ := req["messages"].([]any)
			if len(msgs) > 0 {
				m, _ := msgs[0].(map[string]any)
				stored[uid], _ = m["content"].(string)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/memories/search/":
			var results []MemoryResult
			for k, v := range stored {
				results = append(results, MemoryResult{ID: k, Memory: v, Score: 0.9})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(results)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	reg := metrics.NewRegistry("test")
	cb := newTestBreaker(time.Now)
	c := NewClient(testLogger(), Config{
		Endpoint:       srv.URL,
		TimeoutSeconds: 5,
		Enabled:        true,
	}, cb, reg)

	ctx := context.Background()
	err := c.Store(ctx, "user1", "hello world", map[string]string{"source": "test"})
	require.NoError(t, err)

	results, err := c.Search(ctx, "hello", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "hello world", results[0].Memory)
}

func TestCircuitBreakerOpensOnFailures(t *testing.T) {
	t.Parallel()
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	now := time.Now()
	cb := newTestBreaker(func() time.Time { return now })
	c := NewClient(testLogger(), Config{
		Endpoint:       srv.URL,
		TimeoutSeconds: 5,
		Enabled:        true,
	}, cb, nil)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = c.Store(ctx, "k", "v", nil)
	}
	assert.Equal(t, resilience.StateOpen, cb.State())

	results, err := c.Search(ctx, "anything", 5)
	require.NoError(t, err, "search should degrade gracefully when circuit open")
	assert.Empty(t, results)
	assert.Equal(t, int32(3), callCount.Load(), "no more HTTP calls after circuit opens")
}

func TestGracefulDegradationWhenDisabled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("should not make HTTP calls when disabled")
	}))
	defer srv.Close()

	cb := newTestBreaker(time.Now)
	c := NewClient(testLogger(), Config{
		Endpoint:       srv.URL,
		TimeoutSeconds: 5,
		Enabled:        false,
	}, cb, nil)

	ctx := context.Background()
	err := c.Store(ctx, "k", "v", nil)
	assert.NoError(t, err)

	results, err := c.Search(ctx, "q", 5)
	assert.NoError(t, err)
	assert.Nil(t, results)

	err = c.Delete(ctx, "k")
	assert.NoError(t, err)
}

func TestTimeoutFiresAndRecordedAsFailure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cb := newTestBreaker(time.Now)
	c := NewClient(testLogger(), Config{
		Endpoint:       srv.URL,
		TimeoutSeconds: 1,
		Enabled:        true,
	}, cb, nil)

	ctx := context.Background()
	err := c.Store(ctx, "k", "v", nil)
	require.Error(t, err)
}

func TestSearchReturnsEmptyOnCircuitOpen(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	now := time.Now()
	cb := newTestBreaker(func() time.Time { return now })
	c := NewClient(testLogger(), Config{
		Endpoint:       srv.URL,
		TimeoutSeconds: 5,
		Enabled:        true,
	}, cb, nil)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = c.Store(ctx, "k", "v", nil)
	}
	require.Equal(t, resilience.StateOpen, cb.State())

	results, err := c.Search(ctx, "query", 5)
	assert.NoError(t, err)
	assert.Empty(t, results)

	err = c.Delete(ctx, "k")
	assert.NoError(t, err, "delete should degrade to no-op")

	err = c.Store(ctx, "k", "v", nil)
	assert.NoError(t, err, "store should degrade to no-op")
}
