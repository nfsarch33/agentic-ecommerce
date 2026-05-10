// Package health provides reusable liveness and readiness probe
// handlers for all agentic-ecommerce binaries.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// HealthCheck is the interface that dependency probes must satisfy.
type HealthCheck interface {
	Name() string
	Check(ctx context.Context) error
}

// Response is the JSON body returned by the readiness endpoint.
type Response struct {
	Status string                   `json:"status"`
	Checks map[string]CheckResponse `json:"checks,omitempty"`
}

// CheckResponse is the per-check result embedded in Response.
type CheckResponse struct {
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// Handler serves /healthz (liveness) and /readyz (readiness).
type Handler struct {
	checks  []HealthCheck
	timeout time.Duration
}

// Option configures a Handler.
type Option func(*Handler)

// WithTimeout sets the per-check context deadline.
func WithTimeout(d time.Duration) Option {
	return func(h *Handler) { h.timeout = d }
}

// NewHandler creates an HTTP handler that exposes /healthz and /readyz.
func NewHandler(checks []HealthCheck, opts ...Option) *Handler {
	h := &Handler{
		checks:  checks,
		timeout: 2 * time.Second,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Mux returns an http.ServeMux wired with the probe endpoints.
func (h *Handler) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.Liveness)
	mux.HandleFunc("/readyz", h.Readiness)
	return mux
}

// Liveness always returns 200 if the process is alive.
func (h *Handler) Liveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Response{Status: "ok"})
}

// Readiness runs all registered checks concurrently and returns 200
// only when every check passes.
func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	results := h.runChecks(r.Context())

	resp := Response{
		Status: "ready",
		Checks: results,
	}
	code := http.StatusOK
	for _, cr := range results {
		if cr.Status != "ok" {
			resp.Status = "not_ready"
			code = http.StatusServiceUnavailable
			break
		}
	}
	writeJSON(w, code, resp)
}

func (h *Handler) runChecks(parent context.Context) map[string]CheckResponse {
	out := make(map[string]CheckResponse, len(h.checks))
	if len(h.checks) == 0 {
		return out
	}

	type result struct {
		name string
		cr   CheckResponse
	}
	ch := make(chan result, len(h.checks))

	var wg sync.WaitGroup
	for _, check := range h.checks {
		wg.Add(1)
		go func(c HealthCheck) {
			defer wg.Done()
			start := time.Now()
			ctx, cancel := context.WithTimeout(parent, h.timeout)
			defer cancel()

			cr := CheckResponse{Status: "ok"}
			if err := c.Check(ctx); err != nil {
				cr.Status = "fail"
				cr.Error = err.Error()
			}
			cr.LatencyMS = time.Since(start).Milliseconds()
			ch <- result{name: c.Name(), cr: cr}
		}(check)
	}

	go func() { wg.Wait(); close(ch) }()

	for r := range ch {
		out[r.name] = r.cr
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// --- Built-in checks ---

// PostgresCheck pings a database via the provided PingFunc.
type PostgresCheck struct {
	PingFunc func(ctx context.Context) error
}

func (c *PostgresCheck) Name() string                    { return "postgres" }
func (c *PostgresCheck) Check(ctx context.Context) error { return c.PingFunc(ctx) }

// RedisCheck pings a Redis instance via the provided PingFunc.
type RedisCheck struct {
	PingFunc func(ctx context.Context) error
}

func (c *RedisCheck) Name() string                    { return "redis" }
func (c *RedisCheck) Check(ctx context.Context) error { return c.PingFunc(ctx) }

// TemporalCheck verifies Temporal server connectivity.
type TemporalCheck struct {
	PingFunc func(ctx context.Context) error
}

func (c *TemporalCheck) Name() string                    { return "temporal" }
func (c *TemporalCheck) Check(ctx context.Context) error { return c.PingFunc(ctx) }
