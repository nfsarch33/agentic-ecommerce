package loyalty

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrZeroAmount          = errors.New("amount must be greater than zero")
)

type pointEntry struct {
	amount   int
	source   string
	earnedAt time.Time
}

type PointsStore struct {
	mu      sync.RWMutex
	ledger  map[string][]pointEntry
	expiry  time.Duration
}

func NewPointsStore(expiry time.Duration) *PointsStore {
	return &PointsStore{
		ledger: make(map[string][]pointEntry),
		expiry: expiry,
	}
}

func (ps *PointsStore) EarnPoints(userID string, amount int, source string) error {
	if amount <= 0 {
		return ErrZeroAmount
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.ledger[userID] = append(ps.ledger[userID], pointEntry{amount: amount, source: source, earnedAt: time.Now()})
	return nil
}

func (ps *PointsStore) GetBalance(userID string) (int, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.sumActive(userID), nil
}

func (ps *PointsStore) RedeemPoints(userID string, amount int) error {
	if amount <= 0 {
		return ErrZeroAmount
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	bal := ps.sumActive(userID)
	if bal < amount {
		return ErrInsufficientBalance
	}
	// Record as negative entry
	ps.ledger[userID] = append(ps.ledger[userID], pointEntry{amount: -amount, source: "redemption", earnedAt: time.Now()})
	return nil
}

func (ps *PointsStore) ExpireStale(ctx interface{}) (int, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	expired := 0
	cutoff := time.Now().Add(-ps.expiry)
	for userID, entries := range ps.ledger {
		var kept []pointEntry
		for _, e := range entries {
			if e.amount > 0 && e.earnedAt.Before(cutoff) {
				expired += e.amount
			} else {
				kept = append(kept, e)
			}
		}
		ps.ledger[userID] = kept
	}
	return expired, nil
}

func (ps *PointsStore) sumActive(userID string) int {
	total := 0
	for _, e := range ps.ledger[userID] {
		total += e.amount
	}
	if total < 0 {
		return 0
	}
	return total
}
