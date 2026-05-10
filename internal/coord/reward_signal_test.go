package coord

import (
	"testing"
	"time"
)

func TestRewardSignal_FieldsPopulated(t *testing.T) {
	t.Parallel()
	signal := RewardSignal{
		AgentID:     "pricing",
		ActionID:    "act-001",
		TenantID:    "t1",
		SKU:         "sku-1",
		Outcome:     RewardOutcomeSuccess,
		RewardValue: 1.0,
		PolicyName:  "weighted_priority",
		Timestamp:   time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}

	if signal.AgentID != "pricing" {
		t.Fatalf("AgentID = %s, want pricing", signal.AgentID)
	}
	if signal.RewardValue != 1.0 {
		t.Fatalf("RewardValue = %f, want 1.0", signal.RewardValue)
	}
	if signal.Outcome != RewardOutcomeSuccess {
		t.Fatalf("Outcome = %s, want %s", signal.Outcome, RewardOutcomeSuccess)
	}
}

func TestNoopRewardEmitter_DoesNotError(t *testing.T) {
	t.Parallel()
	emitter := NoopRewardEmitter{}
	err := emitter.Emit(RewardSignal{
		AgentID:     "test",
		ActionID:    "act-001",
		RewardValue: 0.5,
		Timestamp:   time.Now(),
	})
	if err != nil {
		t.Fatalf("NoopRewardEmitter.Emit: %v", err)
	}
}
