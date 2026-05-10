// Package coord -- v4.7.0 RewardSignal struct for MADRL feedback.
//
// A RewardSignal is emitted after each coordinated action completes,
// capturing the outcome and a scalar reward value. The signal feeds
// future RL training pipelines and is persisted to the
// CoordinationLog for offline analysis.
//
// Pure value type; no IO. The emitter lives in the coordinator.
package coord

import "time"

// RewardSignal captures the outcome of a coordinated action for
// future RL training. Emitted after each coordination decision
// completes its downstream effect.
type RewardSignal struct {
	AgentID     string    `json:"agent_id"`
	ActionID    string    `json:"action_id"`
	TenantID    string    `json:"tenant_id"`
	SKU         string    `json:"sku"`
	Outcome     string    `json:"outcome"`
	RewardValue float64   `json:"reward_value"`
	PolicyName  string    `json:"policy_name"`
	Timestamp   time.Time `json:"timestamp"`
}

// RewardOutcome enumerates the outcome categories for reward
// signals. The scalar reward_value is computed by the caller;
// the outcome string provides the categorical label.
const (
	RewardOutcomeSuccess       = "success"
	RewardOutcomePartial       = "partial"
	RewardOutcomeConflict      = "conflict_resolved"
	RewardOutcomeConstraintHit = "constraint_override"
)

// RewardEmitter is the port through which reward signals are
// published. The coordinator calls Emit after each coordinated
// action completes. Implementations may write to the
// CoordinationLog, Prometheus, or an RL training buffer.
type RewardEmitter interface {
	Emit(signal RewardSignal) error
}

// NoopRewardEmitter discards all signals. Used in tests and when
// RL training is not yet wired.
type NoopRewardEmitter struct{}

// Emit satisfies RewardEmitter by discarding the signal.
func (NoopRewardEmitter) Emit(_ RewardSignal) error { return nil }
