package pricing_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/pricing"
)

var variants = []pricing.PriceVariant{
	{Name: "control", Price: 1000},
	{Name: "variant_a", Price: 900},
}

func TestABTest_CreateExperiment(t *testing.T) {
	t.Parallel()
	eng := pricing.NewABTestEngine()
	exp, err := eng.CreateExperiment("EXP-1", "Test", variants)
	if err != nil {
		t.Fatalf("CreateExperiment: %v", err)
	}
	if exp.ID != "EXP-1" {
		t.Fatalf("expected EXP-1, got %s", exp.ID)
	}
}

func TestABTest_AssignDeterministicVariant(t *testing.T) {
	t.Parallel()
	eng := pricing.NewABTestEngine()
	eng.CreateExperiment("EXP-2", "Test", variants)
	v1, _ := eng.AssignVariant("EXP-2", "USER-1")
	v2, _ := eng.AssignVariant("EXP-2", "USER-1")
	if v1.Name != v2.Name {
		t.Fatalf("expected deterministic assignment, got %s and %s", v1.Name, v2.Name)
	}
}

func TestABTest_SameUserSameVariant(t *testing.T) {
	t.Parallel()
	eng := pricing.NewABTestEngine()
	eng.CreateExperiment("EXP-3", "Test", variants)
	first, _ := eng.AssignVariant("EXP-3", "STABLE-USER")
	for i := 0; i < 10; i++ {
		v, _ := eng.AssignVariant("EXP-3", "STABLE-USER")
		if v.Name != first.Name {
			t.Fatalf("variant changed on iteration %d", i)
		}
	}
}

func TestABTest_TrackConversion(t *testing.T) {
	t.Parallel()
	eng := pricing.NewABTestEngine()
	eng.CreateExperiment("EXP-4", "Test", variants)
	if err := eng.TrackConversion("EXP-4", "USER-1", 500); err != nil {
		t.Fatalf("TrackConversion: %v", err)
	}
}

func TestABTest_DeclareWinnerByRate(t *testing.T) {
	t.Parallel()
	eng := pricing.NewABTestEngine()
	eng.CreateExperiment("EXP-5", "Test", variants)
	// Force all into known variants by using multiple users
	for i := 0; i < 20; i++ {
		eng.TrackConversion("EXP-5", "USER-A"+string(rune('A'+i)), 2000)
	}
	winner, err := eng.DeclareWinner("EXP-5")
	if err != nil {
		t.Fatalf("DeclareWinner: %v", err)
	}
	if winner.Name == "" {
		t.Fatal("expected a winner")
	}
}

func TestABTest_NoConversionsError(t *testing.T) {
	t.Parallel()
	eng := pricing.NewABTestEngine()
	eng.CreateExperiment("EXP-6", "Test", variants)
	if _, err := eng.DeclareWinner("EXP-6"); err == nil {
		t.Fatal("expected no conversions error")
	}
}

func TestABTest_ExperimentNotFoundError(t *testing.T) {
	t.Parallel()
	eng := pricing.NewABTestEngine()
	if _, err := eng.AssignVariant("NOEXIST", "U1"); err == nil {
		t.Fatal("expected experiment not found error")
	}
}
