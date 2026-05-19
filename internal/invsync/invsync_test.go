package invsync

import (
	"errors"
	"testing"
	"time"
)

func TestRegistry_SetGet(t *testing.T) {
	t.Parallel()
	reg := &Registry{}
	s := Stock{ProductID: "sku-1", WarehouseID: "wh-a", Available: 50, Reserved: 5}
	reg.SetStock(s)

	got, err := reg.GetStock("sku-1", "wh-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Available != 50 || got.Reserved != 5 {
		t.Errorf("expected Available=50 Reserved=5, got %+v", got)
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	t.Parallel()
	reg := &Registry{}
	_, err := reg.GetStock("nonexistent", "wh-x")
	if !errors.Is(err, ErrWarehouseNotFound) {
		t.Errorf("expected ErrWarehouseNotFound, got %v", err)
	}
}

func TestRegistry_TotalAvailable(t *testing.T) {
	t.Parallel()
	reg := &Registry{}
	reg.SetStock(Stock{ProductID: "sku-2", WarehouseID: "wh-a", Available: 30})
	reg.SetStock(Stock{ProductID: "sku-2", WarehouseID: "wh-b", Available: 20})
	reg.SetStock(Stock{ProductID: "other", WarehouseID: "wh-a", Available: 100})

	total := reg.TotalAvailable("sku-2")
	if total != 50 {
		t.Errorf("expected 50, got %d", total)
	}
}

func TestRegistry_TotalAvailable_Zero(t *testing.T) {
	t.Parallel()
	reg := &Registry{}
	if total := reg.TotalAvailable("missing"); total != 0 {
		t.Errorf("expected 0 for unknown product, got %d", total)
	}
}

func TestAllocator_Allocate(t *testing.T) {
	t.Parallel()
	reg := &Registry{}
	reg.SetStock(Stock{ProductID: "p1", WarehouseID: "w1", Available: 10, Reserved: 0})
	a := NewAllocator(reg)

	if err := a.Allocate("p1", "w1", 3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, _ := reg.GetStock("p1", "w1")
	if s.Available != 7 || s.Reserved != 3 {
		t.Errorf("after allocate: expected Available=7 Reserved=3, got %+v", s)
	}
}

func TestAllocator_Release(t *testing.T) {
	t.Parallel()
	reg := &Registry{}
	reg.SetStock(Stock{ProductID: "p1", WarehouseID: "w1", Available: 7, Reserved: 3})
	a := NewAllocator(reg)

	a.Release("p1", "w1", 3)
	s, _ := reg.GetStock("p1", "w1")
	if s.Available != 10 || s.Reserved != 0 {
		t.Errorf("after release: expected Available=10 Reserved=0, got %+v", s)
	}
}

func TestAllocator_OverAllocation(t *testing.T) {
	t.Parallel()
	reg := &Registry{}
	reg.SetStock(Stock{ProductID: "p2", WarehouseID: "w2", Available: 2})
	a := NewAllocator(reg)

	err := a.Allocate("p2", "w2", 5)
	if !errors.Is(err, ErrInsufficientStock) {
		t.Errorf("expected ErrInsufficientStock, got %v", err)
	}
}

func TestAllocator_AllocateNotFound(t *testing.T) {
	t.Parallel()
	reg := &Registry{}
	a := NewAllocator(reg)

	err := a.Allocate("ghost", "wh-none", 1)
	if !errors.Is(err, ErrWarehouseNotFound) {
		t.Errorf("expected ErrWarehouseNotFound, got %v", err)
	}
}

func TestAllocator_ReleaseNotFound(t *testing.T) {
	t.Parallel()
	// Release on a non-existent record should be a no-op (no panic).
	reg := &Registry{}
	a := NewAllocator(reg)
	a.Release("ghost", "wh-none", 5)
}

func TestTransferLog(t *testing.T) {
	t.Parallel()
	tl := &TransferLog{}
	now := time.Now()
	tl.Record(Transfer{FromWarehouse: "w1", ToWarehouse: "w2", ProductID: "sku-3", Quantity: 10, TransferredAt: now})
	tl.Record(Transfer{FromWarehouse: "w1", ToWarehouse: "w3", ProductID: "sku-3", Quantity: 5, TransferredAt: now})
	tl.Record(Transfer{FromWarehouse: "w1", ToWarehouse: "w2", ProductID: "other", Quantity: 1, TransferredAt: now})

	hist := tl.History("sku-3")
	if len(hist) != 2 {
		t.Fatalf("expected 2 transfers for sku-3, got %d", len(hist))
	}
	if hist[0].Quantity != 10 || hist[1].Quantity != 5 {
		t.Errorf("unexpected transfer history: %+v", hist)
	}
}

func TestTransferLog_EmptyHistory(t *testing.T) {
	t.Parallel()
	tl := &TransferLog{}
	hist := tl.History("sku-x")
	if len(hist) != 0 {
		t.Errorf("expected empty history, got %d entries", len(hist))
	}
}

func TestRegistry_SetStockOverwrite(t *testing.T) {
	t.Parallel()
	reg := &Registry{}
	reg.SetStock(Stock{ProductID: "x", WarehouseID: "w", Available: 5})
	reg.SetStock(Stock{ProductID: "x", WarehouseID: "w", Available: 99, Reserved: 2})

	s, err := reg.GetStock("x", "w")
	if err != nil {
		t.Fatal(err)
	}
	if s.Available != 99 || s.Reserved != 2 {
		t.Errorf("expected overwritten values, got %+v", s)
	}
}
