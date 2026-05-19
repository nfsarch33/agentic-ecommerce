// Package backorder manages backorder handling with ETA calculation,
// priority queuing, and notification tracking.
package backorder

import (
	"errors"
	"sort"
	"sync"
	"time"
)

const (
	StatusWaiting   = "waiting"
	StatusAllocated = "allocated"
	StatusFulfilled = "fulfilled"
	StatusCancelled = "cancelled"
)

var (
	ErrNotFound     = errors.New("backorder not found")
	ErrInvalidStatus = errors.New("invalid status")
)

// BackOrder represents a customer backorder for an out-of-stock product.
type BackOrder struct {
	ID          string
	ProductID   string
	CustomerID  string
	Quantity    int
	Priority    int
	PlacedAt    time.Time
	EstimatedAt *time.Time
	Notified    bool
	Status      string
}

// Store is a thread-safe in-memory backorder repository.
type Store struct {
	mu     sync.RWMutex
	orders map[string]BackOrder
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{orders: make(map[string]BackOrder)}
}

// Add inserts a new backorder. Overwrites if same ID exists.
func (s *Store) Add(bo BackOrder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orders[bo.ID] = bo
}

// Get returns a backorder by ID.
func (s *Store) Get(id string) (*BackOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bo, ok := s.orders[id]
	if !ok {
		return nil, ErrNotFound
	}
	copy := bo
	return &copy, nil
}

// UpdateETA sets the estimated arrival time for a backorder.
func (s *Store) UpdateETA(id string, eta time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	bo, ok := s.orders[id]
	if !ok {
		return ErrNotFound
	}
	bo.EstimatedAt = &eta
	s.orders[id] = bo
	return nil
}

// UpdateStatus changes the status of a backorder.
func (s *Store) UpdateStatus(id, status string) error {
	switch status {
	case StatusWaiting, StatusAllocated, StatusFulfilled, StatusCancelled:
	default:
		return ErrInvalidStatus
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bo, ok := s.orders[id]
	if !ok {
		return ErrNotFound
	}
	bo.Status = status
	s.orders[id] = bo
	return nil
}

// SetNotified marks the notified flag for a backorder.
func (s *Store) SetNotified(id string, notified bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	bo, ok := s.orders[id]
	if !ok {
		return ErrNotFound
	}
	bo.Notified = notified
	s.orders[id] = bo
	return nil
}

// PriorityQueue is a thread-safe per-product priority queue.
// Higher Priority value = dequeued first. Ties broken by PlacedAt (earlier = first).
type PriorityQueue struct {
	mu     sync.Mutex
	queues map[string][]BackOrder
}

// NewPriorityQueue creates an empty PriorityQueue.
func NewPriorityQueue() *PriorityQueue {
	return &PriorityQueue{queues: make(map[string][]BackOrder)}
}

// Enqueue adds a backorder to its product queue maintaining sorted order.
func (pq *PriorityQueue) Enqueue(bo BackOrder) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	q := pq.queues[bo.ProductID]
	q = append(q, bo)
	sort.SliceStable(q, func(i, j int) bool {
		if q[i].Priority != q[j].Priority {
			return q[i].Priority > q[j].Priority
		}
		return q[i].PlacedAt.Before(q[j].PlacedAt)
	})
	pq.queues[bo.ProductID] = q
}

// Dequeue removes and returns the highest-priority backorder for a product.
func (pq *PriorityQueue) Dequeue(productID string) (*BackOrder, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	q := pq.queues[productID]
	if len(q) == 0 {
		return nil, false
	}
	head := q[0]
	pq.queues[productID] = q[1:]
	return &head, true
}

// Peek returns the highest-priority backorder without removing it.
func (pq *PriorityQueue) Peek(productID string) (*BackOrder, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	q := pq.queues[productID]
	if len(q) == 0 {
		return nil, false
	}
	copy := q[0]
	return &copy, true
}

// Depth returns the number of queued backorders for a product.
func (pq *PriorityQueue) Depth(productID string) int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return len(pq.queues[productID])
}

// ETACalculator estimates arrival times based on queue depth.
type ETACalculator struct{}

// EstimateETA returns an estimated arrival date.
// Each unit of queue depth adds daysPerUnit days to baseDate.
func (e ETACalculator) EstimateETA(productID string, baseDate time.Time, queueDepth int, daysPerUnit int) time.Time {
	days := queueDepth * daysPerUnit
	return baseDate.AddDate(0, 0, days)
}
