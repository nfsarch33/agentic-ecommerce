// Package httpclient provides a shared HTTP client base with
// configurable base URL, timeout, retry, circuit breaker delegation,
// and request/response middleware hooks. Extracted in v5.3.0 to
// deduplicate patterns across mem0, AusPost, DHL, and payment adapters.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/resilience"
)

// RequestHook is called before the request is dispatched. Use it
// to inject auth headers, signing, or request-level telemetry.
type RequestHook func(req *http.Request) error

// ResponseHook is called after a successful (non-nil) response
// is received, before the body is consumed. Use it for logging,
// metrics, or header inspection.
type ResponseHook func(resp *http.Response) error

// Config configures a Client instance.
type Config struct {
	BaseURL       string
	Timeout       time.Duration
	MaxRetries    int
	RetryDelay    time.Duration
	MaxBodyBytes  int64
	Breaker       *resilience.CircuitBreaker
	RequestHooks  []RequestHook
	ResponseHooks []ResponseHook
	HTTPClient    *http.Client
}

// Client is a shared HTTP client with base URL, timeout, retry,
// circuit breaker delegation, and middleware hooks.
type Client struct {
	baseURL       string
	httpClient    *http.Client
	maxRetries    int
	retryDelay    time.Duration
	maxBodyBytes  int64
	breaker       *resilience.CircuitBreaker
	requestHooks  []RequestHook
	responseHooks []ResponseHook
}

const (
	defaultTimeout      = 30 * time.Second
	defaultMaxBodyBytes = 8192
	defaultMaxRetries   = 0
	defaultRetryDelay   = 500 * time.Millisecond
)

// New creates a Client from the given Config.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("httpclient: base URL required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	retries := cfg.MaxRetries
	if retries < 0 {
		retries = defaultMaxRetries
	}
	delay := cfg.RetryDelay
	if delay <= 0 {
		delay = defaultRetryDelay
	}
	return &Client{
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		httpClient:    httpClient,
		maxRetries:    retries,
		retryDelay:    delay,
		maxBodyBytes:  maxBody,
		breaker:       cfg.Breaker,
		requestHooks:  cfg.RequestHooks,
		responseHooks: cfg.ResponseHooks,
	}, nil
}

// Do executes an HTTP request, applying circuit breaker wrapping,
// request/response hooks, retry, and body size limits. Returns
// the raw response body on success.
func (c *Client) Do(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	return c.do(ctx, method, path, body, nil)
}

// DoWithHooks executes an HTTP request with additional per-call request
// hooks appended after the client's configured hooks.
func (c *Client) DoWithHooks(ctx context.Context, method, path string, body []byte, hooks ...RequestHook) ([]byte, int, error) {
	return c.do(ctx, method, path, body, hooks)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, hooks []RequestHook) ([]byte, int, error) {
	var respBody []byte
	var statusCode int

	exec := func(ctx context.Context) error {
		var lastErr error
		for attempt := 0; attempt <= c.maxRetries; attempt++ {
			if err := c.waitRetry(ctx, attempt); err != nil {
				return err
			}
			data, status, err := c.doOnce(ctx, method, path, body, hooks)
			if err != nil {
				lastErr = err
				if attempt < c.maxRetries {
					continue
				}
				return lastErr
			}
			statusCode = status
			respBody = data
			if status >= 500 && attempt < c.maxRetries {
				lastErr = fmt.Errorf("httpclient: server error status=%d", status)
				continue
			}
			return nil
		}
		return lastErr
	}

	if c.breaker != nil {
		if err := c.breaker.Do(ctx, exec); err != nil {
			return nil, 0, err
		}
		return respBody, statusCode, nil
	}
	if err := exec(ctx); err != nil {
		return nil, 0, err
	}
	return respBody, statusCode, nil
}

func (c *Client) waitRetry(ctx context.Context, attempt int) error {
	if attempt == 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(c.retryDelay):
		return nil
	}
}

func (c *Client) doOnce(ctx context.Context, method, path string, body []byte, hooks []RequestHook) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("httpclient: build request: %w", err)
	}
	for _, hook := range c.requestHooks {
		if err := hook(req); err != nil {
			return nil, 0, fmt.Errorf("httpclient: request hook: %w", err)
		}
	}
	for _, hook := range hooks {
		if err := hook(req); err != nil {
			return nil, 0, fmt.Errorf("httpclient: request hook: %w", err)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("httpclient: transport: %w", err)
	}
	for _, hook := range c.responseHooks {
		if hookErr := hook(resp); hookErr != nil {
			resp.Body.Close()
			return nil, 0, fmt.Errorf("httpclient: response hook: %w", hookErr)
		}
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, c.maxBodyBytes))
	resp.Body.Close()
	return data, resp.StatusCode, nil
}

// PostJSON marshals body to JSON and dispatches a POST request with
// Content-Type: application/json. Convenience method.
func (c *Client) PostJSON(ctx context.Context, path string, body any) ([]byte, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("httpclient: marshal: %w", err)
	}
	return c.Do(ctx, http.MethodPost, path, payload)
}

// GetJSON dispatches a GET request. Convenience method.
func (c *Client) GetJSON(ctx context.Context, path string) ([]byte, int, error) {
	return c.Do(ctx, http.MethodGet, path, nil)
}

// BaseURL returns the configured base URL for composition.
func (c *Client) BaseURL() string { return c.baseURL }

// JSONRequestHook returns a RequestHook that sets Content-Type to
// application/json, suitable for JSON API clients.
func JSONRequestHook() RequestHook {
	return func(req *http.Request) error {
		if req.Body != nil && req.Body != http.NoBody {
			req.Header.Set("Content-Type", "application/json")
		}
		return nil
	}
}

// BearerAuthHook returns a RequestHook that sets Authorization: Bearer.
func BearerAuthHook(tokenFn func() string) RequestHook {
	return func(req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+tokenFn())
		return nil
	}
}
