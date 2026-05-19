// Package preorder manages pre-order placement, fulfillment queuing, and
// customer notification dispatch.
package preorder

import (
	"errors"
	"sync"
	"time"
)

const (
	StatusOpen      = "open"
	StatusConfirmed = "confirmed"
	StatusFulfilled = "fulfilled"
	StatusCancelled = "cancelled"
)

var (
	ErrPreOrderNotFound = errors.New("pre-order not found")
	ErrInvalidStatus    = errors.New("invalid pre-order status")
)

// PreOrder represents a customer's reservation for a not-yet-available product.
type PreOrder struct {
	ID          string
	ProductID   string
	CustomerID  string
	Deposit     float64
	FullPrice   float64
	Status      string
	PlacedAt    time.Time
	EstimatedAt time.Time
	NotifiedAt  *time.Time
}

// Store is a thread-safe repository of pre-orders.
type Store struct {
	mu     sync.RWMutex
	orders map[string]PreOrder
}

// NewStore returns an initialised Store.
func NewStore() *Store {
	return &Store{orders: make(map[string]PreOrder)}
}

// Place records a new pre-order.
func (s *Store) Place(po PreOrder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orders[po.ID] = po
	return nil
}

// Get returns a copy of the pre-order for the given ID.
func (s *Store) Get(id string) (*PreOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	po, ok := s.orders[id]
	if !ok {
		return nil, ErrPreOrderNotFound
	}
	cp := po
	return &cp, nil
}

// UpdateStatus changes the status of an existing pre-order.
func (s *Store) UpdateStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	po, ok := s.orders[id]
	if !ok {
		return ErrPreOrderNotFound
	}
	po.Status = status
	s.orders[id] = po
	return nil
}

// List returns all pre-orders for a given productID.
func (s *Store) List(productID string) []PreOrder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []PreOrder
	for _, po := range s.orders {
		if po.ProductID == productID {
			out = append(out, po)
		}
	}
	return out
}

// productQueue holds the FIFO list of pre-order IDs for one product.
type productQueue struct {
	ids []string
}

// FulfillmentQueue is a thread-safe FIFO queue per product.
type FulfillmentQueue struct {
	mu     sync.Mutex
	queues map[string]*productQueue
}

// NewFulfillmentQueue returns an initialised FulfillmentQueue.
func NewFulfillmentQueue() *FulfillmentQueue {
	return &FulfillmentQueue{queues: make(map[string]*productQueue)}
}

func (fq *FulfillmentQueue) getQueue(productID string) *productQueue {
	q, ok := fq.queues[productID]
	if !ok {
		q = &productQueue{}
		fq.queues[productID] = q
	}
	return q
}

// Enqueue appends a pre-order ID to the back of the product's queue.
func (fq *FulfillmentQueue) Enqueue(preOrderID, productID string) {
	fq.mu.Lock()
	defer fq.mu.Unlock()
	q := fq.getQueue(productID)
	q.ids = append(q.ids, preOrderID)
}

// Dequeue removes and returns the front pre-order ID from the product's queue.
// Returns ("", false) if the queue is empty.
func (fq *FulfillmentQueue) Dequeue(productID string) (string, bool) {
	fq.mu.Lock()
	defer fq.mu.Unlock()
	q, ok := fq.queues[productID]
	if !ok || len(q.ids) == 0 {
		return "", false
	}
	id := q.ids[0]
	q.ids = q.ids[1:]
	return id, true
}

// Depth returns the number of items in the product's queue.
func (fq *FulfillmentQueue) Depth(productID string) int {
	fq.mu.Lock()
	defer fq.mu.Unlock()
	if q, ok := fq.queues[productID]; ok {
		return len(q.ids)
	}
	return 0
}

// NotificationDispatcher records when a pre-order customer was first notified.
type NotificationDispatcher struct{}

// NewNotificationDispatcher returns a NotificationDispatcher.
func NewNotificationDispatcher() NotificationDispatcher { return NotificationDispatcher{} }

// Notify sets NotifiedAt on the pre-order to now if it has not been set before.
// The caller is responsible for persisting the updated pre-order.
func (nd NotificationDispatcher) Notify(po *PreOrder, now time.Time) {
	if po.NotifiedAt == nil {
		t := now
		po.NotifiedAt = &t
	}
}

// HasBeenNotified returns true if NotifiedAt has been set.
func (nd NotificationDispatcher) HasBeenNotified(po *PreOrder) bool {
	return po.NotifiedAt != nil
}
