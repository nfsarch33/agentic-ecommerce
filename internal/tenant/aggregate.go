package tenant

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Status is the lifecycle state of a Tenant aggregate. Mirrors the
// transition-table pattern of internal/domain/membership/state.go.
type Status string

const (
	// StatusProvisioning is the initial state set by POST /tenants.
	// The tenant is not yet allowed to receive traffic.
	StatusProvisioning Status = "provisioning"
	// StatusActive is the operational state.
	StatusActive Status = "active"
	// StatusSuspended is a reversible disable. Settings and data
	// remain; traffic is rejected at the middleware layer.
	StatusSuspended Status = "suspended"
	// StatusArchived is terminal. The tenant row is retained for
	// audit but no transitions out.
	StatusArchived Status = "archived"
)

// StatusTransition is a named action driving the status state machine.
type StatusTransition string

const (
	StatusTransitionActivate StatusTransition = "activate"
	StatusTransitionSuspend  StatusTransition = "suspend"
	StatusTransitionArchive  StatusTransition = "archive"
)

// statusTransitionTable encodes every legal (from, transition) -> to.
// Anything missing is illegal and returns ErrInvalidTransition.
//
//	provisioning -> active                  (activate)
//	provisioning -> archived                (archive, never-launched cleanup)
//	active -> suspended                     (suspend)
//	active -> archived                      (archive)
//	suspended -> active                     (activate)
//	suspended -> archived                   (archive)
var statusTransitionTable = map[Status]map[StatusTransition]Status{
	StatusProvisioning: {
		StatusTransitionActivate: StatusActive,
		StatusTransitionArchive:  StatusArchived,
	},
	StatusActive: {
		StatusTransitionSuspend: StatusSuspended,
		StatusTransitionArchive: StatusArchived,
	},
	StatusSuspended: {
		StatusTransitionActivate: StatusActive,
		StatusTransitionArchive:  StatusArchived,
	},
}

// String returns the canonical string for a Status.
func (s Status) String() string { return string(s) }

// IsTerminal reports whether the Status permits any further transitions.
func (s Status) IsTerminal() bool {
	_, ok := statusTransitionTable[s]
	return !ok
}

// ParseStatus validates and returns the canonical Status for a string.
func ParseStatus(value string) (Status, error) {
	switch Status(value) {
	case StatusProvisioning, StatusActive, StatusSuspended, StatusArchived:
		return Status(value), nil
	default:
		return "", fmt.Errorf("%w: status=%q", ErrInvalidStatusTransition, value)
	}
}

// nextStatus looks up the destination for a (from, transition) pair.
func nextStatus(from Status, t StatusTransition) (Status, error) {
	moves, ok := statusTransitionTable[from]
	if !ok {
		return "", fmt.Errorf("%w: %s is terminal", ErrInvalidStatusTransition, from)
	}
	to, ok := moves[t]
	if !ok {
		return "", fmt.Errorf("%w: %s -> %s", ErrInvalidStatusTransition, from, t)
	}
	return to, nil
}

// Sentinel errors for the tenant aggregate. Settings-related errors
// remain in tenant.go.
var (
	ErrTenantNotFound          = errors.New("tenant not found")
	ErrTenantSlugInvalid       = errors.New("tenant slug invalid")
	ErrTenantSlugAlreadyExists = errors.New("tenant slug already exists")
	ErrTenantQuotaExceeded     = errors.New("tenant quota exceeded")
	ErrInvalidStatusTransition = errors.New("invalid tenant status transition")
)

// slugPattern enforces kebab-case for tenant slugs: lowercase, digits,
// hyphens; starts with a letter and ends with a letter or digit.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// IsValidSlug reports whether s is a kebab-case tenant slug.
func IsValidSlug(s string) bool {
	return slugPattern.MatchString(s)
}

// Tenant is the aggregate root for a customer-facing tenancy. The
// struct is exported as a value type so adapters can pass it by value
// and avoid pointer aliasing across goroutines.
type Tenant struct {
	ID        ID        `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Plan      string    `json:"plan"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AggregateRepository persists Tenant rows. It is intentionally a
// distinct port from `Repository` (which is for tenant settings)
// because the lifecycle and the settings have different audit
// requirements.
type AggregateRepository interface {
	Create(ctx context.Context, t Tenant) error
	Get(ctx context.Context, id ID) (Tenant, error)
	GetBySlug(ctx context.Context, slug string) (Tenant, error)
	List(ctx context.Context, page, perPage int) ([]Tenant, int, error)
	SaveStatus(ctx context.Context, t Tenant) error
	Update(ctx context.Context, t Tenant) error
}

// CreateTenantInput is the validated input for AggregateService.Create.
type CreateTenantInput struct {
	ID   ID
	Slug string
	Name string
	Plan string
}

// AggregateService orchestrates tenant lifecycle. It is the thing
// /api/v1/tenants endpoints call.
type AggregateService struct {
	repo  AggregateRepository
	now   func() time.Time
	quota int
}

// NewAggregateService validates the config and returns a service.
// Quota of zero means unlimited (the v2.4.0 default until v2.5.0
// per-tenant billing lands).
func NewAggregateService(repo AggregateRepository) *AggregateService {
	return &AggregateService{repo: repo, now: time.Now, quota: 0}
}

// WithQuota sets the maximum tenant count. Non-positive disables
// the quota check.
func (s *AggregateService) WithQuota(q int) *AggregateService {
	s.quota = q
	return s
}

// withClock is a test seam.
func (s *AggregateService) withClock(now func() time.Time) *AggregateService {
	s.now = now
	return s
}

// Create provisions a new tenant in StatusProvisioning. Slug must be
// unique (case-sensitive), kebab-case, and the quota (if set) must
// not be exceeded.
func (s *AggregateService) Create(ctx context.Context, in CreateTenantInput) (Tenant, error) {
	id, err := s.normaliseInputID(in.ID, in.Slug)
	if err != nil {
		return Tenant{}, err
	}
	if !IsValidSlug(in.Slug) {
		return Tenant{}, fmt.Errorf("%w: %q", ErrTenantSlugInvalid, in.Slug)
	}
	if strings.TrimSpace(in.Name) == "" {
		return Tenant{}, fmt.Errorf("%w: name empty", ErrTenantSlugInvalid)
	}
	if err := s.checkQuota(ctx); err != nil {
		return Tenant{}, err
	}
	if err := s.checkSlugUnique(ctx, in.Slug); err != nil {
		return Tenant{}, err
	}
	now := s.now().UTC()
	t := Tenant{
		ID:        id,
		Slug:      in.Slug,
		Name:      in.Name,
		Plan:      defaultPlan(in.Plan),
		Status:    StatusProvisioning,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return Tenant{}, err
	}
	return t, nil
}

// Get returns the tenant with the given id.
func (s *AggregateService) Get(ctx context.Context, id ID) (Tenant, error) {
	id, err := RequireID(id)
	if err != nil {
		return Tenant{}, err
	}
	return s.repo.Get(ctx, id)
}

// GetBySlug looks the tenant up by its public slug.
func (s *AggregateService) GetBySlug(ctx context.Context, slug string) (Tenant, error) {
	if !IsValidSlug(slug) {
		return Tenant{}, fmt.Errorf("%w: %q", ErrTenantSlugInvalid, slug)
	}
	return s.repo.GetBySlug(ctx, slug)
}

// List returns paginated tenants for super-admin views.
func (s *AggregateService) List(ctx context.Context, page, perPage int) ([]Tenant, int, error) {
	return s.repo.List(ctx, page, perPage)
}

// Update changes name/plan but never status. Status transitions go
// through Activate/Suspend/Archive.
func (s *AggregateService) Update(ctx context.Context, id ID, name, plan string) (Tenant, error) {
	id, err := RequireID(id)
	if err != nil {
		return Tenant{}, err
	}
	t, err := s.repo.Get(ctx, id)
	if err != nil {
		return Tenant{}, err
	}
	if name != "" {
		t.Name = name
	}
	if plan != "" {
		t.Plan = plan
	}
	t.UpdatedAt = s.now().UTC()
	if err := s.repo.Update(ctx, t); err != nil {
		return Tenant{}, err
	}
	return t, nil
}

// Activate transitions provisioning|suspended -> active.
func (s *AggregateService) Activate(ctx context.Context, id ID) (Tenant, error) {
	return s.transition(ctx, id, StatusTransitionActivate)
}

// Suspend transitions active -> suspended.
func (s *AggregateService) Suspend(ctx context.Context, id ID) (Tenant, error) {
	return s.transition(ctx, id, StatusTransitionSuspend)
}

// Archive transitions any non-archived state -> archived.
func (s *AggregateService) Archive(ctx context.Context, id ID) (Tenant, error) {
	return s.transition(ctx, id, StatusTransitionArchive)
}

func (s *AggregateService) transition(ctx context.Context, id ID, t StatusTransition) (Tenant, error) {
	id, err := RequireID(id)
	if err != nil {
		return Tenant{}, err
	}
	tenant, err := s.repo.Get(ctx, id)
	if err != nil {
		return Tenant{}, err
	}
	target, err := nextStatus(tenant.Status, t)
	if err != nil {
		return Tenant{}, err
	}
	tenant.Status = target
	tenant.UpdatedAt = s.now().UTC()
	if err := s.repo.SaveStatus(ctx, tenant); err != nil {
		return Tenant{}, err
	}
	return tenant, nil
}

func (s *AggregateService) normaliseInputID(id ID, slug string) (ID, error) {
	if strings.TrimSpace(string(id)) == "" {
		// Default to slug-as-id, which is the common storefront pattern.
		return RequireID(ID(slug))
	}
	return RequireID(id)
}

func (s *AggregateService) checkQuota(ctx context.Context) error {
	if s.quota <= 0 {
		return nil
	}
	_, total, err := s.repo.List(ctx, 1, 1)
	if err != nil {
		return err
	}
	if total >= s.quota {
		return fmt.Errorf("%w: limit=%d", ErrTenantQuotaExceeded, s.quota)
	}
	return nil
}

func (s *AggregateService) checkSlugUnique(ctx context.Context, slug string) error {
	if _, err := s.repo.GetBySlug(ctx, slug); err == nil {
		return fmt.Errorf("%w: %q", ErrTenantSlugAlreadyExists, slug)
	} else if !errors.Is(err, ErrTenantNotFound) {
		return err
	}
	return nil
}

func defaultPlan(plan string) string {
	if strings.TrimSpace(plan) == "" {
		return "free"
	}
	return plan
}

// InMemoryAggregateRepository is a goroutine-safe in-memory store for
// tests and dev mode. Slugs are unique across the store.
type InMemoryAggregateRepository struct {
	mu     sync.RWMutex
	byID   map[ID]Tenant
	bySlug map[string]ID
}

// NewInMemoryAggregateRepository builds an empty store.
func NewInMemoryAggregateRepository() *InMemoryAggregateRepository {
	return &InMemoryAggregateRepository{
		byID:   make(map[ID]Tenant),
		bySlug: make(map[string]ID),
	}
}

// Create inserts a tenant. Returns ErrTenantSlugAlreadyExists when the
// slug is taken.
func (r *InMemoryAggregateRepository) Create(_ context.Context, t Tenant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.bySlug[t.Slug]; ok {
		return fmt.Errorf("%w: %q", ErrTenantSlugAlreadyExists, t.Slug)
	}
	if _, ok := r.byID[t.ID]; ok {
		return fmt.Errorf("%w: id=%q", ErrTenantSlugAlreadyExists, t.ID)
	}
	r.byID[t.ID] = t
	r.bySlug[t.Slug] = t.ID
	return nil
}

// Get returns the tenant with the given id.
func (r *InMemoryAggregateRepository) Get(_ context.Context, id ID) (Tenant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byID[id]
	if !ok {
		return Tenant{}, fmt.Errorf("%w: id=%q", ErrTenantNotFound, id)
	}
	return t, nil
}

// GetBySlug returns the tenant with the given slug.
func (r *InMemoryAggregateRepository) GetBySlug(_ context.Context, slug string) (Tenant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.bySlug[slug]
	if !ok {
		return Tenant{}, fmt.Errorf("%w: slug=%q", ErrTenantNotFound, slug)
	}
	return r.byID[id], nil
}

// List returns paginated tenants sorted by created_at ascending.
func (r *InMemoryAggregateRepository) List(_ context.Context, page, perPage int) ([]Tenant, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	tenants := make([]Tenant, 0, len(r.byID))
	for _, t := range r.byID {
		tenants = append(tenants, t)
	}
	// Sort newest first to match admin-list expectations.
	for i := 1; i < len(tenants); i++ {
		for j := i; j > 0 && tenants[j-1].CreatedAt.Before(tenants[j].CreatedAt); j-- {
			tenants[j-1], tenants[j] = tenants[j], tenants[j-1]
		}
	}
	total := len(tenants)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	out := make([]Tenant, end-start)
	copy(out, tenants[start:end])
	return out, total, nil
}

// SaveStatus persists a status-only change.
func (r *InMemoryAggregateRepository) SaveStatus(_ context.Context, t Tenant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.byID[t.ID]
	if !ok {
		return fmt.Errorf("%w: id=%q", ErrTenantNotFound, t.ID)
	}
	existing.Status = t.Status
	existing.UpdatedAt = t.UpdatedAt
	r.byID[t.ID] = existing
	return nil
}

// Update persists a name/plan change.
func (r *InMemoryAggregateRepository) Update(_ context.Context, t Tenant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.byID[t.ID]
	if !ok {
		return fmt.Errorf("%w: id=%q", ErrTenantNotFound, t.ID)
	}
	existing.Name = t.Name
	existing.Plan = t.Plan
	existing.UpdatedAt = t.UpdatedAt
	r.byID[t.ID] = existing
	return nil
}
