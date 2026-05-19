// Package marketplaceseller manages marketplace seller onboarding, commission
// splits, and payout scheduling.
package marketplaceseller

import (
	"errors"
	"sync"
	"time"
)

const (
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusClosed    = "closed"
)

var (
	ErrSellerNotFound      = errors.New("seller not found")
	ErrInvalidCommission   = errors.New("commission rate must be between 0 and 1")
	ErrInvalidStatus       = errors.New("invalid seller status")
)

// Seller represents a marketplace seller account.
type Seller struct {
	ID             string
	Name           string
	Status         string
	CommissionRate float64
	JoinedAt       time.Time
	NextPayoutAt   time.Time
}

// Registry is a thread-safe store of Seller records.
type Registry struct {
	mu      sync.RWMutex
	sellers map[string]Seller
}

// NewRegistry returns an initialised Registry.
func NewRegistry() *Registry {
	return &Registry{sellers: make(map[string]Seller)}
}

// Onboard adds a new seller after validating the commission rate.
func (r *Registry) Onboard(seller Seller) error {
	if seller.CommissionRate < 0 || seller.CommissionRate > 1 {
		return ErrInvalidCommission
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sellers[seller.ID] = seller
	return nil
}

// Get returns a copy of the seller for the given ID.
func (r *Registry) Get(id string) (*Seller, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sellers[id]
	if !ok {
		return nil, ErrSellerNotFound
	}
	cp := s
	return &cp, nil
}

// UpdateStatus changes the status field of an existing seller.
func (r *Registry) UpdateStatus(id, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sellers[id]
	if !ok {
		return ErrSellerNotFound
	}
	s.Status = status
	r.sellers[id] = s
	return nil
}

// List returns a snapshot of all registered sellers.
func (r *Registry) List() []Seller {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Seller, 0, len(r.sellers))
	for _, s := range r.sellers {
		out = append(out, s)
	}
	return out
}

// PayoutSplit represents the financial split of a single transaction.
type PayoutSplit struct {
	SellerAmount   float64
	PlatformAmount float64
	Total          float64
}

// CalculateSplit divides total between seller and platform using the given
// commission rate (platform share = total * commissionRate).
func CalculateSplit(total float64, commissionRate float64) PayoutSplit {
	platform := total * commissionRate
	return PayoutSplit{
		SellerAmount:   total - platform,
		PlatformAmount: platform,
		Total:          total,
	}
}

// payoutEntry stores scheduled next-payout times per seller.
type payoutEntry struct {
	next time.Time
}

// PayoutScheduler tracks next payout times per seller.
type PayoutScheduler struct {
	mu      sync.RWMutex
	entries map[string]*payoutEntry
}

// NewPayoutScheduler returns an initialised PayoutScheduler.
func NewPayoutScheduler() *PayoutScheduler {
	return &PayoutScheduler{entries: make(map[string]*payoutEntry)}
}

// Schedule sets the next payout time for a seller to now() + interval.
func (p *PayoutScheduler) Schedule(sellerID string, interval time.Duration, now func() time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[sellerID] = &payoutEntry{next: now().Add(interval)}
}

// NextPayout returns the scheduled next payout time for a seller.
// Returns the zero time if no schedule exists.
func (p *PayoutScheduler) NextPayout(sellerID string) time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if e, ok := p.entries[sellerID]; ok {
		return e.next
	}
	return time.Time{}
}
