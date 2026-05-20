package ml_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/ml"
)

func TestPersonalize_ContentRankByPreference(t *testing.T) {
	t.Parallel()
	profile := ml.UserProfile{UserID: "U1", Prefs: map[string]float64{"electronics": 0.9, "clothing": 0.3}}
	items := []ml.Item{{ID: "P1", Category: "electronics"}, {ID: "P2", Category: "clothing"}}
	ranked := ml.ContentRank(profile, items)
	if ranked[0].Item.ID != "P1" {
		t.Fatalf("expected electronics first, got %s", ranked[0].Item.ID)
	}
}

func TestPersonalize_ABVariantDeterministic(t *testing.T) {
	t.Parallel()
	v1 := ml.ABVariant("USER-1", "exp-1")
	v2 := ml.ABVariant("USER-1", "exp-1")
	if v1 != v2 {
		t.Fatalf("expected deterministic variant, got %s and %s", v1, v2)
	}
}

func TestPersonalize_BanditExploresWithEpsilon(t *testing.T) {
	t.Parallel()
	arms := []ml.Arm{{Name: "A", Reward: 0, Pulls: 0}}
	result := ml.ContextualBandit(arms, ml.Context{UserID: "U1", Page: "home"})
	if result.Name != "A" {
		t.Fatalf("expected arm A, got %s", result.Name)
	}
}

func TestPersonalize_BanditExploitsBestArm(t *testing.T) {
	t.Parallel()
	arms := []ml.Arm{
		{Name: "low", Reward: 10, Pulls: 10},
		{Name: "high", Reward: 90, Pulls: 10},
	}
	// Most contexts should return "high"
	highCount := 0
	for _, uid := range []string{"U1", "U2", "U3", "U4", "U5", "U6", "U7", "U8"} {
		result := ml.ContextualBandit(arms, ml.Context{UserID: uid, Page: "page"})
		if result.Name == "high" {
			highCount++
		}
	}
	if highCount < 5 { // epsilon-greedy: ~90% should exploit
		t.Fatalf("expected mostly high arm, got %d/8", highCount)
	}
}

func TestPersonalize_EmptyProfileReturnsDefaultOrder(t *testing.T) {
	t.Parallel()
	profile := ml.UserProfile{UserID: "U2"}
	items := []ml.Item{{ID: "P1", Category: "electronics"}}
	ranked := ml.ContentRank(profile, items)
	if len(ranked) != 1 {
		t.Fatalf("expected 1 result, got %d", len(ranked))
	}
}

func TestPersonalize_ConcurrentRankingSafety(t *testing.T) {
	t.Parallel()
	profile := ml.UserProfile{UserID: "U3", Prefs: map[string]float64{"cat": 0.5}}
	items := []ml.Item{{ID: "P1", Category: "cat"}}
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			ml.ContentRank(profile, items)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}
