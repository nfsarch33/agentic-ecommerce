package loyalty_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/domain/loyalty"
)

func TestLoyalty_EarnAndCheckBalance(t *testing.T) {
	t.Parallel()
	ps := loyalty.NewPointsStore(365 * 24 * time.Hour)
	ps.EarnPoints("U1", 100, "purchase")
	bal, _ := ps.GetBalance("U1")
	if bal != 100 {
		t.Fatalf("expected 100, got %d", bal)
	}
}

func TestLoyalty_RedeemSuccess(t *testing.T) {
	t.Parallel()
	ps := loyalty.NewPointsStore(365 * 24 * time.Hour)
	ps.EarnPoints("U2", 200, "purchase")
	if err := ps.RedeemPoints("U2", 50); err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	bal, _ := ps.GetBalance("U2")
	if bal != 150 {
		t.Fatalf("expected 150, got %d", bal)
	}
}

func TestLoyalty_RedeemInsufficient(t *testing.T) {
	t.Parallel()
	ps := loyalty.NewPointsStore(365 * 24 * time.Hour)
	ps.EarnPoints("U3", 10, "purchase")
	if err := ps.RedeemPoints("U3", 100); err == nil {
		t.Fatal("expected insufficient balance error")
	}
}

func TestLoyalty_ExpiryRemovesOldPoints(t *testing.T) {
	t.Parallel()
	ps := loyalty.NewPointsStore(time.Millisecond)
	ps.EarnPoints("U4", 100, "purchase")
	time.Sleep(5 * time.Millisecond)
	expired, _ := ps.ExpireStale(nil)
	if expired != 100 {
		t.Fatalf("expected 100 expired, got %d", expired)
	}
	bal, _ := ps.GetBalance("U4")
	if bal != 0 {
		t.Fatalf("expected 0 after expiry, got %d", bal)
	}
}

func TestLoyalty_EarnFromMultipleSources(t *testing.T) {
	t.Parallel()
	ps := loyalty.NewPointsStore(365 * 24 * time.Hour)
	ps.EarnPoints("U5", 50, "purchase")
	ps.EarnPoints("U5", 30, "referral")
	bal, _ := ps.GetBalance("U5")
	if bal != 80 {
		t.Fatalf("expected 80, got %d", bal)
	}
}

func TestLoyalty_ZeroAmountRejection(t *testing.T) {
	t.Parallel()
	ps := loyalty.NewPointsStore(365 * 24 * time.Hour)
	if err := ps.EarnPoints("U6", 0, "purchase"); err == nil {
		t.Fatal("expected error for zero amount")
	}
}
