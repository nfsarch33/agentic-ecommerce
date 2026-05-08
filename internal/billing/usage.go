package billing

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// UsageMeter records and rolls up tenant usage by metric+period.
// Adapters are pluggable: in-memory for tests, Redis for prod.
type UsageMeter interface {
	Record(ctx context.Context, tenantID, metric string, value int64, at time.Time) error
	Sum(ctx context.Context, tenantID, metric string, periodStart, periodEnd time.Time) (int64, error)
}

// MetricAPIRequests is the canonical metric for API request counts.
const MetricAPIRequests = "api.requests"

// MetricAgentRuns is the canonical metric for agent workflow runs.
const MetricAgentRuns = "agent.runs"

// MetricStorageBytes is the canonical metric for cumulative storage
// usage. Stored as bytes; rollups sum across the period.
const MetricStorageBytes = "storage.bytes"

// InMemoryUsageMeter is a goroutine-safe in-process UsageMeter.
type InMemoryUsageMeter struct {
	mu      sync.RWMutex
	records map[string][]usageRow
}

type usageRow struct {
	Metric string
	Value  int64
	At     time.Time
}

// NewInMemoryUsageMeter returns an empty in-memory meter.
func NewInMemoryUsageMeter() *InMemoryUsageMeter {
	return &InMemoryUsageMeter{records: make(map[string][]usageRow)}
}

// Record appends a usage row for tenantID+metric at time at.
func (m *InMemoryUsageMeter) Record(_ context.Context, tenantID, metric string, value int64, at time.Time) error {
	if tenantID == "" {
		return ErrTenantRequired
	}
	if metric == "" {
		return fmt.Errorf("usage meter: metric required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	m.records[tenantID] = append(m.records[tenantID], usageRow{Metric: metric, Value: value, At: at.UTC()})
	return nil
}

// Sum returns the total value for (tenant, metric) within
// [periodStart, periodEnd]. Open-ended periods (zero values) match
// "from beginning" / "to end".
func (m *InMemoryUsageMeter) Sum(_ context.Context, tenantID, metric string, periodStart, periodEnd time.Time) (int64, error) {
	if tenantID == "" {
		return 0, ErrTenantRequired
	}
	if metric == "" {
		return 0, fmt.Errorf("usage meter: metric required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var total int64
	for _, row := range m.records[tenantID] {
		if row.Metric != metric {
			continue
		}
		if !periodStart.IsZero() && row.At.Before(periodStart) {
			continue
		}
		if !periodEnd.IsZero() && row.At.After(periodEnd) {
			continue
		}
		total += row.Value
	}
	return total, nil
}

// Snapshot returns a deterministic ordered slice for tests.
func (m *InMemoryUsageMeter) Snapshot(tenantID string) []UsageRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rows := m.records[tenantID]
	out := make([]UsageRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, UsageRecord{
			TenantID:   tenantID,
			Metric:     row.Metric,
			Value:      row.Value,
			RecordedAt: row.At,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RecordedAt.Before(out[j].RecordedAt) })
	return out
}

// UsageRollup pairs a metric with its current-period total.
type UsageRollup struct {
	Metric string `json:"metric"`
	Value  int64  `json:"value"`
	Limit  int64  `json:"limit"`
}

// Snapshot returns the per-metric rollup for the current period for
// tenantID, comparing against the plan's quota limits.
func Snapshot(ctx context.Context, meter UsageMeter, plan Plan, tenantID string, periodStart, periodEnd time.Time) ([]UsageRollup, error) {
	if meter == nil {
		return nil, fmt.Errorf("usage meter required")
	}
	metrics := []struct {
		name  string
		limit int64
	}{
		{MetricAPIRequests, int64(plan.APIRatePerMinute)},
		{MetricAgentRuns, int64(plan.AgentRunsPerDay)},
		{MetricStorageBytes, plan.StorageBytes},
	}
	out := make([]UsageRollup, 0, len(metrics))
	for _, m := range metrics {
		value, err := meter.Sum(ctx, tenantID, m.name, periodStart, periodEnd)
		if err != nil {
			return nil, err
		}
		out = append(out, UsageRollup{Metric: m.name, Value: value, Limit: m.limit})
	}
	return out, nil
}
