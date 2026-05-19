// Package creditnote provides credit note issuance, redemption, and expiry
// management for partial refunds and store credit.
package creditnote

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Credit note type constants.
const (
	TypePartialRefund = "PARTIAL_REFUND"
	TypeStoreCredit   = "STORE_CREDIT"
)

// Credit note status constants.
const (
	StatusActive  = "ACTIVE"
	StatusUsed    = "USED"
	StatusExpired = "EXPIRED"
)

// Sentinel errors.
var (
	ErrNotFound    = errors.New("creditnote: note not found")
	ErrNegAmount   = errors.New("creditnote: amount must be positive")
	ErrAlreadyUsed = errors.New("creditnote: note has already been used")
	ErrExpired     = errors.New("creditnote: note has expired")
)

// CreditNote represents an issued credit against an order or a store credit
// balance.
type CreditNote struct {
	ID         string
	OrderID    string
	CustomerID string
	Amount     float64
	Type       string
	Status     string
	IssuedAt   time.Time
	ExpiresAt  time.Time
	UsedAt     *time.Time
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// Store is a thread-safe in-memory repository for CreditNote values.
type Store struct {
	mu    sync.RWMutex
	notes map[string]CreditNote
}

// NewStore returns an initialised Store.
func NewStore() *Store {
	return &Store{notes: make(map[string]CreditNote)}
}

// Issue validates and inserts a new CreditNote.  It returns ErrNegAmount when
// note.Amount is not strictly positive.
func (s *Store) Issue(note CreditNote) error {
	if note.Amount <= 0 {
		return ErrNegAmount
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notes[note.ID] = note
	return nil
}

// Get returns a pointer to the stored CreditNote.  It returns ErrNotFound when
// the ID is absent.
func (s *Store) Get(id string) (*CreditNote, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.notes[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := n
	return &cp, nil
}

// ByCustomer returns all notes that belong to customerID in unspecified order.
func (s *Store) ByCustomer(customerID string) []CreditNote {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []CreditNote
	for _, n := range s.notes {
		if n.CustomerID == customerID {
			cp := n
			out = append(out, cp)
		}
	}
	return out
}

// update overwrites an existing note atomically.  Must be called with the
// write lock held.
func (s *Store) update(note CreditNote) {
	s.notes[note.ID] = note
}

// ---------------------------------------------------------------------------
// Redeemer
// ---------------------------------------------------------------------------

// Redeemer handles credit note redemption.
type Redeemer struct{}

// Redeem marks the note identified by noteID as StatusUsed at now, setting
// UsedAt accordingly.  It returns the note's Amount on success.
//
// Errors returned:
//   - ErrNotFound      -- noteID does not exist.
//   - ErrExpired       -- the note has passed its ExpiresAt timestamp.
//   - ErrAlreadyUsed   -- the note was already redeemed.
func (r *Redeemer) Redeem(store *Store, noteID string, now time.Time) (float64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	note, ok := store.notes[noteID]
	if !ok {
		return 0, ErrNotFound
	}
	if note.Status == StatusUsed {
		return 0, ErrAlreadyUsed
	}
	if note.Status == StatusExpired || !now.Before(note.ExpiresAt) {
		return 0, ErrExpired
	}

	t := now
	note.Status = StatusUsed
	note.UsedAt = &t
	store.update(note)
	return note.Amount, nil
}

// ---------------------------------------------------------------------------
// ExpiryChecker
// ---------------------------------------------------------------------------

// ExpiryChecker scans a Store for notes whose ExpiresAt has passed and
// transitions them to StatusExpired.
type ExpiryChecker struct{}

// ExpireStale iterates over all active notes in store and marks those with
// ExpiresAt <= now as StatusExpired.  It returns the count of notes that were
// transitioned.
func (e *ExpiryChecker) ExpireStale(store *Store, now time.Time) int {
	store.mu.Lock()
	defer store.mu.Unlock()

	count := 0
	for id, note := range store.notes {
		if note.Status == StatusActive && !now.Before(note.ExpiresAt) {
			note.Status = StatusExpired
			store.notes[id] = note
			count++
		}
	}
	return count
}

// String provides a human-readable description of a CreditNote for debugging.
func (n CreditNote) String() string {
	return fmt.Sprintf("CreditNote{ID:%s Type:%s Status:%s Amount:%.2f}", n.ID, n.Type, n.Status, n.Amount)
}
