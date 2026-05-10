package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/nfsarch33/agentic-ecommerce/internal/resilience"
)

var (
	ErrNoProviders        = errors.New("llm_failover: no providers configured")
	ErrAllProvidersFailed = errors.New("llm_failover: all providers failed")
)

const defaultFailoverOrder = "ironclaw,minimax,template"

// Provider is an LLM backend that can serve text generation requests.
type Provider interface {
	Complete(ctx context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error)
	Name() string
}

// FailoverChainConfig configures the unified LLM failover chain.
type FailoverChainConfig struct {
	FailoverOrder string
	CBRegistry    *resilience.Registry
	Logger        *slog.Logger
	OnFailover    func(from, to, reason string)
}

// LLMFailoverChain tries providers in order, skipping those with open
// circuit breakers, and recording failures for breaker state tracking.
type LLMFailoverChain struct {
	providers  map[string]Provider
	order      []string
	cbRegistry *resilience.Registry
	logger     *slog.Logger
	onFailover func(from, to, reason string)
}

func NewLLMFailoverChain(cfg FailoverChainConfig, providers map[string]Provider) (*LLMFailoverChain, error) {
	order := parseOrder(cfg.FailoverOrder)
	if len(order) == 0 {
		return nil, ErrNoProviders
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &LLMFailoverChain{
		providers:  providers,
		order:      order,
		cbRegistry: cfg.CBRegistry,
		logger:     cfg.Logger,
		onFailover: cfg.OnFailover,
	}, nil
}

// Execute tries each provider in the failover order. Providers with
// open circuit breakers are skipped. Returns the first successful
// response or ErrAllProvidersFailed.
func (c *LLMFailoverChain) Execute(ctx context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	var lastErr error
	var lastProvider string

	for _, name := range c.order {
		prov, ok := c.providers[name]
		if !ok {
			continue
		}

		if c.isCircuitOpen(name) {
			c.logger.Debug("llm_failover.skip_open_circuit", "provider", name)
			if lastProvider != "" && c.onFailover != nil {
				c.onFailover(name, "", "circuit_open")
			}
			continue
		}

		resp, err := c.tryProvider(ctx, name, prov, req)
		if err == nil {
			return resp, nil
		}

		if lastProvider != "" && c.onFailover != nil {
			c.onFailover(lastProvider, name, "provider_failed")
		}
		lastProvider = name
		lastErr = err
	}

	if lastErr == nil {
		return port.AICompletionResponse{}, ErrAllProvidersFailed
	}
	return port.AICompletionResponse{}, fmt.Errorf("%w: %v", ErrAllProvidersFailed, lastErr)
}

func (c *LLMFailoverChain) tryProvider(ctx context.Context, name string, prov Provider, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	if c.cbRegistry == nil {
		return prov.Complete(ctx, req)
	}

	cb := c.cbRegistry.Get("llm:"+name, resilience.CBConfig{
		FailureThreshold: 3,
		SuccessThreshold: 1,
		CooldownDuration: 30 * time.Second,
	})

	var resp port.AICompletionResponse
	err := cb.Do(ctx, func(innerCtx context.Context) error {
		var callErr error
		resp, callErr = prov.Complete(innerCtx, req)
		return callErr
	})
	return resp, err
}

func (c *LLMFailoverChain) isCircuitOpen(name string) bool {
	if c.cbRegistry == nil {
		return false
	}
	cb := c.cbRegistry.Get("llm:"+name, resilience.CBConfig{
		FailureThreshold: 3,
		SuccessThreshold: 1,
		CooldownDuration: 30 * time.Second,
	})
	return cb.State() == resilience.StateOpen
}

func parseOrder(s string) []string {
	if s == "" {
		s = defaultFailoverOrder
	}
	parts := strings.Split(s, ",")
	var order []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			order = append(order, p)
		}
	}
	return order
}

// TemplateProvider is a simple fallback that returns static template
// content when all LLM providers are exhausted.
type TemplateProvider struct {
	template string
}

func NewTemplateProvider(tmpl string) *TemplateProvider {
	return &TemplateProvider{template: tmpl}
}

func (t *TemplateProvider) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	return port.AICompletionResponse{Content: t.template, TokensUsed: 0}, nil
}

func (t *TemplateProvider) Name() string { return "template" }
