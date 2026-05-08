// Package quota implements per-tenant resource limits enforced at the
// API boundary. Limits are sourced from the billing plan catalog so a
// plan upgrade automatically raises a tenant's ceiling.
//
// The package is deliberately small: a typed Policy value type, a
// pluggable Enforcer interface, and an in-memory implementation
// keyed by (tenant, metric, day-bucket). Production wiring uses the
// Redis-backed UsageMeter from internal/billing.
package quota

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrQuotaExceeded is the typed sentinel returned when a check would
// push usage past the plan limit. Handlers translate it to HTTP 429.
var ErrQuotaExceeded = errors.New("quota exceeded")

// ErrTenantRequired is returned when a quota check is invoked without
// a tenant id.
var ErrTenantRequired = errors.New("quota tenant id required")

// Metric is the typed name of a resource limit. Keep the set small so
// the policy evaluator stays a switch.
type Metric string

const (
	// MetricAPIPerMinute tracks API requests per rolling minute.
	MetricAPIPerMinute Metric = "api.requests.per_minute"
	// MetricAgentRunsPerDay tracks agent workflow runs per UTC day.
	MetricAgentRunsPerDay Metric = "agent.runs.per_day"
	// MetricStorageBytes tracks total stored bytes for a tenant.
	MetricStorageBytes Metric = "storage.bytes"
	// MetricPluginCount tracks installed marketplace plugins for a
	// tenant.
	MetricPluginCount Metric = "plugin.count"
)

// Policy captures the limits a tenant is entitled to. Zero limits
// mean "no quota enforced" for that metric.
type Policy struct {
	APIRatePerMinute int   `json:"api_rate_per_minute"`
	AgentRunsPerDay  int   `json:"agent_runs_per_day"`
	StorageBytes     int64 `json:"storage_bytes"`
	PluginCount      int   `json:"plugin_count"`
}

// Limit returns the int64 limit for metric. Zero -> not enforced.
func (p Policy) Limit(metric Metric) int64 {
	switch metric {
	case MetricAPIPerMinute:
		return int64(p.APIRatePerMinute)
	case MetricAgentRunsPerDay:
		return int64(p.AgentRunsPerDay)
	case MetricStorageBytes:
		return p.StorageBytes
	case MetricPluginCount:
		return int64(p.PluginCount)
	default:
		return 0
	}
}

// Enforcer atomically checks and increments the per-tenant counter
// for metric. Returns ErrQuotaExceeded when the increment would push
// usage above the policy limit.
type Enforcer interface {
	CheckAndIncrement(ctx context.Context, tenantID string, metric Metric, delta int64, policy Policy) error
}

// InMemoryEnforcer is a goroutine-safe Enforcer that buckets counters
// by (tenant, metric, time bucket). Buckets are minute-granular for
// rate limits and day-granular for daily limits.
type InMemoryEnforcer struct {
	mu      sync.Mutex
	buckets map[string]int64
	now     func() time.Time
}

// NewInMemoryEnforcer builds an empty enforcer.
func NewInMemoryEnforcer(now func() time.Time) *InMemoryEnforcer {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &InMemoryEnforcer{buckets: make(map[string]int64), now: now}
}

// CheckAndIncrement implements Enforcer.
func (e *InMemoryEnforcer) CheckAndIncrement(_ context.Context, tenantID string, metric Metric, delta int64, policy Policy) error {
	if tenantID == "" {
		return ErrTenantRequired
	}
	if delta <= 0 {
		delta = 1
	}
	limit := policy.Limit(metric)
	if limit <= 0 {
		return nil
	}
	key := bucketKey(tenantID, metric, e.now())
	e.mu.Lock()
	defer e.mu.Unlock()
	current := e.buckets[key]
	if current+delta > limit {
		return fmt.Errorf("%w: tenant=%q metric=%s used=%d limit=%d", ErrQuotaExceeded, tenantID, metric, current, limit)
	}
	e.buckets[key] = current + delta
	return nil
}

// Snapshot returns the current bucket value for diagnostic tests.
func (e *InMemoryEnforcer) Snapshot(tenantID string, metric Metric) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.buckets[bucketKey(tenantID, metric, e.now())]
}

// bucketKey returns a deterministic counter key per metric. Per-minute
// metrics bucket by minute; per-day by UTC day; cumulative metrics
// (storage, plugin count) by tenant only.
func bucketKey(tenantID string, metric Metric, now time.Time) string {
	switch metric {
	case MetricAPIPerMinute:
		return fmt.Sprintf("%s|%s|%s", tenantID, metric, now.UTC().Format("2006-01-02T15:04"))
	case MetricAgentRunsPerDay:
		return fmt.Sprintf("%s|%s|%s", tenantID, metric, now.UTC().Format("2006-01-02"))
	default:
		return fmt.Sprintf("%s|%s", tenantID, metric)
	}
}

// PolicyFromLimits builds a Policy from raw limit values. Used by the
// billing plan adapter so quota stays in sync with billing.
func PolicyFromLimits(apiPerMinute, agentRunsPerDay int, storageBytes int64, pluginCount int) Policy {
	return Policy{
		APIRatePerMinute: apiPerMinute,
		AgentRunsPerDay:  agentRunsPerDay,
		StorageBytes:     storageBytes,
		PluginCount:      pluginCount,
	}
}
