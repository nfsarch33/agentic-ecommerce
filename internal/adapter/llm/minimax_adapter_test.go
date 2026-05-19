package llm_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/llm"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

type stubKeyStore struct {
	mu     sync.Mutex
	states map[string]llm.KeyState
}

func newStubKeyStore() *stubKeyStore {
	return &stubKeyStore{states: make(map[string]llm.KeyState)}
}

func (s *stubKeyStore) LoadKeyState(_ context.Context, alias string) (llm.KeyState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[alias]
	if !ok {
		return llm.KeyState{}, errors.New("not found")
	}
	return st, nil
}

func (s *stubKeyStore) SaveKeyState(_ context.Context, state llm.KeyState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.Alias] = state
	return nil
}

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func successCompleteFn(_ context.Context, _ string, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	return port.AICompletionResponse{Content: "generated", TokensUsed: 10}, nil
}

func rateLimitedCompleteFn(keyToFail string) func(context.Context, string, port.AICompletionRequest) (port.AICompletionResponse, error) {
	return func(_ context.Context, key string, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
		if key == keyToFail {
			return port.AICompletionResponse{}, fmt.Errorf("minimax bridge status 429: rate limited")
		}
		return port.AICompletionResponse{Content: "ok", TokensUsed: 5}, nil
	}
}

func quotaExhaustedCompleteFn(keyToFail string) func(context.Context, string, port.AICompletionRequest) (port.AICompletionResponse, error) {
	return func(_ context.Context, key string, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
		if key == keyToFail {
			return port.AICompletionResponse{}, fmt.Errorf("minimax bridge status 402: quota exhausted")
		}
		return port.AICompletionResponse{Content: "ok", TokensUsed: 5}, nil
	}
}

func allFailCompleteFn(_ context.Context, _ string, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	return port.AICompletionResponse{}, fmt.Errorf("minimax bridge status 429: rate limited")
}

func TestStickyKeyUsed(t *testing.T) {
	t.Parallel()

	var usedKey string
	adapter, err := llm.NewMinimaxAdapter(llm.MinimaxAdapterConfig{
		APIKey1:   "key1",
		APIKey2:   "key2",
		StickyKey: "1",
		CompleteFn: func(_ context.Context, key string, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
			usedKey = key
			return port.AICompletionResponse{Content: "ok", TokensUsed: 1}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewMinimaxAdapter: %v", err)
	}

	req := port.AICompletionRequest{Messages: []port.AIMessage{{Role: "user", Content: "hi"}}}
	resp, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if usedKey != "1" {
		t.Fatalf("used key = %q, want 1", usedKey)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
}

func TestFailoverOn429(t *testing.T) {
	t.Parallel()

	var failovers []string
	adapter, err := llm.NewMinimaxAdapter(llm.MinimaxAdapterConfig{
		APIKey1:    "key1",
		APIKey2:    "key2",
		StickyKey:  "1",
		CompleteFn: rateLimitedCompleteFn("1"),
		OnFailover: func(from, to string) { failovers = append(failovers, from+"->"+to) },
	})
	if err != nil {
		t.Fatalf("NewMinimaxAdapter: %v", err)
	}

	req := port.AICompletionRequest{Messages: []port.AIMessage{{Role: "user", Content: "hi"}}}
	resp, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
	if len(failovers) != 1 || failovers[0] != "1->2" {
		t.Fatalf("failovers = %v, want [1->2]", failovers)
	}
}

func TestFailoverOn402(t *testing.T) {
	t.Parallel()

	adapter, err := llm.NewMinimaxAdapter(llm.MinimaxAdapterConfig{
		APIKey1:    "key1",
		APIKey2:    "key2",
		StickyKey:  "1",
		CompleteFn: quotaExhaustedCompleteFn("1"),
	})
	if err != nil {
		t.Fatalf("NewMinimaxAdapter: %v", err)
	}

	resp, err := adapter.Complete(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "write"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
	if adapter.ActiveKey() != "2" {
		t.Fatalf("active key = %q, want 2", adapter.ActiveKey())
	}
}

func TestCooldownRespected(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	store := newStubKeyStore()

	adapter, err := llm.NewMinimaxAdapter(llm.MinimaxAdapterConfig{
		APIKey1:         "key1",
		APIKey2:         "key2",
		StickyKey:       "1",
		CooldownSeconds: 60,
		NowFunc:         clock.Now,
		KeyStateStore:   store,
		CompleteFn:      allFailCompleteFn,
	})
	if err != nil {
		t.Fatalf("NewMinimaxAdapter: %v", err)
	}

	req := port.AICompletionRequest{Messages: []port.AIMessage{{Role: "user", Content: "hi"}}}
	_, err = adapter.Complete(context.Background(), req)
	if !errors.Is(err, llm.ErrAllKeysExhausted) {
		t.Fatalf("err = %v, want ErrAllKeysExhausted", err)
	}

	remaining := adapter.CooldownRemaining("1")
	if remaining < 50 || remaining > 61 {
		t.Fatalf("cooldown remaining = %f, want ~60", remaining)
	}

	clock.Advance(61 * time.Second)
	adapter2, err := llm.NewMinimaxAdapter(llm.MinimaxAdapterConfig{
		APIKey1:         "key1",
		APIKey2:         "key2",
		StickyKey:       "1",
		CooldownSeconds: 60,
		NowFunc:         clock.Now,
		CompleteFn:      successCompleteFn,
	})
	if err != nil {
		t.Fatalf("NewMinimaxAdapter: %v", err)
	}
	resp, err := adapter2.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete after cooldown: %v", err)
	}
	if resp.Content != "generated" {
		t.Fatalf("content = %q, want generated", resp.Content)
	}
}

func TestBothKeysExhausted(t *testing.T) {
	t.Parallel()

	adapter, err := llm.NewMinimaxAdapter(llm.MinimaxAdapterConfig{
		APIKey1:    "key1",
		APIKey2:    "key2",
		StickyKey:  "1",
		CompleteFn: allFailCompleteFn,
	})
	if err != nil {
		t.Fatalf("NewMinimaxAdapter: %v", err)
	}

	_, err = adapter.Complete(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, llm.ErrAllKeysExhausted) {
		t.Fatalf("err = %v, want ErrAllKeysExhausted", err)
	}
}

func TestKeyRecoveryAfterCooldown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}

	callCount := 0
	adapter, err := llm.NewMinimaxAdapter(llm.MinimaxAdapterConfig{
		APIKey1:         "key1",
		APIKey2:         "key2",
		StickyKey:       "1",
		CooldownSeconds: 30,
		NowFunc:         clock.Now,
		CompleteFn: func(_ context.Context, key string, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
			callCount++
			if callCount <= 2 {
				return port.AICompletionResponse{}, fmt.Errorf("minimax bridge status 429: rate limited")
			}
			return port.AICompletionResponse{Content: "recovered-" + key, TokensUsed: 1}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewMinimaxAdapter: %v", err)
	}

	req := port.AICompletionRequest{Messages: []port.AIMessage{{Role: "user", Content: "hi"}}}
	_, err = adapter.Complete(context.Background(), req)
	if !errors.Is(err, llm.ErrAllKeysExhausted) {
		t.Fatalf("first call err = %v, want ErrAllKeysExhausted", err)
	}

	clock.Advance(31 * time.Second)

	resp, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("post-cooldown Complete: %v", err)
	}
	if resp.Content != "recovered-1" && resp.Content != "recovered-2" {
		t.Fatalf("content = %q, want recovered-{1|2}", resp.Content)
	}
}

type mockClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mockClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
