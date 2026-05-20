package config_test

import (
	"sync"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/config"
)

func TestFlags_DefineAndEvaluateDefault(t *testing.T) {
	t.Parallel()
	fs := config.NewFlagStore()
	fs.Define("new_checkout", true)
	val, err := fs.Evaluate("new_checkout", "USER-1")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !val {
		t.Fatal("expected default true")
	}
}

func TestFlags_Rollout0PercentAllFalse(t *testing.T) {
	t.Parallel()
	fs := config.NewFlagStore()
	fs.Define("feature", true)
	fs.SetRollout("feature", 0)
	for i := 0; i < 20; i++ {
		v, _ := fs.Evaluate("feature", "USER-"+string(rune('A'+i)))
		if v {
			t.Fatalf("expected false for 0%% rollout")
		}
	}
}

func TestFlags_Rollout100PercentAllTrue(t *testing.T) {
	t.Parallel()
	fs := config.NewFlagStore()
	fs.Define("feature", false)
	fs.SetRollout("feature", 100)
	for i := 0; i < 20; i++ {
		v, _ := fs.Evaluate("feature", "USER-"+string(rune('A'+i)))
		if !v {
			t.Fatalf("expected true for 100%% rollout")
		}
	}
}

func TestFlags_Rollout50PercentDeterministic(t *testing.T) {
	t.Parallel()
	fs := config.NewFlagStore()
	fs.Define("feature", false)
	fs.SetRollout("feature", 50)
	v1, _ := fs.Evaluate("feature", "STABLE-USER")
	v2, _ := fs.Evaluate("feature", "STABLE-USER")
	if v1 != v2 {
		t.Fatal("expected deterministic assignment for same user")
	}
}

func TestFlags_TargetUserOverride(t *testing.T) {
	t.Parallel()
	fs := config.NewFlagStore()
	fs.Define("feature", false)
	fs.SetRollout("feature", 0)
	fs.TargetUser("feature", "BETA-USER", true)
	v, _ := fs.Evaluate("feature", "BETA-USER")
	if !v {
		t.Fatal("expected user override to enable feature")
	}
}

func TestFlags_UndefinedFlagError(t *testing.T) {
	t.Parallel()
	fs := config.NewFlagStore()
	if _, err := fs.Evaluate("noexist", "U1"); err == nil {
		t.Fatal("expected undefined flag error")
	}
}

func TestFlags_ConcurrentEvaluateSafety(t *testing.T) {
	t.Parallel()
	fs := config.NewFlagStore()
	fs.Define("concurrent", true)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fs.Evaluate("concurrent", "USER-"+string(rune('A'+i%26)))
		}(i)
	}
	wg.Wait()
}
