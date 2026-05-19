package invsync

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// WarehouseID is a type alias for string identifying a warehouse.
type WarehouseID = string

// Sentinel errors.
var (
	ErrInsufficientStock = errors.New("invsync: insufficient stock")
	ErrWarehouseNotFound = errors.New("invsync: warehouse not found")
)

// Stock represents the inventory of a product at a specific warehouse.
type Stock struct {
	ProductID   string
	WarehouseID string
	Available   int
	Reserved    int
}

// Registry is a thread-safe store of Stock records keyed by (productID, warehouseID).
type Registry struct {
	mu    sync.RWMutex
	items map[string]*Stock // key: productID+":"+warehouseID
}

func stockKey(productID, warehouseID string) string {
	return productID + ":" + warehouseID
}

// SetStock inserts or replaces the stock record for the given product/warehouse pair.
func (r *Registry) SetStock(s Stock) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.items == nil {
		r.items = make(map[string]*Stock)
	}
	cp := s
	r.items[stockKey(s.ProductID, s.WarehouseID)] = &cp
}

// GetStock retrieves the stock record for the given product/warehouse pair.
// Returns ErrWarehouseNotFound if no record exists.
func (r *Registry) GetStock(productID, warehouseID string) (*Stock, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.items[stockKey(productID, warehouseID)]
	if !ok {
		return nil, fmt.Errorf("%w: product=%s warehouse=%s", ErrWarehouseNotFound, productID, warehouseID)
	}
	cp := *s
	return &cp, nil
}

// TotalAvailable returns the sum of Available units across all warehouses for a product.
func (r *Registry) TotalAvailable(productID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var total int
	for _, s := range r.items {
		if s.ProductID == productID {
			total += s.Available
		}
	}
	return total
}

// Allocator performs allocation and release operations against a Registry.
type Allocator struct {
	reg *Registry
}

// NewAllocator returns an Allocator backed by the given Registry.
func NewAllocator(reg *Registry) *Allocator {
	return &Allocator{reg: reg}
}

// Allocate reduces Available and increases Reserved by qty.
// Returns ErrInsufficientStock if Available < qty, or ErrWarehouseNotFound if the record is absent.
func (a *Allocator) Allocate(productID, warehouseID string, qty int) error {
	a.reg.mu.Lock()
	defer a.reg.mu.Unlock()
	s, ok := a.reg.items[stockKey(productID, warehouseID)]
	if !ok {
		return fmt.Errorf("%w: product=%s warehouse=%s", ErrWarehouseNotFound, productID, warehouseID)
	}
	if s.Available < qty {
		return fmt.Errorf("%w: available=%d requested=%d", ErrInsufficientStock, s.Available, qty)
	}
	s.Available -= qty
	s.Reserved += qty
	return nil
}

// Release increases Available and decreases Reserved by qty (no-op if record missing).
func (a *Allocator) Release(productID, warehouseID string, qty int) {
	a.reg.mu.Lock()
	defer a.reg.mu.Unlock()
	s, ok := a.reg.items[stockKey(productID, warehouseID)]
	if !ok {
		return
	}
	s.Available += qty
	if s.Reserved >= qty {
		s.Reserved -= qty
	} else {
		s.Reserved = 0
	}
}

// Transfer records a stock movement between two warehouses.
type Transfer struct {
	FromWarehouse string
	ToWarehouse   string
	ProductID     string
	Quantity      int
	TransferredAt time.Time
}

// TransferLog is a thread-safe append-only log of Transfer records.
type TransferLog struct {
	mu      sync.RWMutex
	records []Transfer
}

// Record appends a Transfer to the log.
func (tl *TransferLog) Record(t Transfer) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.records = append(tl.records, t)
}

// History returns all transfers for the given productID in insertion order.
func (tl *TransferLog) History(productID string) []Transfer {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	var out []Transfer
	for _, t := range tl.records {
		if t.ProductID == productID {
			out = append(out, t)
		}
	}
	return out
}
