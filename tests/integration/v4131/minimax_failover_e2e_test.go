//go:build v4131_smoke

package v4131_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/llm"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"github.com/nfsarch33/helixon-ec/internal/resilience"
)

func TestE2ENormalOperationKey1Serves(t *testing.T) {
	t.Parallel()

	var keysUsed []string
	adapter, err := llm.NewMinimaxAdapter(llm.MinimaxAdapterConfig{
		APIKey1:   "key1",
		APIKey2:   "key2",
		StickyKey: "1",
		CompleteFn: func(_ context.Context, key string, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
			keysUsed = append(keysUsed, key)
			return port.AICompletionResponse{Content: "ok-" + key, TokensUsed: 1}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewMinimaxAdapter: %v", err)
	}

	minimaxProv := &adapterProvider{name: "minimax", adapter: adapter}
	template := llm.NewTemplateProvider("fallback")

	chain, err := llm.NewLLMFailoverChain(llm.FailoverChainConfig{
		FailoverOrder: "minimax,template",
	}, map[string]llm.Provider{
		"minimax":  minimaxProv,
		"template": template,
	})
	if err != nil {
		t.Fatalf("NewLLMFailoverChain: %v", err)
	}

	for i := 0; i < 5; i++ {
		resp, err := chain.Execute(context.Background(), port.AICompletionRequest{
			Messages: []port.AIMessage{{Role: "user", Content: "test"}},
		})
		if err != nil {
			t.Fatalf("Execute[%d]: %v", i, err)
		}
		if resp.Content != "ok-1" {
			t.Fatalf("Execute[%d] content = %q, want ok-1", i, resp.Content)
		}
	}
	for _, k := range keysUsed {
		if k != "1" {
			t.Fatalf("expected all requests on key 1, got %s", k)
		}
	}
}

func TestE2EKey1RateLimitedFailoverToKey2(t *testing.T) {
	t.Parallel()

	var failovers []string
	adapter, err := llm.NewMinimaxAdapter(llm.MinimaxAdapterConfig{
		APIKey1:   "key1",
		APIKey2:   "key2",
		StickyKey: "1",
		CompleteFn: func(_ context.Context, key string, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
			if key == "1" {
				return port.AICompletionResponse{}, fmt.Errorf("minimax bridge status 429: rate limited")
			}
			return port.AICompletionResponse{Content: "ok-" + key, TokensUsed: 1}, nil
		},
		OnFailover: func(from, to string) { failovers = append(failovers, from+"->"+to) },
	})
	if err != nil {
		t.Fatalf("NewMinimaxAdapter: %v", err)
	}

	minimaxProv := &adapterProvider{name: "minimax", adapter: adapter}
	template := llm.NewTemplateProvider("fallback")

	chain, err := llm.NewLLMFailoverChain(llm.FailoverChainConfig{
		FailoverOrder: "minimax,template",
	}, map[string]llm.Provider{
		"minimax":  minimaxProv,
		"template": template,
	})
	if err != nil {
		t.Fatalf("NewLLMFailoverChain: %v", err)
	}

	resp, err := chain.Execute(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "ok-2" {
		t.Fatalf("content = %q, want ok-2", resp.Content)
	}
	if len(failovers) != 1 {
		t.Fatalf("failover count = %d, want 1", len(failovers))
	}
}

func TestE2EBothExhaustedTemplateFallback(t *testing.T) {
	t.Parallel()

	adapter, err := llm.NewMinimaxAdapter(llm.MinimaxAdapterConfig{
		APIKey1:   "key1",
		APIKey2:   "key2",
		StickyKey: "1",
		CompleteFn: func(_ context.Context, _ string, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
			return port.AICompletionResponse{}, fmt.Errorf("minimax bridge status 429: rate limited")
		},
	})
	if err != nil {
		t.Fatalf("NewMinimaxAdapter: %v", err)
	}

	minimaxProv := &adapterProvider{name: "minimax", adapter: adapter}
	template := llm.NewTemplateProvider("emergency-fallback")
	registry := resilience.NewRegistry(slog.Default())

	chain, err := llm.NewLLMFailoverChain(llm.FailoverChainConfig{
		FailoverOrder: "minimax,template",
		CBRegistry:    registry,
	}, map[string]llm.Provider{
		"minimax":  minimaxProv,
		"template": template,
	})
	if err != nil {
		t.Fatalf("NewLLMFailoverChain: %v", err)
	}

	resp, err := chain.Execute(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "emergency-fallback" {
		t.Fatalf("content = %q, want emergency-fallback", resp.Content)
	}
}

func TestE2EKey1CooldownExpiresRecovery(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
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

	req := port.AICompletionRequest{Messages: []port.AIMessage{{Role: "user", Content: "test"}}}
	_, err = adapter.Complete(context.Background(), req)
	if !errors.Is(err, llm.ErrAllKeysExhausted) {
		t.Fatalf("first call err = %v, want ErrAllKeysExhausted", err)
	}

	clock.Advance(31 * time.Second)

	resp, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("post-cooldown: %v", err)
	}
	if resp.Content != "recovered-1" {
		t.Fatalf("content = %q, want recovered-1", resp.Content)
	}
}

type adapterProvider struct {
	name    string
	adapter *llm.MinimaxAdapter
}

func (a *adapterProvider) Complete(ctx context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	return a.adapter.Complete(ctx, req)
}

func (a *adapterProvider) Name() string { return a.name }

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
