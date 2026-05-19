// Package searchanal provides search analytics: query logging, zero-result detection, and synonym suggestions.
package searchanal

import (
	"sort"
	"sync"
	"time"
)

// QueryEvent represents a single search query event.
type QueryEvent struct {
	Query       string
	ResultCount int
	SessionID   string
	OccurredAt  time.Time
}

// QueryCount pairs a query string with its occurrence count.
type QueryCount struct {
	Query string
	Count int
}

// QueryLog is a thread-safe store for query events.
type QueryLog struct {
	mu     sync.RWMutex
	events []QueryEvent
	counts map[string]int // query -> total count
}

// NewQueryLog creates a new QueryLog.
func NewQueryLog() *QueryLog {
	return &QueryLog{
		counts: make(map[string]int),
	}
}

// Record stores a query event.
func (l *QueryLog) Record(event QueryEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
	l.counts[event.Query]++
}

// ZeroResultQueries returns all distinct queries with zero results recorded on or after since.
func (l *QueryLog) ZeroResultQueries(since time.Time) []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	seen := make(map[string]struct{})
	var result []string
	for _, e := range l.events {
		if e.ResultCount == 0 && !e.OccurredAt.Before(since) {
			if _, ok := seen[e.Query]; !ok {
				seen[e.Query] = struct{}{}
				result = append(result, e.Query)
			}
		}
	}
	return result
}

// TopQueries returns the top-n queries by total count, sorted descending.
// Ties are broken alphabetically by query string.
func (l *QueryLog) TopQueries(n int) []QueryCount {
	l.mu.RLock()
	counts := make(map[string]int, len(l.counts))
	for k, v := range l.counts {
		counts[k] = v
	}
	l.mu.RUnlock()

	all := make([]QueryCount, 0, len(counts))
	for q, c := range counts {
		all = append(all, QueryCount{Query: q, Count: c})
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Count != all[j].Count {
			return all[i].Count > all[j].Count
		}
		return all[i].Query < all[j].Query
	})

	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

// SynonymStore is a thread-safe store mapping queries to their synonyms.
type SynonymStore struct {
	mu       sync.RWMutex
	synonyms map[string][]string
}

// NewSynonymStore creates a new SynonymStore.
func NewSynonymStore() *SynonymStore {
	return &SynonymStore{
		synonyms: make(map[string][]string),
	}
}

// Add registers synonym as a synonym for query.
// Duplicate synonyms for the same query are not added.
func (s *SynonymStore) Add(query, synonym string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.synonyms[query] {
		if existing == synonym {
			return
		}
	}
	s.synonyms[query] = append(s.synonyms[query], synonym)
}

// Synonyms returns all synonyms registered for the given query.
func (s *SynonymStore) Synonyms(query string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	syns := s.synonyms[query]
	if len(syns) == 0 {
		return nil
	}
	result := make([]string, len(syns))
	copy(result, syns)
	return result
}

// ZeroResultHandler suggests alternative queries for zero-result searches.
type ZeroResultHandler struct{}

// Suggests returns the list of synonyms for a zero-result query from the synonym store.
// Returns nil if no synonyms are found.
func (h *ZeroResultHandler) Suggests(zeroResultQuery string, synonymStore *SynonymStore) []string {
	return synonymStore.Synonyms(zeroResultQuery)
}
