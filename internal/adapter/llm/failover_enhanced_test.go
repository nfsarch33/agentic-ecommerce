package llm_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/llm"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/nfsarch33/agentic-ecommerce/internal/resilience"
)

type stubProvider struct {
	name   string
	resp   port.AICompletionResponse
	err    error
	called int
}

func (s *stubProvider) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	s.called++
	return s.resp, s.err
}

func (s *stubProvider) Name() string { return s.name }

func TestIronClawSucceedsMiniMaxNotCalled(t *testing.T) {
	t.Parallel()

	ironclaw := &stubProvider{name: "ironclaw", resp: port.AICompletionResponse{Content: "ironclaw-ok", TokensUsed: 5}}
	minimax := &stubProvider{name: "minimax", resp: port.AICompletionResponse{Content: "minimax-ok", TokensUsed: 3}}
	template := &stubProvider{name: "template", resp: port.AICompletionResponse{Content: "template-ok"}}

	chain, err := llm.NewLLMFailoverChain(llm.FailoverChainConfig{
		FailoverOrder: "ironclaw,minimax,template",
	}, map[string]llm.Provider{
		"ironclaw": ironclaw,
		"minimax":  minimax,
		"template": template,
	})
	if err != nil {
		t.Fatalf("NewLLMFailoverChain: %v", err)
	}

	resp, err := chain.Execute(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "ironclaw-ok" {
		t.Fatalf("content = %q, want ironclaw-ok", resp.Content)
	}
	if minimax.called != 0 {
		t.Fatalf("minimax called %d times, want 0", minimax.called)
	}
}

func TestIronClawFailsMiniMaxSucceeds(t *testing.T) {
	t.Parallel()

	ironclaw := &stubProvider{name: "ironclaw", err: errors.New("ironclaw down")}
	minimax := &stubProvider{name: "minimax", resp: port.AICompletionResponse{Content: "minimax-ok", TokensUsed: 3}}
	template := &stubProvider{name: "template", resp: port.AICompletionResponse{Content: "template-ok"}}

	chain, err := llm.NewLLMFailoverChain(llm.FailoverChainConfig{
		FailoverOrder: "ironclaw,minimax,template",
	}, map[string]llm.Provider{
		"ironclaw": ironclaw,
		"minimax":  minimax,
		"template": template,
	})
	if err != nil {
		t.Fatalf("NewLLMFailoverChain: %v", err)
	}

	resp, err := chain.Execute(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "minimax-ok" {
		t.Fatalf("content = %q, want minimax-ok", resp.Content)
	}
}

func TestBothFailTemplateFallback(t *testing.T) {
	t.Parallel()

	ironclaw := &stubProvider{name: "ironclaw", err: errors.New("ironclaw down")}
	minimax := &stubProvider{name: "minimax", err: errors.New("minimax quota exhausted")}
	template := &stubProvider{name: "template", resp: port.AICompletionResponse{Content: "fallback text"}}

	chain, err := llm.NewLLMFailoverChain(llm.FailoverChainConfig{
		FailoverOrder: "ironclaw,minimax,template",
	}, map[string]llm.Provider{
		"ironclaw": ironclaw,
		"minimax":  minimax,
		"template": template,
	})
	if err != nil {
		t.Fatalf("NewLLMFailoverChain: %v", err)
	}

	resp, err := chain.Execute(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "fallback text" {
		t.Fatalf("content = %q, want fallback text", resp.Content)
	}
}

func TestMiniMaxQuotaExhaustedSkipToTemplate(t *testing.T) {
	t.Parallel()

	ironclaw := &stubProvider{name: "ironclaw", err: errors.New("ironclaw unavailable")}
	minimax := &stubProvider{name: "minimax", err: llm.ErrAllKeysExhausted}
	template := &stubProvider{name: "template", resp: port.AICompletionResponse{Content: "template-fallback"}}

	chain, err := llm.NewLLMFailoverChain(llm.FailoverChainConfig{
		FailoverOrder: "ironclaw,minimax,template",
	}, map[string]llm.Provider{
		"ironclaw": ironclaw,
		"minimax":  minimax,
		"template": template,
	})
	if err != nil {
		t.Fatalf("NewLLMFailoverChain: %v", err)
	}

	resp, err := chain.Execute(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "template-fallback" {
		t.Fatalf("content = %q, want template-fallback", resp.Content)
	}
}

func TestCircuitBreakerOpenSkipsMiniMax(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	registry := resilience.NewRegistry(logger)

	cb := registry.Get("llm:minimax", resilience.CBConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		CooldownDuration: 1<<63 - 1, // effectively infinite
	})
	_ = cb.Do(context.Background(), func(_ context.Context) error {
		return errors.New("trip the breaker")
	})

	ironclaw := &stubProvider{name: "ironclaw", err: errors.New("ironclaw down")}
	minimax := &stubProvider{name: "minimax", resp: port.AICompletionResponse{Content: "should-not-reach"}}
	template := &stubProvider{name: "template", resp: port.AICompletionResponse{Content: "cb-fallback"}}

	chain, err := llm.NewLLMFailoverChain(llm.FailoverChainConfig{
		FailoverOrder: "ironclaw,minimax,template",
		CBRegistry:    registry,
		Logger:        logger,
	}, map[string]llm.Provider{
		"ironclaw": ironclaw,
		"minimax":  minimax,
		"template": template,
	})
	if err != nil {
		t.Fatalf("NewLLMFailoverChain: %v", err)
	}

	resp, err := chain.Execute(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "cb-fallback" {
		t.Fatalf("content = %q, want cb-fallback", resp.Content)
	}
	if minimax.called != 0 {
		t.Fatalf("minimax called %d times, want 0 (circuit open)", minimax.called)
	}
}

func TestTemplateProviderReturnsStaticFallback(t *testing.T) {
	t.Parallel()

	provider := llm.NewTemplateProvider("fallback copy")
	resp, err := provider.Complete(context.Background(), port.AICompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if provider.Name() != "template" {
		t.Fatalf("Name = %q, want template", provider.Name())
	}
	if resp.Content != "fallback copy" || resp.TokensUsed != 0 {
		t.Fatalf("unexpected response: %#v", resp)
	}
}
