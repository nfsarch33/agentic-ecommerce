package flashsale_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/flashsale"
)

var (
	epoch   = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	before  = epoch.Add(-1 * time.Hour)
	during  = epoch.Add(30 * time.Minute)
	after   = epoch.Add(2 * time.Hour)
)

func activeSale(id string, limit int) flashsale.Sale {
	return flashsale.Sale{
		ID:             id,
		ProductID:      "prod-1",
		DiscountPct:    25,
		InventoryLimit: limit,
		StartAt:        epoch,
		EndAt:          epoch.Add(1 * time.Hour),
	}
}

func TestManager_AddAndGetSale(t *testing.T) {
	t.Parallel()
	m := flashsale.NewManager()
	s := activeSale("s1", 10)
	m.AddSale(s)
	got, err := m.GetSale("s1")
	if err != nil {
		t.Fatalf("GetSale: %v", err)
	}
	if got.DiscountPct != 25 {
		t.Errorf("DiscountPct = %.0f, want 25", got.DiscountPct)
	}
}

func TestManager_GetSaleNotFound(t *testing.T) {
	t.Parallel()
	m := flashsale.NewManager()
	_, err := m.GetSale("nope")
	if err != flashsale.ErrSaleNotFound {
		t.Errorf("err = %v, want ErrSaleNotFound", err)
	}
}

func TestManager_ActiveSales(t *testing.T) {
	t.Parallel()
	m := flashsale.NewManager()
	m.AddSale(activeSale("active", 5))
	expired := flashsale.Sale{
		ID: "expired", ProductID: "p", DiscountPct: 10,
		StartAt: before.Add(-2 * time.Hour), EndAt: before,
	}
	m.AddSale(expired)

	got := m.ActiveSales(during)
	if len(got) != 1 || got[0].ID != "active" {
		t.Errorf("ActiveSales = %v, want [active]", got)
	}
}

func TestManager_ActiveSales_BeforeStart(t *testing.T) {
	t.Parallel()
	m := flashsale.NewManager()
	m.AddSale(activeSale("future", 5))
	got := m.ActiveSales(before)
	if len(got) != 0 {
		t.Errorf("expected 0 active sales before start, got %d", len(got))
	}
}

func TestReservationStore_ReserveWithinLimit(t *testing.T) {
	t.Parallel()
	m := flashsale.NewManager()
	m.AddSale(activeSale("s1", 3))
	rs := flashsale.NewReservationStore(m)

	for _, uid := range []string{"u1", "u2", "u3"} {
		if err := rs.Reserve("s1", uid, during); err != nil {
			t.Fatalf("Reserve(%s): %v", uid, err)
		}
	}
	if rs.CountReservations("s1") != 3 {
		t.Errorf("CountReservations = %d, want 3", rs.CountReservations("s1"))
	}
}

func TestReservationStore_OverReservationFails(t *testing.T) {
	t.Parallel()
	m := flashsale.NewManager()
	m.AddSale(activeSale("s1", 2))
	rs := flashsale.NewReservationStore(m)

	_ = rs.Reserve("s1", "u1", during)
	_ = rs.Reserve("s1", "u2", during)
	err := rs.Reserve("s1", "u3", during)
	if err != flashsale.ErrInventoryExhausted {
		t.Errorf("err = %v, want ErrInventoryExhausted", err)
	}
}

func TestReservationStore_CancelFreesSlot(t *testing.T) {
	t.Parallel()
	m := flashsale.NewManager()
	m.AddSale(activeSale("s1", 1))
	rs := flashsale.NewReservationStore(m)

	_ = rs.Reserve("s1", "u1", during)
	rs.Cancel("s1", "u1")

	if err := rs.Reserve("s1", "u2", during); err != nil {
		t.Errorf("Reserve after cancel: %v", err)
	}
}

func TestReservationStore_ExpiredSaleFails(t *testing.T) {
	t.Parallel()
	m := flashsale.NewManager()
	m.AddSale(activeSale("s1", 5))
	rs := flashsale.NewReservationStore(m)

	err := rs.Reserve("s1", "u1", after)
	if err != flashsale.ErrSaleNotActive {
		t.Errorf("err = %v, want ErrSaleNotActive", err)
	}
}

func TestReservationStore_NotStartedYetFails(t *testing.T) {
	t.Parallel()
	m := flashsale.NewManager()
	m.AddSale(activeSale("s1", 5))
	rs := flashsale.NewReservationStore(m)

	err := rs.Reserve("s1", "u1", before)
	if err != flashsale.ErrSaleNotActive {
		t.Errorf("err = %v, want ErrSaleNotActive", err)
	}
}

func TestSaleCountdown_Active(t *testing.T) {
	t.Parallel()
	s := activeSale("s", 1)
	remaining := flashsale.SaleCountdown(s, during)
	want := s.EndAt.Sub(during)
	if remaining != want {
		t.Errorf("SaleCountdown = %v, want %v", remaining, want)
	}
}

func TestSaleCountdown_Expired(t *testing.T) {
	t.Parallel()
	s := activeSale("s", 1)
	if d := flashsale.SaleCountdown(s, after); d != 0 {
		t.Errorf("SaleCountdown (expired) = %v, want 0", d)
	}
}

func TestSaleCountdown_ExactEnd(t *testing.T) {
	t.Parallel()
	s := activeSale("s", 1)
	if d := flashsale.SaleCountdown(s, s.EndAt); d != 0 {
		t.Errorf("SaleCountdown (at end) = %v, want 0", d)
	}
}
