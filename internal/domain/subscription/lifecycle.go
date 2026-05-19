package subscription

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidTransition = errors.New("invalid subscription state transition")
	ErrNotFound          = errors.New("subscription not found")
	ErrInvalidPlan       = errors.New("plan ID must not be empty")
)

type State string

const (
	StatePending   State = "pending"
	StateActive    State = "active"
	StatePaused    State = "paused"
	StateCancelled State = "cancelled"
)

// Subscription is the aggregate root for subscription lifecycle management.
type Subscription struct {
	ID           string
	UserID       string
	PlanID       string
	State        State
	CreatedAt    time.Time
	LastBilledAt time.Time
	CancelledAt  time.Time
}

// SubscriptionManager creates and transitions subscriptions.
type SubscriptionManager struct {
	mu   sync.RWMutex
	subs map[string]Subscription
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{subs: make(map[string]Subscription)}
}

func (m *SubscriptionManager) Create(userID, planID string) (Subscription, error) {
	if planID == "" {
		return Subscription{}, ErrInvalidPlan
	}
	sub := Subscription{
		ID:        uuid.New().String(),
		UserID:    userID,
		PlanID:    planID,
		State:     StatePending,
		CreatedAt: time.Now().UTC(),
	}
	m.mu.Lock()
	m.subs[sub.ID] = sub
	m.mu.Unlock()
	return sub, nil
}

func (m *SubscriptionManager) Activate(id string) (Subscription, error) {
	return m.transition(id, StateActive, StatePending)
}

func (m *SubscriptionManager) Pause(id string) (Subscription, error) {
	return m.transition(id, StatePaused, StateActive)
}

func (m *SubscriptionManager) Resume(id string) (Subscription, error) {
	return m.transition(id, StateActive, StatePaused)
}

func (m *SubscriptionManager) Cancel(id string) (Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sub, ok := m.subs[id]
	if !ok {
		return Subscription{}, ErrNotFound
	}
	if sub.State == StateCancelled {
		return sub, nil
	}
	if sub.State == StatePending || sub.State == StateActive || sub.State == StatePaused {
		sub.State = StateCancelled
		sub.CancelledAt = time.Now().UTC()
		m.subs[id] = sub
		return sub, nil
	}
	return Subscription{}, ErrInvalidTransition
}

func (m *SubscriptionManager) Get(id string) (Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sub, ok := m.subs[id]
	if !ok {
		return Subscription{}, ErrNotFound
	}
	return sub, nil
}

func (m *SubscriptionManager) transition(id string, to State, allowedFrom ...State) (Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sub, ok := m.subs[id]
	if !ok {
		return Subscription{}, ErrNotFound
	}
	for _, from := range allowedFrom {
		if sub.State == from {
			sub.State = to
			m.subs[id] = sub
			return sub, nil
		}
	}
	return Subscription{}, ErrInvalidTransition
}

// BillingCycle computes billing dates and proration credits.
type BillingCycle struct {
	period time.Duration
}

func NewBillingCycle(period time.Duration) *BillingCycle {
	return &BillingCycle{period: period}
}

func (bc *BillingCycle) NextBillingDate(sub Subscription) time.Time {
	return sub.LastBilledAt.Add(bc.period)
}

// ProrationCredit returns the unused portion of cycleAmount when a subscription
// is cancelled between start and end of a billing period.
func (bc *BillingCycle) ProrationCredit(cycleAmount int, start, end time.Time) int {
	totalSeconds := bc.period.Seconds()
	usedSeconds := end.Sub(start).Seconds()
	remaining := totalSeconds - usedSeconds
	if remaining <= 0 {
		return 0
	}
	return int(float64(cycleAmount) * remaining / totalSeconds)
}
