package marketplacesync

import (
	"strings"

	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

type RegistryMetrics struct {
	registry *metrics.Registry
}

func NewRegistryMetrics(registry *metrics.Registry) *RegistryMetrics {
	return &RegistryMetrics{registry: registry}
}

func (m *RegistryMetrics) RecordSyncEvent(event ProductEvent, status SyncStatus) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.MarketplaceSyncEventsTotal.Inc(syncLabels(event, string(status)))
}

func (m *RegistryMetrics) RecordDLQ(record DLQRecord) {
	if m == nil || m.registry == nil {
		return
	}
	labels := baseLabels(record.Event)
	labels["reason"] = boundedReason(record.Reason)
	m.registry.MarketplaceSyncDLQTotal.Inc(labels)
}

func (m *RegistryMetrics) RecordReplay(record DLQRecord, status SyncStatus) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.MarketplaceReplayTotal.Inc(syncLabels(record.Event, string(status)))
}

func syncLabels(event ProductEvent, status string) metrics.Labels {
	labels := baseLabels(event)
	labels["status"] = status
	return labels
}

func baseLabels(event ProductEvent) metrics.Labels {
	return metrics.Labels{
		"provider":    event.Provider,
		"entity_type": string(event.EntityType),
	}
}

func boundedReason(reason string) string {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case normalized == "":
		return "unknown"
	case strings.Contains(normalized, "transient"):
		return "transient"
	case strings.Contains(normalized, "timeout") || strings.Contains(normalized, "deadline"):
		return "timeout"
	case strings.Contains(normalized, "validation") || strings.Contains(normalized, "invalid"):
		return "validation"
	case strings.Contains(normalized, "conflict"):
		return "conflict"
	case strings.Contains(normalized, "permanent"):
		return "permanent"
	default:
		return "unknown"
	}
}
