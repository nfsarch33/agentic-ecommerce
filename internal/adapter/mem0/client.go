// Package mem0 provides a thin HTTP client for the mem0 memory API,
// wrapped with circuit-breaker resilience from internal/resilience.
package mem0

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/resilience"
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
type Client struct {
	cfg     Config
	http    *http.Client
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
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
		},
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
		return c.post(ctx, "/v1/memories/", body)
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
		resp, err := c.doPost(ctx, "/v1/memories/search/", body)
		if err != nil {
			return err
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
		url := c.cfg.Endpoint + "/v1/memories/" + key + "/"
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("mem0 DELETE %d", resp.StatusCode)
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

func (c *Client) post(ctx context.Context, path string, body any) error {
	_, err := c.doPost(ctx, path, body)
	return err
}

func (c *Client) doPost(
	ctx context.Context,
	path string,
	body any,
) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("mem0: marshal: %w", err)
	}
	url := c.cfg.Endpoint + path
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, url, bytes.NewReader(payload),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mem0 %s %d: %s", path, resp.StatusCode, data)
	}
	return data, nil
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
