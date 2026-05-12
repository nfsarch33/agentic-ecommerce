// Package spine defines the v7 observability contract shared by metrics,
// dashboard snapshots, and EvoMap/EvoLoop ingestion.
package spine

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/evomap"
)

const SchemaVersion = "ec-observability-spine/v1"

type MetricKind string

const (
	Counter   MetricKind = "counter"
	Gauge     MetricKind = "gauge"
	Histogram MetricKind = "histogram"
)

type LabelContract struct {
	Name           string
	MaxCardinality int
	Values         []string
}

type MetricContract struct {
	Name            string
	Kind            MetricKind
	Owner           string
	Description     string
	Labels          []LabelContract
	DashboardFields []string
}

type DashboardSnapshot struct {
	SchemaVersion string
	RecordedAt    time.Time
	EventAt       time.Time
	Binary        string
	Fields        []KPIField
}

type KPIField struct {
	Name  string
	Value float64
	Unit  string
}

type MetricSample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

func MetricInventory() []MetricContract {
	return []MetricContract{
		{Name: "ec_http_duration_seconds", Kind: Histogram, Owner: "mc-api", Description: "HTTP request latency.", DashboardFields: []string{"p95_ms"}},
		{Name: "ec_oom_alarms_total", Kind: Counter, Owner: "memwatch", Description: "Heap ceiling alarms.", DashboardFields: []string{"oom_alarms"}},
		{Name: "ec_goroutine_count", Kind: Gauge, Owner: "runtime", Description: "Runtime goroutine count.", DashboardFields: []string{"goroutine_count"}},
		{Name: "ec_heap_bytes", Kind: Gauge, Owner: "runtime", Description: "Heap in-use bytes.", DashboardFields: []string{"heap_in_use_bytes"}},
		{Name: "ec_agentrace_session_duration_seconds", Kind: Histogram, Owner: "agentrace", Description: "Observed agent session duration.", DashboardFields: []string{"agentrace_session_duration_seconds"}},
		{
			Name:        "ec_agentrace_tool_calls_total",
			Kind:        Counter,
			Owner:       "agentrace",
			Description: "Observed agent tool calls by tool and outcome.",
			Labels: []LabelContract{
				{Name: "tool_name", MaxCardinality: 30},
				{Name: "outcome", MaxCardinality: 2, Values: []string{"ok", "error"}},
			},
			DashboardFields: []string{"agentrace_tool_call_count"},
		},
		{Name: "ec_agentrace_cost_usd_total", Kind: Counter, Owner: "agentrace", Description: "Observed agent cost.", DashboardFields: []string{"agentrace_cost_usd"}},
		{Name: "ec_agentrace_bottlenecks_total", Kind: Counter, Owner: "agentrace", Description: "Observed agent bottlenecks.", Labels: []LabelContract{{Name: "severity", MaxCardinality: 8, Values: []string{"all"}}}, DashboardFields: []string{"agentrace_bottleneck_count"}},
		{Name: "ec_agentrace_parallelism_ratio", Kind: Gauge, Owner: "agentrace", Description: "Agent parallelism efficiency.", DashboardFields: []string{"agentrace_parallelism_efficiency"}},
		{Name: "ec_workerpool_rejected_total", Kind: Counter, Owner: "workerpool", Description: "Bounded worker pool rejections.", Labels: []LabelContract{{Name: "pool", MaxCardinality: 16}}, DashboardFields: []string{"workerpool_rejected_total"}},
		{Name: "ec_breaker_open_total", Kind: Counter, Owner: "resilience", Description: "Circuit breaker open transitions.", Labels: []LabelContract{{Name: "name", MaxCardinality: 16}}, DashboardFields: []string{"breaker_open_total"}},
		{Name: "ec_coord_conflicts_total", Kind: Counter, Owner: "coord", Description: "Coordination conflict resolutions.", Labels: []LabelContract{{Name: "agent_a", MaxCardinality: 8}, {Name: "agent_b", MaxCardinality: 8}, {Name: "resolution", MaxCardinality: 4}}, DashboardFields: []string{"coord_conflicts_total"}},
		{
			Name:        "ec_marketplace_sync_events_total",
			Kind:        Counter,
			Owner:       "marketplacesync",
			Description: "Marketplace sync events by provider, entity type, and bounded outcome.",
			Labels: []LabelContract{
				{Name: "provider", MaxCardinality: 8, Values: []string{"shopify", "shopee", "tiktok", "facebook"}},
				{Name: "entity_type", MaxCardinality: 8, Values: []string{"product", "inventory", "order"}},
				{Name: "status", MaxCardinality: 8, Values: []string{"applied", "duplicate", "retry", "dlq", "failed"}},
			},
			DashboardFields: []string{"marketplace_sync_events_total"},
		},
		{
			Name:        "ec_marketplace_sync_dlq_total",
			Kind:        Counter,
			Owner:       "marketplacesync",
			Description: "Marketplace sync DLQ enqueues by provider, entity type, and bounded reason.",
			Labels: []LabelContract{
				{Name: "provider", MaxCardinality: 8, Values: []string{"shopify", "shopee", "tiktok", "facebook"}},
				{Name: "entity_type", MaxCardinality: 8, Values: []string{"product", "inventory", "order"}},
				{Name: "reason", MaxCardinality: 8, Values: []string{"transient", "permanent", "validation", "conflict", "timeout", "unknown"}},
			},
			DashboardFields: []string{"marketplace_sync_dlq_total"},
		},
		{
			Name:        "ec_marketplace_replay_total",
			Kind:        Counter,
			Owner:       "marketplacesync",
			Description: "Marketplace replay outcomes by provider, entity type, and bounded status.",
			Labels: []LabelContract{
				{Name: "provider", MaxCardinality: 8, Values: []string{"shopify", "shopee", "tiktok", "facebook"}},
				{Name: "entity_type", MaxCardinality: 8, Values: []string{"product", "inventory", "order"}},
				{Name: "status", MaxCardinality: 8, Values: []string{"replayed", "duplicate", "failed", "skipped"}},
			},
			DashboardFields: []string{"marketplace_replay_total"},
		},
	}
}

func ValidateMetricInventory(inventory []MetricContract) error {
	seen := make(map[string]struct{}, len(inventory))
	for _, metric := range inventory {
		if metric.Name == "" || metric.Owner == "" || metric.Description == "" {
			return fmt.Errorf("metric %q missing required metadata", metric.Name)
		}
		if !strings.HasPrefix(metric.Name, "ec_") {
			return fmt.Errorf("metric %q missing ec_ prefix", metric.Name)
		}
		if _, ok := seen[metric.Name]; ok {
			return fmt.Errorf("duplicate metric %q", metric.Name)
		}
		seen[metric.Name] = struct{}{}
		if metric.Kind != Counter && metric.Kind != Gauge && metric.Kind != Histogram {
			return fmt.Errorf("metric %q has invalid kind %q", metric.Name, metric.Kind)
		}
		for _, label := range metric.Labels {
			if label.Name == "" || label.MaxCardinality <= 0 {
				return fmt.Errorf("metric %q has invalid label contract", metric.Name)
			}
			if label.Name == "tenant_id" {
				return fmt.Errorf("metric %q uses raw tenant_id in v7 spine", metric.Name)
			}
		}
	}
	return nil
}

func FindMetric(inventory []MetricContract, name string) (MetricContract, bool) {
	for _, metric := range inventory {
		if metric.Name == name {
			return metric, true
		}
	}
	return MetricContract{}, false
}

func SnapshotFromCapsule(c evomap.Capsule) DashboardSnapshot {
	k := c.KPIs
	return DashboardSnapshot{
		SchemaVersion: SchemaVersion,
		RecordedAt:    c.RecordedAt,
		EventAt:       c.EventAt,
		Binary:        c.Binary,
		Fields: []KPIField{
			{Name: "throughput_rps", Value: k.ThroughputRPS, Unit: "rps"},
			{Name: "p95_ms", Value: k.P95Ms, Unit: "ms"},
			{Name: "error_rate", Value: k.ErrorRate, Unit: "ratio"},
			{Name: "oom_alarms", Value: float64(k.OOMAlarms), Unit: "count"},
			{Name: "goroutine_count", Value: float64(k.GoroutineCount), Unit: "count"},
			{Name: "heap_in_use_bytes", Value: float64(k.HeapInUseBytes), Unit: "bytes"},
			{Name: "agentrace_available", Value: boolFloat(k.AgentraceAvailable), Unit: "bool"},
			{Name: "agentrace_session_duration_seconds", Value: k.AgentraceSessionDurationSec, Unit: "seconds"},
			{Name: "agentrace_tool_call_count", Value: float64(k.AgentraceToolCallCount), Unit: "count"},
			{Name: "agentrace_cost_usd", Value: k.AgentraceCostUSD, Unit: "usd"},
			{Name: "agentrace_bottleneck_count", Value: float64(k.AgentraceBottleneckCount), Unit: "count"},
			{Name: "agentrace_parallelism_efficiency", Value: k.AgentraceParallelismRatio, Unit: "ratio"},
			{Name: "uiauto_rate_limit_drops_total", Value: float64(k.UIAutoRateLimitDropsTotal), Unit: "count"},
			{Name: "workerpool_rejected_total", Value: float64(k.WorkerpoolRejectedTotal), Unit: "count"},
			{Name: "breaker_open_total", Value: float64(k.BreakerOpenTotal), Unit: "count"},
			{Name: "coord_conflicts_total", Value: float64(k.CoordConflictsTotal), Unit: "count"},
			{Name: "marketplace_sync_events_total", Value: float64(k.MarketplaceSyncEventsTotal), Unit: "count"},
			{Name: "marketplace_sync_dlq_total", Value: float64(k.MarketplaceSyncDLQTotal), Unit: "count"},
			{Name: "marketplace_replay_total", Value: float64(k.MarketplaceReplayTotal), Unit: "count"},
		},
	}
}

func ValidateDashboardSnapshot(s DashboardSnapshot) error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("snapshot schema %q", s.SchemaVersion)
	}
	if s.Binary == "" {
		return fmt.Errorf("snapshot binary required")
	}
	seen := make(map[string]struct{}, len(s.Fields))
	for _, field := range s.Fields {
		if field.Name == "" || field.Unit == "" {
			return fmt.Errorf("invalid KPI field %#v", field)
		}
		if _, ok := seen[field.Name]; ok {
			return fmt.Errorf("duplicate KPI field %q", field.Name)
		}
		seen[field.Name] = struct{}{}
	}
	return nil
}

// DecodeCapsuleSnapshots replays EvoMap NDJSON capsules into dashboard snapshots.
func DecodeCapsuleSnapshots(r io.Reader) ([]DashboardSnapshot, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var snapshots []DashboardSnapshot
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var capsule evomap.Capsule
		if err := json.Unmarshal([]byte(raw), &capsule); err != nil {
			return nil, fmt.Errorf("decode capsule line %d: %w", line, err)
		}
		snapshot := SnapshotFromCapsule(capsule)
		if err := ValidateDashboardSnapshot(snapshot); err != nil {
			return nil, fmt.Errorf("snapshot line %d: %w", line, err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan capsules: %w", err)
	}
	return snapshots, nil
}

// ValidateInventoryAgainstPrometheus checks that inventory metrics appear in Prometheus text.
func ValidateInventoryAgainstPrometheus(inventory []MetricContract, text string) error {
	seen := prometheusMetricNames(text)
	for _, metric := range inventory {
		if metricSeen(metric, seen) {
			continue
		}
		return fmt.Errorf("metric %q missing from prometheus text", metric.Name)
	}
	return nil
}

// ValidateDashboardFields checks that declared dashboard fields exist in a snapshot.
func ValidateDashboardFields(inventory []MetricContract, snapshot DashboardSnapshot) error {
	fields := make(map[string]struct{}, len(snapshot.Fields))
	for _, field := range snapshot.Fields {
		fields[field.Name] = struct{}{}
	}
	for _, metric := range inventory {
		for _, field := range metric.DashboardFields {
			if _, ok := fields[field]; !ok {
				return fmt.Errorf("dashboard field %q for metric %q missing from snapshot", field, metric.Name)
			}
		}
	}
	return nil
}

func AgentraceMetricSamples(k evomap.AgentraceKPIs) []MetricSample {
	if !k.Available {
		return nil
	}
	samples := []MetricSample{
		{Name: "ec_agentrace_bottlenecks_total", Labels: map[string]string{"severity": "all"}, Value: float64(k.BottleneckCount)},
		{Name: "ec_agentrace_cost_usd_total", Value: k.CostUSD},
		{Name: "ec_agentrace_parallelism_ratio", Value: k.ParallelismRatio},
		{Name: "ec_agentrace_session_duration_seconds", Value: k.SessionDurationSec},
	}
	tools := sortedKeys(k.ToolUsage)
	for _, tool := range tools {
		calls := k.ToolUsage[tool]
		errors := k.ToolErrors[tool]
		if errors > 0 {
			samples = append(samples, MetricSample{
				Name:   "ec_agentrace_tool_calls_total",
				Labels: map[string]string{"outcome": "error", "tool_name": tool},
				Value:  float64(errors),
			})
		}
		okCalls := calls - errors
		if okCalls > 0 {
			samples = append(samples, MetricSample{
				Name:   "ec_agentrace_tool_calls_total",
				Labels: map[string]string{"outcome": "ok", "tool_name": tool},
				Value:  float64(okCalls),
			})
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].Key() < samples[j].Key() })
	return samples
}

func ValidateMetricSample(sample MetricSample) error {
	if sample.Name == "" {
		return fmt.Errorf("sample name required")
	}
	if sample.Value < 0 {
		return fmt.Errorf("sample %s has negative value", sample.Name)
	}
	for k, v := range sample.Labels {
		if k == "" || v == "" {
			return fmt.Errorf("sample %s has empty label", sample.Name)
		}
		if k == "tenant_id" {
			return fmt.Errorf("sample %s uses raw tenant_id", sample.Name)
		}
	}
	return nil
}

func (s MetricSample) Key() string {
	if len(s.Labels) == 0 {
		return s.Name + "{}"
	}
	parts := make([]string, 0, len(s.Labels))
	for k, v := range s.Labels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return s.Name + "{" + strings.Join(parts, ",") + "}"
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func prometheusMetricNames(text string) map[string]struct{} {
	names := make(map[string]struct{})
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := line
		if idx := strings.IndexAny(name, "{ \t"); idx >= 0 {
			name = name[:idx]
		}
		if name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}

func metricSeen(metric MetricContract, seen map[string]struct{}) bool {
	if _, ok := seen[metric.Name]; ok {
		return true
	}
	if metric.Kind != Histogram {
		return false
	}
	for _, suffix := range []string{"_bucket", "_sum", "_count"} {
		if _, ok := seen[metric.Name+suffix]; ok {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
