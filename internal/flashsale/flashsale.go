// Package flashsale implements time-limited sale events with inventory
// reservation and countdown support.
package flashsale

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrSaleNotFound         = errors.New("flash sale not found")
	ErrSaleNotActive        = errors.New("flash sale is not active")
	ErrInventoryExhausted   = errors.New("flash sale inventory exhausted")
	ErrAlreadyReserved      = errors.New("user already has a reservation for this sale")
)

// Sale represents a time-limited discount event with a capped inventory.
type Sale struct {
	ID             string
	ProductID      string
	DiscountPct    float64
	InventoryLimit int
	StartAt        time.Time
	EndAt          time.Time
}

// isActive returns true if now falls within the sale window.
func (s Sale) isActive(now time.Time) bool {
	return !now.Before(s.StartAt) && now.Before(s.EndAt)
}

// Manager is a thread-safe store of flash sales.
type Manager struct {
	mu    sync.RWMutex
	sales map[string]Sale
}

// NewManager returns an initialised Manager.
func NewManager() *Manager {
	return &Manager{sales: make(map[string]Sale)}
}

// AddSale registers a new flash sale.
func (m *Manager) AddSale(sale Sale) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sales[sale.ID] = sale
}

// GetSale returns a copy of the sale for the given ID.
func (m *Manager) GetSale(id string) (*Sale, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sales[id]
	if !ok {
		return nil, ErrSaleNotFound
	}
	cp := s
	return &cp, nil
}

// ActiveSales returns all sales whose window includes now.
func (m *Manager) ActiveSales(now time.Time) []Sale {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Sale
	for _, s := range m.sales {
		if s.isActive(now) {
			out = append(out, s)
		}
	}
	return out
}

// Reservation records a single user's slot in a flash sale.
type Reservation struct {
	SaleID     string
	UserID     string
	ReservedAt time.Time
}

// saleReservations holds the reservation set for one sale.
type saleReservations struct {
	byUser map[string]Reservation
}

// ReservationStore is a thread-safe inventory reservation tracker.
type ReservationStore struct {
	mu      sync.RWMutex
	manager *Manager
	store   map[string]*saleReservations
}

// NewReservationStore returns an initialised ReservationStore backed by manager.
func NewReservationStore(manager *Manager) *ReservationStore {
	return &ReservationStore{
		manager: manager,
		store:   make(map[string]*saleReservations),
	}
}

func (rs *ReservationStore) getSaleReservations(saleID string) *saleReservations {
	sr, ok := rs.store[saleID]
	if !ok {
		sr = &saleReservations{byUser: make(map[string]Reservation)}
		rs.store[saleID] = sr
	}
	return sr
}

// Reserve creates a reservation for userID in the given sale.
// Fails if the sale is not active, the user already has a reservation,
// or the inventory limit has been reached.
func (rs *ReservationStore) Reserve(saleID, userID string, now time.Time) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	sale, err := rs.manager.GetSale(saleID)
	if err != nil {
		return err
	}
	if !sale.isActive(now) {
		return ErrSaleNotActive
	}

	sr := rs.getSaleReservations(saleID)
	if _, exists := sr.byUser[userID]; exists {
		return ErrAlreadyReserved
	}
	if len(sr.byUser) >= sale.InventoryLimit {
		return ErrInventoryExhausted
	}

	sr.byUser[userID] = Reservation{SaleID: saleID, UserID: userID, ReservedAt: now}
	return nil
}

// Cancel removes the reservation for userID in the given sale.
func (rs *ReservationStore) Cancel(saleID, userID string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if sr, ok := rs.store[saleID]; ok {
		delete(sr.byUser, userID)
	}
}

// CountReservations returns the number of active reservations for a sale.
func (rs *ReservationStore) CountReservations(saleID string) int {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	if sr, ok := rs.store[saleID]; ok {
		return len(sr.byUser)
	}
	return 0
}

// SaleCountdown returns the time remaining until the sale ends.
// Returns 0 if the sale has expired.
func SaleCountdown(sale Sale, now time.Time) time.Duration {
	if now.After(sale.EndAt) || now.Equal(sale.EndAt) {
		return 0
	}
	return sale.EndAt.Sub(now)
}
