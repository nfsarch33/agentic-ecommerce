package marketplaceseller_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/marketplaceseller"
)

func baseSeller(id string) marketplaceseller.Seller {
	return marketplaceseller.Seller{
		ID:             id,
		Name:           "Acme Store",
		Status:         marketplaceseller.StatusPending,
		CommissionRate: 0.15,
		JoinedAt:       time.Now(),
	}
}

func TestRegistry_OnboardAndGet(t *testing.T) {
	t.Parallel()
	r := marketplaceseller.NewRegistry()
	s := baseSeller("s1")
	if err := r.Onboard(s); err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	got, err := r.Get("s1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Acme Store" {
		t.Errorf("Name = %q, want %q", got.Name, "Acme Store")
	}
}

func TestRegistry_OnboardInvalidRate(t *testing.T) {
	t.Parallel()
	r := marketplaceseller.NewRegistry()

	for _, rate := range []float64{-0.01, 1.01} {
		s := baseSeller("bad")
		s.CommissionRate = rate
		if err := r.Onboard(s); err != marketplaceseller.ErrInvalidCommission {
			t.Errorf("rate %.2f: err = %v, want ErrInvalidCommission", rate, err)
		}
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	t.Parallel()
	r := marketplaceseller.NewRegistry()
	_, err := r.Get("missing")
	if err != marketplaceseller.ErrSellerNotFound {
		t.Errorf("err = %v, want ErrSellerNotFound", err)
	}
}

func TestRegistry_UpdateStatus(t *testing.T) {
	t.Parallel()
	r := marketplaceseller.NewRegistry()
	_ = r.Onboard(baseSeller("s2"))
	if err := r.UpdateStatus("s2", marketplaceseller.StatusActive); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := r.Get("s2")
	if got.Status != marketplaceseller.StatusActive {
		t.Errorf("Status = %q, want %q", got.Status, marketplaceseller.StatusActive)
	}
}

func TestRegistry_StatusTransitions(t *testing.T) {
	t.Parallel()
	r := marketplaceseller.NewRegistry()
	_ = r.Onboard(baseSeller("s3"))

	transitions := []string{
		marketplaceseller.StatusActive,
		marketplaceseller.StatusSuspended,
		marketplaceseller.StatusClosed,
	}
	for _, status := range transitions {
		if err := r.UpdateStatus("s3", status); err != nil {
			t.Fatalf("UpdateStatus to %q: %v", status, err)
		}
		got, _ := r.Get("s3")
		if got.Status != status {
			t.Errorf("Status = %q, want %q", got.Status, status)
		}
	}
}

func TestRegistry_List(t *testing.T) {
	t.Parallel()
	r := marketplaceseller.NewRegistry()
	_ = r.Onboard(baseSeller("a"))
	_ = r.Onboard(baseSeller("b"))
	if len(r.List()) != 2 {
		t.Errorf("List len = %d, want 2", len(r.List()))
	}
}

func TestCalculateSplit(t *testing.T) {
	t.Parallel()
	split := marketplaceseller.CalculateSplit(100.00, 0.20)
	if split.PlatformAmount != 20.00 {
		t.Errorf("PlatformAmount = %.2f, want 20.00", split.PlatformAmount)
	}
	if split.SellerAmount != 80.00 {
		t.Errorf("SellerAmount = %.2f, want 80.00", split.SellerAmount)
	}
	if split.Total != 100.00 {
		t.Errorf("Total = %.2f, want 100.00", split.Total)
	}
}

func TestCalculateSplit_ZeroCommission(t *testing.T) {
	t.Parallel()
	split := marketplaceseller.CalculateSplit(50.00, 0)
	if split.SellerAmount != 50.00 {
		t.Errorf("SellerAmount = %.2f, want 50.00", split.SellerAmount)
	}
	if split.PlatformAmount != 0 {
		t.Errorf("PlatformAmount = %.2f, want 0", split.PlatformAmount)
	}
}

func TestPayoutScheduler(t *testing.T) {
	t.Parallel()
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }

	ps := marketplaceseller.NewPayoutScheduler()
	ps.Schedule("s1", 7*24*time.Hour, now)

	want := fixed.Add(7 * 24 * time.Hour)
	got := ps.NextPayout("s1")
	if !got.Equal(want) {
		t.Errorf("NextPayout = %v, want %v", got, want)
	}
}

func TestPayoutScheduler_UnknownSellerReturnsZero(t *testing.T) {
	t.Parallel()
	ps := marketplaceseller.NewPayoutScheduler()
	if !ps.NextPayout("nobody").IsZero() {
		t.Error("expected zero time for unknown seller")
	}
}
