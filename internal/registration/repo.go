package registration

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Repository persists RegistrationRequest rows. Tenant id is set on
// MarkActive so the repository row links back to the provisioned
// tenant aggregate.
type Repository interface {
	Create(ctx context.Context, r Request) error
	Get(ctx context.Context, id string) (Request, error)
	GetByEmail(ctx context.Context, email string) (Request, error)
	List(ctx context.Context, page, perPage int) ([]Request, int, error)
	Save(ctx context.Context, r Request) error
}

// ErrRequestNotFound is returned when a registration row cannot be
// resolved.
var ErrRequestNotFound = fmt.Errorf("registration request not found")

// ErrRequestAlreadyExists is returned when Create is asked to insert
// a duplicate id or email.
var ErrRequestAlreadyExists = fmt.Errorf("registration request already exists")

// InMemoryRepository is the goroutine-safe in-process store used by
// tests and dev mode.
type InMemoryRepository struct {
	mu      sync.RWMutex
	byID    map[string]Request
	byEmail map[string]string
}

// NewInMemoryRepository returns an empty store.
func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{byID: make(map[string]Request), byEmail: make(map[string]string)}
}

// Create inserts a row. Returns ErrRequestAlreadyExists for duplicate
// id or duplicate email.
func (r *InMemoryRepository) Create(_ context.Context, req Request) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[req.ID]; ok {
		return fmt.Errorf("%w: id=%q", ErrRequestAlreadyExists, req.ID)
	}
	if existingID, ok := r.byEmail[req.Email]; ok {
		// Allow re-registering when the prior row has expired or is
		// still in pending state; reject only when the email is
		// already active.
		if existing, ok := r.byID[existingID]; ok && existing.Status == StatusActive {
			return fmt.Errorf("%w: email=%q", ErrRequestAlreadyExists, req.Email)
		}
	}
	r.byID[req.ID] = req
	r.byEmail[req.Email] = req.ID
	return nil
}

// Get returns the row with the given id.
func (r *InMemoryRepository) Get(_ context.Context, id string) (Request, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	row, ok := r.byID[id]
	if !ok {
		return Request{}, fmt.Errorf("%w: id=%q", ErrRequestNotFound, id)
	}
	return row, nil
}

// GetByEmail returns the row with the given email.
func (r *InMemoryRepository) GetByEmail(_ context.Context, email string) (Request, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byEmail[email]
	if !ok {
		return Request{}, fmt.Errorf("%w: email=%q", ErrRequestNotFound, email)
	}
	return r.byID[id], nil
}

// List returns paginated rows sorted by created_at desc.
func (r *InMemoryRepository) List(_ context.Context, page, perPage int) ([]Request, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Request, 0, len(r.byID))
	for _, row := range r.byID {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	total := len(out)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	pageRows := make([]Request, end-start)
	copy(pageRows, out[start:end])
	return pageRows, total, nil
}

// Save persists status updates. Errors when the row is missing.
func (r *InMemoryRepository) Save(_ context.Context, req Request) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[req.ID]; !ok {
		return fmt.Errorf("%w: id=%q", ErrRequestNotFound, req.ID)
	}
	r.byID[req.ID] = req
	return nil
}
