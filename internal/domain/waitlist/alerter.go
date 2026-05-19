package waitlist

import (
	"sync"
	"time"
)

type subscriber struct {
	userID      string
	subscribedAt time.Time
}

type Alerter struct {
	mu    sync.RWMutex
	queues map[string][]subscriber
}

func NewAlerter() *Alerter {
	return &Alerter{queues: make(map[string][]subscriber)}
}

func (a *Alerter) Subscribe(userID, productID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.queues[productID] {
		if s.userID == userID {
			return nil // idempotent
		}
	}
	a.queues[productID] = append(a.queues[productID], subscriber{userID: userID, subscribedAt: time.Now()})
	return nil
}

func (a *Alerter) Unsubscribe(userID, productID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	subs := a.queues[productID]
	var kept []subscriber
	for _, s := range subs {
		if s.userID != userID {
			kept = append(kept, s)
		}
	}
	a.queues[productID] = kept
	return nil
}

func (a *Alerter) NotifyInStock(productID string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	subs := a.queues[productID]
	var notified []string
	for _, s := range subs {
		notified = append(notified, s.userID)
	}
	a.queues[productID] = nil // clear after notifying
	return notified, nil
}
