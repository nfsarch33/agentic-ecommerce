package inmemory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
)

// MarketplaceSubmissions is the in-memory implementation of
// marketplace.SubmissionRepository. Mirrors the shape of
// MarketplaceCatalog/MarketplaceInstallations so wiring stays
// familiar and so unit tests do not need docker.
type MarketplaceSubmissions struct {
	mu   sync.RWMutex
	byID map[string]marketplace.Submission
}

// NewMarketplaceSubmissions returns an empty submission queue.
func NewMarketplaceSubmissions() *MarketplaceSubmissions {
	return &MarketplaceSubmissions{byID: make(map[string]marketplace.Submission)}
}

// Create inserts a new submission. Returns ErrSubmissionAlreadyExists
// when ID is duplicated.
func (s *MarketplaceSubmissions) Create(_ context.Context, row marketplace.Submission) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[row.ID]; ok {
		return fmt.Errorf("%w: id=%s", marketplace.ErrSubmissionAlreadyExists, row.ID)
	}
	s.byID[row.ID] = row
	return nil
}

// Get returns the submission row by ID.
func (s *MarketplaceSubmissions) Get(_ context.Context, id string) (marketplace.Submission, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.byID[id]
	if !ok {
		return marketplace.Submission{}, fmt.Errorf("%w: id=%s", marketplace.ErrSubmissionNotFound, id)
	}
	return row, nil
}

// ListPending returns the queue. Pass tenantID="" for the cross-
// tenant super-admin view.
func (s *MarketplaceSubmissions) ListPending(_ context.Context, tenantID string, page, perPage int) ([]marketplace.Submission, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	rows := make([]marketplace.Submission, 0, len(s.byID))
	for _, row := range s.byID {
		if row.State != marketplace.SubmissionPendingReview {
			continue
		}
		if tenantID != "" && row.TenantID != tenantID {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].SubmittedAt < rows[j].SubmittedAt })
	total := len(rows)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	out := make([]marketplace.Submission, end-start)
	copy(out, rows[start:end])
	return out, total, nil
}

// SaveState updates the state, review notes, reviewer, and reviewed
// timestamp of an existing submission. Returns ErrSubmissionNotFound
// when the row does not exist.
func (s *MarketplaceSubmissions) SaveState(_ context.Context, row marketplace.Submission) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[row.ID]; !ok {
		return fmt.Errorf("%w: id=%s", marketplace.ErrSubmissionNotFound, row.ID)
	}
	s.byID[row.ID] = row
	return nil
}
