// Package llm provides quota-aware LLM adapters and a unified failover
// chain for the EC stack's content-generation surfaces.
package llm

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

var (
	ErrAllKeysExhausted = errors.New("minimax: all API keys exhausted")
	ErrKeyNotConfigured = errors.New("minimax: API key not configured")
)

const (
	defaultCooldownSeconds = 3600
	defaultStickyKey       = "1"
)

type KeyState struct {
	Alias       string    `json:"alias"`
	Exhausted   bool      `json:"exhausted"`
	ExhaustedAt time.Time `json:"exhausted_at,omitempty"`
}

type KeyStateStore interface {
	LoadKeyState(ctx context.Context, alias string) (KeyState, error)
	SaveKeyState(ctx context.Context, state KeyState) error
}

type MinimaxAdapterConfig struct {
	APIKey1         string
	APIKey2         string
	StickyKey       string
	CooldownSeconds int
	NowFunc         func() time.Time
	Logger          *slog.Logger
	KeyStateStore   KeyStateStore
	CompleteFn      func(ctx context.Context, keyAlias string, req port.AICompletionRequest) (port.AICompletionResponse, error)
	OnRequest       func(keyAlias string, dur time.Duration, err error)
	OnFailover      func(fromKey, toKey string)
}

// MinimaxAdapter wraps MiniMax API access with sticky key selection and
// automatic quota-based failover between two API keys.
type MinimaxAdapter struct {
	cfg    MinimaxAdapterConfig
	logger *slog.Logger

	mu        sync.Mutex
	activeKey string
	states    map[string]*KeyState
}

func NewMinimaxAdapter(cfg MinimaxAdapterConfig) (*MinimaxAdapter, error) {
	if cfg.APIKey1 == "" && cfg.APIKey2 == "" {
		return nil, ErrKeyNotConfigured
	}
	if cfg.StickyKey == "" {
		cfg.StickyKey = defaultStickyKey
	}
	if cfg.CooldownSeconds <= 0 {
		cfg.CooldownSeconds = defaultCooldownSeconds
	}
	if cfg.NowFunc == nil {
		cfg.NowFunc = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	a := &MinimaxAdapter{
		cfg:       cfg,
		logger:    cfg.Logger,
		activeKey: cfg.StickyKey,
		states:    make(map[string]*KeyState),
	}
	a.states["1"] = &KeyState{Alias: "1"}
	a.states["2"] = &KeyState{Alias: "2"}
	if cfg.KeyStateStore != nil {
		a.loadPersistedStates(context.Background())
	}
	return a, nil
}

func (a *MinimaxAdapter) Complete(ctx context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	a.mu.Lock()
	primary := a.selectKey()
	a.mu.Unlock()

	if primary == "" {
		return port.AICompletionResponse{}, ErrAllKeysExhausted
	}

	resp, err := a.makeRequest(ctx, primary, req)
	if err == nil {
		return resp, nil
	}
	if !isQuotaError(err) {
		return port.AICompletionResponse{}, err
	}

	return a.handleQuotaError(ctx, primary, req, err)
}

func (a *MinimaxAdapter) selectKey() string {
	a.refreshCooldowns()
	if st, ok := a.states[a.activeKey]; ok && !st.Exhausted {
		return a.activeKey
	}
	alt := a.alternateKey(a.activeKey)
	if st, ok := a.states[alt]; ok && !st.Exhausted {
		return alt
	}
	return ""
}

func (a *MinimaxAdapter) makeRequest(ctx context.Context, keyAlias string, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	if a.cfg.CompleteFn == nil {
		return port.AICompletionResponse{}, errors.New("minimax: no CompleteFn configured")
	}
	start := a.cfg.NowFunc()
	resp, err := a.cfg.CompleteFn(ctx, keyAlias, req)
	dur := a.cfg.NowFunc().Sub(start)
	if a.cfg.OnRequest != nil {
		a.cfg.OnRequest(keyAlias, dur, err)
	}
	return resp, err
}

func (a *MinimaxAdapter) handleQuotaError(ctx context.Context, failedKey string, req port.AICompletionRequest, _ error) (port.AICompletionResponse, error) {
	a.mu.Lock()
	a.markExhausted(failedKey)
	alt := a.alternateKey(failedKey)
	altAvailable := false
	if st, ok := a.states[alt]; ok && !st.Exhausted {
		altAvailable = true
		a.activeKey = alt
	}
	a.mu.Unlock()

	if a.cfg.OnFailover != nil {
		a.cfg.OnFailover(failedKey, alt)
	}
	if !altAvailable {
		return port.AICompletionResponse{}, ErrAllKeysExhausted
	}

	resp, err := a.makeRequest(ctx, alt, req)
	if err != nil && isQuotaError(err) {
		a.mu.Lock()
		a.markExhausted(alt)
		a.mu.Unlock()
		return port.AICompletionResponse{}, ErrAllKeysExhausted
	}
	return resp, err
}

func (a *MinimaxAdapter) markExhausted(alias string) {
	if st, ok := a.states[alias]; ok {
		st.Exhausted = true
		st.ExhaustedAt = a.cfg.NowFunc()
	}
	if a.cfg.KeyStateStore != nil {
		if st, ok := a.states[alias]; ok {
			_ = a.cfg.KeyStateStore.SaveKeyState(context.Background(), *st)
		}
	}
}

func (a *MinimaxAdapter) refreshCooldowns() {
	now := a.cfg.NowFunc()
	cooldown := time.Duration(a.cfg.CooldownSeconds) * time.Second
	for _, st := range a.states {
		if st.Exhausted && now.Sub(st.ExhaustedAt) >= cooldown {
			st.Exhausted = false
			st.ExhaustedAt = time.Time{}
			a.logger.Info("minimax.key_cooldown_recovered", "alias", st.Alias)
		}
	}
}

func (a *MinimaxAdapter) loadPersistedStates(ctx context.Context) {
	for alias, st := range a.states {
		persisted, err := a.cfg.KeyStateStore.LoadKeyState(ctx, alias)
		if err != nil {
			a.logger.Debug("minimax.load_key_state_skipped", "alias", alias, "error", err)
			continue
		}
		st.Exhausted = persisted.Exhausted
		st.ExhaustedAt = persisted.ExhaustedAt
	}
}

func (a *MinimaxAdapter) alternateKey(current string) string {
	if current == "1" {
		return "2"
	}
	return "1"
}

// ActiveKey returns the alias of the currently active key. Thread-safe.
func (a *MinimaxAdapter) ActiveKey() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.activeKey
}

// CooldownRemaining returns seconds until the given key's cooldown expires.
func (a *MinimaxAdapter) CooldownRemaining(alias string) float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.states[alias]
	if !ok || !st.Exhausted {
		return 0
	}
	cooldown := time.Duration(a.cfg.CooldownSeconds) * time.Second
	remaining := cooldown - a.cfg.NowFunc().Sub(st.ExhaustedAt)
	if remaining < 0 {
		return 0
	}
	return remaining.Seconds()
}

func isQuotaError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []string{"status 429", "status 402"} {
		if contains(msg, code) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
