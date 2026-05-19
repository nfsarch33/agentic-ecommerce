// Package affiliate provides referral code tracking, commission calculation,
// and payout ledger management for an ecommerce affiliate programme.
package affiliate

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrCodeNotFound = errors.New("affiliate code not found")
	ErrInactiveCode = errors.New("affiliate code is inactive")
)

// Code represents an affiliate referral code.
type Code struct {
	ID             string
	OwnerID        string
	Slug           string
	DiscountPct    float64
	CommissionRate float64
	Active         bool
	CreatedAt      time.Time
}

// Registry is a thread-safe store of affiliate codes keyed by slug.
type Registry struct {
	mu    sync.RWMutex
	codes map[string]Code
}

// NewRegistry returns an initialised Registry.
func NewRegistry() *Registry {
	return &Registry{codes: make(map[string]Code)}
}

// Register adds or replaces a code in the registry.
func (r *Registry) Register(code Code) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.codes[code.Slug] = code
}

// Lookup returns a pointer to the code for the given slug.
// Returns ErrCodeNotFound if not present, ErrInactiveCode if disabled.
func (r *Registry) Lookup(slug string) (*Code, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.codes[slug]
	if !ok {
		return nil, ErrCodeNotFound
	}
	if !c.Active {
		return nil, ErrInactiveCode
	}
	cp := c
	return &cp, nil
}

// List returns a snapshot of all registered codes.
func (r *Registry) List() []Code {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Code, 0, len(r.codes))
	for _, c := range r.codes {
		out = append(out, c)
	}
	return out
}

// Sale records a single transaction attributed to an affiliate code.
type Sale struct {
	SaleID           string
	CodeID           string
	Amount           float64
	CommissionEarned float64
	OccurredAt       time.Time
}

// ledgerEntry tracks pending balance per owner.
type ledgerEntry struct {
	pending float64
}

// Ledger is a thread-safe accounting store for affiliate commissions.
type Ledger struct {
	mu      sync.RWMutex
	entries map[string]*ledgerEntry
}

// NewLedger returns an initialised Ledger.
func NewLedger() *Ledger {
	return &Ledger{entries: make(map[string]*ledgerEntry)}
}

// RecordSale adds the commission earned from a sale to the owner's balance.
// The ownerID is derived from the CodeID field; callers must populate it
// by looking up the code before recording.
func (l *Ledger) RecordSale(sale Sale) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[sale.CodeID]
	if !ok {
		e = &ledgerEntry{}
		l.entries[sale.CodeID] = e
	}
	e.pending += sale.CommissionEarned
}

// PendingPayout returns the unpaid commission balance for the given ownerID.
func (l *Ledger) PendingPayout(ownerID string) float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if e, ok := l.entries[ownerID]; ok {
		return e.pending
	}
	return 0
}

// MarkPaid zeroes the pending balance for the given ownerID.
func (l *Ledger) MarkPaid(ownerID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.entries[ownerID]; ok {
		e.pending = 0
	}
}
