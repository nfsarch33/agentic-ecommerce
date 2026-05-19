package featureflag

import (
	"fmt"
	"testing"
)

func TestStore_SetGetDeleteList(t *testing.T) {
	t.Parallel()

	s := NewStore()
	f := Flag{Key: "my-flag", Description: "test", Enabled: true, Rollout: 1.0}
	s.Set(f)

	got, err := s.Get("my-flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Key != "my-flag" {
		t.Errorf("Key = %q, want %q", got.Key, "my-flag")
	}

	list := s.List()
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	s.Delete("my-flag")
	if _, err := s.Get("my-flag"); err == nil {
		t.Error("expected error after delete")
	}
}

func TestEvaluator_KillSwitch(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Set(Flag{Key: "k", Enabled: true, Rollout: 1.0, KillSwitch: true})

	ev := Evaluator{}
	if ev.IsEnabled(s, "k", "user1", nil) {
		t.Error("kill switch should always return false")
	}
}

func TestEvaluator_Disabled(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Set(Flag{Key: "d", Enabled: false, Rollout: 1.0})

	ev := Evaluator{}
	if ev.IsEnabled(s, "d", "user1", nil) {
		t.Error("disabled flag should return false")
	}
}

func TestEvaluator_RolloutDistribution(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Set(Flag{Key: "rollout-flag", Enabled: true, Rollout: 0.5})

	ev := Evaluator{}
	enabled := 0
	total := 1000
	for i := 0; i < total; i++ {
		userID := fmt.Sprintf("user-%d", i)
		if ev.IsEnabled(s, "rollout-flag", userID, nil) {
			enabled++
		}
	}

	// Expect ~50% ±5% (i.e., 450-550).
	pct := float64(enabled) / float64(total)
	if pct < 0.45 || pct > 0.55 {
		t.Errorf("rollout distribution = %.2f%%, want 45-55%%", pct*100)
	}
}

func TestEvaluator_UserIDRule(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Set(Flag{
		Key:     "targeted",
		Enabled: true,
		Rollout: 0.0,
		Rules:   []Rule{{Type: RuleTypeUserID, Value: "alice"}},
	})

	ev := Evaluator{}
	if !ev.IsEnabled(s, "targeted", "alice", nil) {
		t.Error("alice should be enabled by user ID rule")
	}
	if ev.IsEnabled(s, "targeted", "bob", nil) {
		t.Error("bob should NOT be enabled")
	}
}

func TestEvaluator_AttributeRule(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Set(Flag{
		Key:     "attr-flag",
		Enabled: true,
		Rollout: 0.0,
		Rules:   []Rule{{Type: RuleTypeAttribute, Value: "plan=premium"}},
	})

	ev := Evaluator{}
	premiumAttrs := map[string]string{"plan": "premium"}
	freeAttrs := map[string]string{"plan": "free"}

	if !ev.IsEnabled(s, "attr-flag", "u1", premiumAttrs) {
		t.Error("premium user should be enabled")
	}
	if ev.IsEnabled(s, "attr-flag", "u2", freeAttrs) {
		t.Error("free user should NOT be enabled")
	}
}

func TestEvaluator_UnknownFlag(t *testing.T) {
	t.Parallel()

	s := NewStore()
	ev := Evaluator{}
	if ev.IsEnabled(s, "nonexistent", "user", nil) {
		t.Error("unknown flag should return false")
	}
}
