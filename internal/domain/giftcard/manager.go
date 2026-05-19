package giftcard

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
)

var (
	ErrNotFound         = errors.New("gift card not found")
	ErrNotActive        = errors.New("gift card not active")
	ErrInsufficientFunds = errors.New("insufficient gift card balance")
	ErrAlreadyActive    = errors.New("already active")
)

type GiftCardStatus string

const (
	StatusInactive GiftCardStatus = "inactive"
	StatusActive   GiftCardStatus = "active"
	StatusDepleted GiftCardStatus = "depleted"
)

type GiftCard struct {
	Code     string
	Value    int
	Balance  int
	Currency string
	OwnerID  string
	Status   GiftCardStatus
}

type Manager struct {
	mu    sync.RWMutex
	cards map[string]*GiftCard
}

func NewManager() *Manager {
	return &Manager{cards: make(map[string]*GiftCard)}
}

func (m *Manager) Create(value int, currency string) (GiftCard, error) {
	code, err := generateCode()
	if err != nil {
		return GiftCard{}, err
	}
	gc := &GiftCard{
		Code:     code,
		Value:    value,
		Balance:  value,
		Currency: currency,
		Status:   StatusInactive,
	}
	m.mu.Lock()
	m.cards[code] = gc
	m.mu.Unlock()
	return *gc, nil
}

func (m *Manager) Activate(code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	gc, ok := m.cards[code]
	if !ok {
		return ErrNotFound
	}
	if gc.Status == StatusActive {
		return ErrAlreadyActive
	}
	gc.Status = StatusActive
	return nil
}

func (m *Manager) Redeem(code string, amount int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	gc, ok := m.cards[code]
	if !ok {
		return 0, ErrNotFound
	}
	if gc.Status != StatusActive {
		return 0, ErrNotActive
	}
	if gc.Balance < amount {
		return 0, ErrInsufficientFunds
	}
	gc.Balance -= amount
	if gc.Balance == 0 {
		gc.Status = StatusDepleted
	}
	return gc.Balance, nil
}

func (m *Manager) CheckBalance(code string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	gc, ok := m.cards[code]
	if !ok {
		return 0, ErrNotFound
	}
	return gc.Balance, nil
}

func (m *Manager) Transfer(code, newOwnerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	gc, ok := m.cards[code]
	if !ok {
		return ErrNotFound
	}
	gc.OwnerID = newOwnerID
	return nil
}

func generateCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
