// Package mem0 provides a thin HTTP client for the mem0 memory API,
// wrapped with circuit-breaker resilience from internal/resilience.
// Refactored in v5.3.0 to use internal/httpclient for base transport.
package mem0

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/httpclient"
	"github.com/nfsarch33/helixon-ec/internal/metrics"
	"github.com/nfsarch33/helixon-ec/internal/resilience"
)

// MemoryResult is a single result from a mem0 Search.
type MemoryResult struct {
	ID       string            `json:"id"`
	Memory   string            `json:"memory"`
	Score    float64           `json:"score"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Config holds mem0 client configuration sourced from env vars.
type Config struct {
	Endpoint       string
	TimeoutSeconds int
	Enabled        bool
}

// ConfigFromEnv reads mem0 config from environment variables.
func ConfigFromEnv() Config {
	enabled := true
	if v := os.Getenv("EC_MEM0_ENABLED"); v == "false" || v == "0" {
		enabled = false
	}
	timeout := 5
	if v := os.Getenv("EC_MEM0_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = n
		}
	}
	return Config{
		Endpoint:       os.Getenv("EC_MEM0_ENDPOINT"),
		TimeoutSeconds: timeout,
		Enabled:        enabled,
	}
}

// Client is the mem0 API client with circuit-breaker protection.
// v5.3.0: uses internal/httpclient for shared transport.
type Client struct {
	cfg     Config
	hc      *httpclient.Client
	breaker *resilience.CircuitBreaker
	logger  *slog.Logger
	reg     *metrics.Registry
}

// NewClient creates a mem0 client. Pass nil registry to skip metrics.
func NewClient(
	logger *slog.Logger,
	cfg Config,
	cb *resilience.CircuitBreaker,
	reg *metrics.Registry,
) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:8080"
	}
	hc, _ := httpclient.New(httpclient.Config{
		BaseURL: endpoint,
		Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
		RequestHooks: []httpclient.RequestHook{
			httpclient.JSONRequestHook(),
		},
	})
	return &Client{
		cfg:     cfg,
		hc:      hc,
		breaker: cb,
		logger:  logger,
		reg:     reg,
	}
}

// Store persists a key-value pair in mem0.
func (c *Client) Store(
	ctx context.Context,
	key, value string,
	metadata map[string]string,
) error {
	if !c.cfg.Enabled || c.cfg.Endpoint == "" {
		c.observe("store", "disabled", 0)
		return nil
	}
	start := time.Now()
	err := c.breaker.Do(ctx, func(ctx context.Context) error {
		body := map[string]any{
			"messages": []map[string]string{
				{"role": "user", "content": value},
			},
			"user_id":  key,
			"metadata": metadata,
		}
		_, status, err := c.hc.PostJSON(ctx, "/v1/memories/", body)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("mem0 POST %d", status)
		}
		return nil
	})
	c.observe("store", statusLabel(err), time.Since(start))
	if err != nil {
		if c.isCircuitOpen(err) {
			c.logger.Warn("mem0.store: circuit open, degrading",
				"key", key)
			return nil
		}
		return fmt.Errorf("mem0.store: %w", err)
	}
	return nil
}

// Search queries mem0 for relevant memories.
func (c *Client) Search(
	ctx context.Context,
	query string,
	limit int,
) ([]MemoryResult, error) {
	if !c.cfg.Enabled || c.cfg.Endpoint == "" {
		c.observe("search", "disabled", 0)
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	var results []MemoryResult
	start := time.Now()
	err := c.breaker.Do(ctx, func(ctx context.Context) error {
		body := map[string]any{
			"query": query,
			"limit": limit,
		}
		resp, status, err := c.hc.PostJSON(ctx, "/v1/memories/search/", body)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("mem0 search %d: %s", status, resp)
		}
		return json.Unmarshal(resp, &results)
	})
	c.observe("search", statusLabel(err), time.Since(start))
	if err != nil {
		if c.isCircuitOpen(err) {
			c.logger.Warn("mem0.search: circuit open, returning empty")
			return nil, nil
		}
		return nil, fmt.Errorf("mem0.search: %w", err)
	}
	return results, nil
}

// Delete removes a memory entry by key.
func (c *Client) Delete(ctx context.Context, key string) error {
	if !c.cfg.Enabled || c.cfg.Endpoint == "" {
		c.observe("delete", "disabled", 0)
		return nil
	}
	start := time.Now()
	err := c.breaker.Do(ctx, func(ctx context.Context) error {
		path := "/v1/memories/" + key + "/"
		_, status, err := c.hc.Do(ctx, http.MethodDelete, path, nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("mem0 DELETE %d", status)
		}
		return nil
	})
	c.observe("delete", statusLabel(err), time.Since(start))
	if err != nil {
		if c.isCircuitOpen(err) {
			c.logger.Warn("mem0.delete: circuit open, degrading",
				"key", key)
			return nil
		}
		return fmt.Errorf("mem0.delete: %w", err)
	}
	return nil
}

func (c *Client) isCircuitOpen(err error) bool {
	return err == resilience.ErrCircuitOpen
}

func (c *Client) observe(op, status string, dur time.Duration) {
	if c.reg == nil {
		return
	}
	c.reg.Mem0Requests.Inc(metrics.Labels{"op": op, "status": status})
	if dur > 0 {
		c.reg.Mem0Duration.Observe(
			dur.Seconds(),
			metrics.Labels{"op": op},
		)
	}
}

func statusLabel(err error) string {
	if err == nil {
		return "ok"
	}
	if err == resilience.ErrCircuitOpen {
		return "circuit_open"
	}
	return "error"
}
