package observability

import (
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

// MinimaxMetrics is the v4.13.0 typed facade for MiniMax quota-aware
// adapter observability. Satisfies the metrics port consumed by
// MinimaxAdapter.OnRequest and OnFailover callbacks.
//
// Cardinality budget (~50 series):
//
//	ec_minimax_requests_total{key_alias, status}          ~ 2*4 = 8 series
//	ec_minimax_request_duration_seconds{key_alias}        ~ 2*10 = 20 series
//	ec_minimax_active_key gauge                           ~ 2 series
//	ec_minimax_key_cooldown_remaining_seconds{key_alias}  ~ 2*2 = 4 series
//	ec_minimax_failover_events_total{from_key, to_key}    ~ 2*2 = 4 series
//	Total ~ 38 series. Plan estimate ~50 with headroom.
type MinimaxMetrics struct {
	registry *metrics.Registry
}

// NewMinimaxMetrics binds to the supplied registry.
func NewMinimaxMetrics(registry *metrics.Registry) *MinimaxMetrics {
	return &MinimaxMetrics{registry: registry}
}

// RecordRequest increments the request counter and observes latency.
func (m *MinimaxMetrics) RecordRequest(keyAlias, status string, durationSec float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.MinimaxRequestsTotal.Inc(metrics.Labels{
		"key_alias": keyAlias,
		"status":    status,
	})
	m.registry.MinimaxRequestDuration.Observe(durationSec, metrics.Labels{
		"key_alias": keyAlias,
	})
}

// RecordFailover increments the failover event counter.
func (m *MinimaxMetrics) RecordFailover(fromKey, toKey string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.MinimaxFailoverEventsTotal.Inc(metrics.Labels{
		"from_key": fromKey,
		"to_key":   toKey,
	})
}

// SetActiveKey updates the active key gauge.
func (m *MinimaxMetrics) SetActiveKey(keyAlias string) {
	if m == nil || m.registry == nil {
		return
	}
	for _, alias := range []string{"1", "2"} {
		val := float64(0)
		if alias == keyAlias {
			val = 1
		}
		m.registry.MinimaxActiveKey.Set(val, metrics.Labels{
			"key_alias": alias,
		})
	}
}

// SetCooldownRemaining updates the cooldown gauge for a key.
func (m *MinimaxMetrics) SetCooldownRemaining(keyAlias string, seconds float64) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.MinimaxKeyCooldownRemaining.Set(seconds, metrics.Labels{
		"key_alias": keyAlias,
	})
}
