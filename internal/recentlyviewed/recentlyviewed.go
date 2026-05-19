// Package recentlyviewed tracks per-user product view history with
// exponential decay scoring for personalisation.
package recentlyviewed

import (
	"math"
	"sort"
	"sync"
	"time"
)

const maxPerUser = 50

// ViewEvent captures a single product view within a session.
type ViewEvent struct {
	ProductID string
	ViewedAt  time.Time
	SessionID string
}

// ScoredProduct pairs a product with its decay-weighted relevance score.
type ScoredProduct struct {
	ProductID string
	Score     float64
}

// Store is a thread-safe per-user view history with FIFO eviction at maxPerUser.
type Store struct {
	mu      sync.RWMutex
	history map[string][]ViewEvent
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{history: make(map[string][]ViewEvent)}
}

// Record appends a view event for userID, evicting the oldest if at capacity.
func (s *Store) Record(userID string, event ViewEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.history[userID]
	if len(h) >= maxPerUser {
		// Drop the oldest (index 0)
		h = h[1:]
	}
	s.history[userID] = append(h, event)
}

// Recent returns the most recent limit events for userID, newest first.
func (s *Store) Recent(userID string, limit int) []ViewEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := s.history[userID]
	if len(h) == 0 {
		return nil
	}
	// Return up to limit items from the end (most recent)
	start := len(h) - limit
	if start < 0 {
		start = 0
	}
	out := make([]ViewEvent, len(h)-start)
	copy(out, h[start:])
	// Reverse so index 0 is the most recent
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Clear removes all view history for userID.
func (s *Store) Clear(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.history, userID)
}

// All returns all events for userID (oldest first). Internal helper.
func (s *Store) all(userID string) []ViewEvent {
	h := s.history[userID]
	out := make([]ViewEvent, len(h))
	copy(out, h)
	return out
}

// DecayScorer computes time-based exponential decay scores.
type DecayScorer struct{}

// Score returns a value in (0, 1]. A view from now scores 1.0; a view
// halfLifeHours ago scores 0.5.
func (d DecayScorer) Score(event ViewEvent, now time.Time, halfLifeHours float64) float64 {
	if halfLifeHours <= 0 {
		return 1.0
	}
	ageHours := now.Sub(event.ViewedAt).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	return math.Pow(0.5, ageHours/halfLifeHours)
}

// PersonalizationSignal aggregates view scores into ranked product recommendations.
type PersonalizationSignal struct{}

// TopProducts returns ScoredProducts sorted by descending decay-aggregated score,
// limited to limit items.
func (p PersonalizationSignal) TopProducts(userID string, store *Store, now time.Time, halfLife float64, limit int) []ScoredProduct {
	store.mu.RLock()
	events := store.all(userID)
	store.mu.RUnlock()

	scorer := DecayScorer{}
	totals := make(map[string]float64)
	for _, ev := range events {
		totals[ev.ProductID] += scorer.Score(ev, now, halfLife)
	}

	result := make([]ScoredProduct, 0, len(totals))
	for pid, sc := range totals {
		result = append(result, ScoredProduct{ProductID: pid, Score: sc})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}
