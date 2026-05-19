package cart

import (
	"context"
	"sync"
	"time"
)

// Cart represents a shopping cart with its last activity timestamp.
type Cart struct {
	ID         string
	CustomerID string
	UpdatedAt  time.Time
	recovered  bool
}

// CartStore is the persistence interface for carts.
type CartStore interface {
	Save(c Cart)
	All() []Cart
	MarkRecovered(id string)
	IsRecovered(id string) bool
}

// MemoryCartStore is an in-memory CartStore for testing.
type MemoryCartStore struct {
	mu        sync.Mutex
	carts     map[string]Cart
	recovered map[string]bool
}

func NewMemoryCartStore() *MemoryCartStore {
	return &MemoryCartStore{
		carts:     make(map[string]Cart),
		recovered: make(map[string]bool),
	}
}

func (s *MemoryCartStore) Save(c Cart) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.carts[c.ID] = c
}

func (s *MemoryCartStore) All() []Cart {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Cart, 0, len(s.carts))
	for _, c := range s.carts {
		out = append(out, c)
	}
	return out
}

func (s *MemoryCartStore) MarkRecovered(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recovered[id] = true
}

func (s *MemoryCartStore) IsRecovered(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recovered[id]
}

// AbandonmentDetector finds carts inactive longer than the threshold.
type AbandonmentDetector struct {
	store CartStore
}

func NewAbandonmentDetector(store CartStore) *AbandonmentDetector {
	return &AbandonmentDetector{store: store}
}

func (d *AbandonmentDetector) DetectAbandoned(_ context.Context, threshold time.Duration) []Cart {
	cutoff := time.Now().Add(-threshold)
	all := d.store.All()
	out := make([]Cart, 0)
	for _, c := range all {
		if c.UpdatedAt.Before(cutoff) {
			out = append(out, c)
		}
	}
	return out
}

// RecoveryNotifier is called with each abandoned cart to send a recovery nudge.
type RecoveryNotifier func(c Cart) error

// RecoveryWorkflow detects abandoned carts and dispatches recovery notifications.
type RecoveryWorkflow struct {
	store     CartStore
	notifier  RecoveryNotifier
	threshold time.Duration
	detector  *AbandonmentDetector
}

func NewRecoveryWorkflow(store CartStore, notifier RecoveryNotifier, threshold time.Duration) *RecoveryWorkflow {
	return &RecoveryWorkflow{
		store:     store,
		notifier:  notifier,
		threshold: threshold,
		detector:  NewAbandonmentDetector(store),
	}
}

// RunOnce detects abandoned carts and notifies each that has not been recovered yet.
// Returns the count of carts for which a recovery notification was sent.
func (w *RecoveryWorkflow) RunOnce(ctx context.Context) (int, error) {
	abandoned := w.detector.DetectAbandoned(ctx, w.threshold)
	count := 0
	for _, c := range abandoned {
		if w.store.IsRecovered(c.ID) {
			continue
		}
		if err := w.notifier(c); err != nil {
			continue
		}
		w.store.MarkRecovered(c.ID)
		count++
	}
	return count, nil
}
