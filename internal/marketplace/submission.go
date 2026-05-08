package marketplace

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Submission is the v2.7.0 vendor-side plugin submission row. It
// carries the candidate Manifest and the lifecycle state.
//
// Tenant scoping mirrors the rest of the marketplace package:
// every Submission belongs to exactly one tenant. SubmitterEmail is
// the human contact channel; the super-admin sets ReviewNotes when
// approving or rejecting.
type Submission struct {
	ID             string
	TenantID       string
	SubmitterEmail string
	Manifest       Manifest
	State          SubmissionState
	ReviewNotes    string
	SubmittedAt    string
	ReviewedAt     string
	Reviewer       string
}

// SubmissionRepository persists Submission rows. Implementations
// MUST be tenant-aware so listings cannot accidentally cross
// tenants. The super-admin path uses the empty tenantID to opt
// into the cross-tenant queue (gated by RBAC at the handler).
type SubmissionRepository interface {
	Create(ctx context.Context, s Submission) error
	Get(ctx context.Context, id string) (Submission, error)
	ListPending(ctx context.Context, tenantID string, page, perPage int) ([]Submission, int, error)
	SaveState(ctx context.Context, s Submission) error
}

// SubmissionService orchestrates submit/approve/reject. It holds a
// reference to the parent Catalog so approvals can publish into
// marketplace_plugins atomically with the state transition.
type SubmissionService struct {
	subs    SubmissionRepository
	catalog CatalogRepository
	clock   Clock
}

// SubmissionServiceConfig wires SubmissionService.
type SubmissionServiceConfig struct {
	Submissions SubmissionRepository
	Catalog     CatalogRepository
	Clock       Clock
}

// NewSubmissionService validates the config and returns a service.
func NewSubmissionService(cfg SubmissionServiceConfig) (*SubmissionService, error) {
	if cfg.Submissions == nil {
		return nil, fmt.Errorf("%w: submissions repository missing", ErrSubmissionInvalid)
	}
	if cfg.Catalog == nil {
		return nil, fmt.Errorf("%w: catalog repository missing", ErrSubmissionInvalid)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = defaultClock
	}
	return &SubmissionService{subs: cfg.Submissions, catalog: cfg.Catalog, clock: clock}, nil
}

// emailPattern is a minimal RFC 5322 subset: one@two with at least
// one dot in the right hand side. Validation is intentionally
// loose because this is a contact field, not an authentication
// surface.
var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// Submit creates a new Submission row in pending_review state.
// Returns ErrSubmissionInvalid for shape failures and the
// ErrSubmissionAlreadyExists sentinel when the repository rejects
// the insert as a duplicate.
func (s *SubmissionService) Submit(ctx context.Context, tenantID, submitterEmail, id string, m Manifest) (Submission, error) {
	if id = strings.TrimSpace(id); id == "" {
		return Submission{}, fmt.Errorf("%w: id required", ErrSubmissionInvalid)
	}
	if tenantID = strings.TrimSpace(tenantID); tenantID == "" {
		return Submission{}, fmt.Errorf("%w: tenantID required", ErrSubmissionInvalid)
	}
	submitterEmail = strings.TrimSpace(submitterEmail)
	if !emailPattern.MatchString(submitterEmail) {
		return Submission{}, fmt.Errorf("%w: submitter email invalid", ErrSubmissionInvalid)
	}
	if err := m.Validate(); err != nil {
		return Submission{}, fmt.Errorf("%w: %s", ErrSubmissionInvalid, err.Error())
	}
	row := Submission{
		ID:             id,
		TenantID:       tenantID,
		SubmitterEmail: submitterEmail,
		Manifest:       m,
		State:          SubmissionPendingReview,
		SubmittedAt:    s.clock(),
	}
	if err := s.subs.Create(ctx, row); err != nil {
		return Submission{}, err
	}
	return row, nil
}

// ListPending returns the queue of pending submissions for tenantID.
// Pass tenantID="" for the super-admin cross-tenant view.
func (s *SubmissionService) ListPending(ctx context.Context, tenantID string, page, perPage int) ([]Submission, int, error) {
	return s.subs.ListPending(ctx, tenantID, page, perPage)
}

// Approve transitions the submission to approved, registers the
// manifest in the catalog, and persists the state change. The
// reviewer string is the super-admin user identifier captured in
// the audit trail.
func (s *SubmissionService) Approve(ctx context.Context, id, reviewer, notes string) (Submission, error) {
	row, err := s.transition(ctx, id, SubmissionTransitionApprove, reviewer, notes)
	if err != nil {
		return Submission{}, err
	}
	if regErr := s.catalog.RegisterManifest(ctx, row.Manifest); regErr != nil {
		// If the slug already exists, the manifest is already in the
		// catalogue from a prior approval; that's idempotent and we
		// keep the approval. All other errors propagate.
		return Submission{}, regErr
	}
	return row, nil
}

// Reject transitions the submission to rejected without touching
// the catalog. Notes carries the human-readable reason.
func (s *SubmissionService) Reject(ctx context.Context, id, reviewer, notes string) (Submission, error) {
	return s.transition(ctx, id, SubmissionTransitionReject, reviewer, notes)
}

// Get returns the current Submission row.
func (s *SubmissionService) Get(ctx context.Context, id string) (Submission, error) {
	return s.subs.Get(ctx, id)
}

func (s *SubmissionService) transition(ctx context.Context, id string, t SubmissionTransition, reviewer, notes string) (Submission, error) {
	if id = strings.TrimSpace(id); id == "" {
		return Submission{}, fmt.Errorf("%w: id required", ErrSubmissionInvalid)
	}
	if reviewer = strings.TrimSpace(reviewer); reviewer == "" {
		return Submission{}, fmt.Errorf("%w: reviewer required", ErrSubmissionInvalid)
	}
	row, err := s.subs.Get(ctx, id)
	if err != nil {
		return Submission{}, err
	}
	to, err := nextSubmissionState(row.State, t)
	if err != nil {
		return Submission{}, err
	}
	row.State = to
	row.ReviewNotes = notes
	row.Reviewer = reviewer
	row.ReviewedAt = s.clock()
	if err := s.subs.SaveState(ctx, row); err != nil {
		return Submission{}, err
	}
	return row, nil
}
