package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
	"github.com/nfsarch33/agentic-ecommerce/internal/security"
)

// submissionRequest is the wire shape posted by vendors to
// /api/v1/marketplace/plugins/submit. The handler converts this into
// a marketplace.Manifest before invoking the SubmissionService.
type submissionRequest struct {
	SubmitterEmail string                       `json:"submitter_email"`
	Slug           string                       `json:"slug"`
	Name           string                       `json:"name"`
	Version        string                       `json:"version"`
	Vendor         string                       `json:"vendor"`
	Description    string                       `json:"description,omitempty"`
	Category       string                       `json:"category,omitempty"`
	HomepageURL    string                       `json:"homepage_url,omitempty"`
	EventSubs      []string                     `json:"event_subscriptions,omitempty"`
	Permissions    []string                     `json:"permissions,omitempty"`
	Dependencies   []manifestDependencyResponse `json:"dependencies,omitempty"`
}

// submissionResponse is the wire shape returned by submission and
// admin endpoints. It mirrors the marketplace.Submission row.
type submissionResponse struct {
	ID             string           `json:"id"`
	TenantID       string           `json:"tenant_id"`
	SubmitterEmail string           `json:"submitter_email"`
	Manifest       manifestResponse `json:"manifest"`
	State          string           `json:"state"`
	ReviewNotes    string           `json:"review_notes,omitempty"`
	SubmittedAt    string           `json:"submitted_at"`
	ReviewedAt     string           `json:"reviewed_at,omitempty"`
	Reviewer       string           `json:"reviewer,omitempty"`
}

type submissionListResponse struct {
	Submissions []submissionResponse `json:"submissions"`
	Total       int                  `json:"total"`
	Page        int                  `json:"page"`
	PerPage     int                  `json:"per_page"`
}

type reviewActionRequest struct {
	ReviewNotes string `json:"review_notes,omitempty"`
}

// submitMarketplacePlugin handles POST /api/v1/marketplace/plugins/submit.
// Vendors call this from an authenticated tenant context. The
// handler builds the Manifest, generates a submission ID, and
// hands off to SubmissionService.Submit.
func (s *server) submitMarketplacePlugin(w http.ResponseWriter, r *http.Request) {
	if s.marketplaceSubs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "submissions_unconfigured"})
		return
	}
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	var body submissionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	manifest := submissionToManifest(body)
	id := strings.TrimSpace("sub-" + uuid.NewString())
	row, err := s.marketplaceSubs.Submit(r.Context(), tenantID, body.SubmitterEmail, id, manifest)
	if err != nil {
		writeSubmissionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSubmissionResponse(row))
}

// adminSubmissionsHandler routes /api/v1/admin/marketplace/submissions*.
func (s *server) adminSubmissionsHandler(w http.ResponseWriter, r *http.Request) {
	if s.marketplaceSubs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "submissions_unconfigured"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/marketplace/submissions")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" && r.Method == http.MethodGet {
		s.listAdminSubmissions(w, r)
		return
	}
	id, action := splitPluginPath(rest)
	if id == "" {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		s.getAdminSubmission(w, r, id)
	case action == "approve" && r.Method == http.MethodPost:
		s.approveAdminSubmission(w, r, id)
	case action == "reject" && r.Method == http.MethodPost:
		s.rejectAdminSubmission(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) listAdminSubmissions(w http.ResponseWriter, r *http.Request) {
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	tenantFilter := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	rows, total, err := s.marketplaceSubs.ListPending(r.Context(), tenantFilter, page, perPage)
	if err != nil {
		s.log.Error("list submissions", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	out := make([]submissionResponse, len(rows))
	for i, row := range rows {
		out[i] = toSubmissionResponse(row)
	}
	writeJSON(w, http.StatusOK, submissionListResponse{Submissions: out, Total: total, Page: page, PerPage: perPage})
}

func (s *server) getAdminSubmission(w http.ResponseWriter, r *http.Request, id string) {
	row, err := s.marketplaceSubs.Get(r.Context(), id)
	if err != nil {
		writeSubmissionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSubmissionResponse(row))
}

func (s *server) approveAdminSubmission(w http.ResponseWriter, r *http.Request, id string) {
	reviewer, body := s.reviewerFromRequest(r)
	row, err := s.marketplaceSubs.Approve(r.Context(), id, reviewer, body.ReviewNotes)
	if err != nil {
		writeSubmissionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSubmissionResponse(row))
}

func (s *server) rejectAdminSubmission(w http.ResponseWriter, r *http.Request, id string) {
	reviewer, body := s.reviewerFromRequest(r)
	row, err := s.marketplaceSubs.Reject(r.Context(), id, reviewer, body.ReviewNotes)
	if err != nil {
		writeSubmissionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSubmissionResponse(row))
}

// reviewerFromRequest extracts the reviewer identifier from the
// authenticated actor in the request context. The body is also
// decoded for review_notes; when the body is absent or invalid the
// review_notes default to empty string.
func (s *server) reviewerFromRequest(r *http.Request) (string, reviewActionRequest) {
	subject := "admin"
	if actor, ok := r.Context().Value(actorContextKey{}).(requestActor); ok && actor.Subject != "" {
		subject = actor.Subject
	}
	var body reviewActionRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	return subject, body
}

// submissionRole gates the vendor-facing submit endpoint to
// operator+; the admin endpoints are gated by the super-admin
// adminBillingRole shape.
func submissionRole(r *http.Request) security.Role {
	if r.Method == http.MethodGet {
		return security.RoleViewer
	}
	return security.RoleOperator
}

func submissionAuditAction(r *http.Request) auditAction {
	if r.Method == http.MethodGet {
		return auditAction{}
	}
	return auditAction{Action: "marketplace.submission.create", Resource: r.URL.Path, Mutates: true}
}

func adminSubmissionsRole(_ *http.Request) security.Role {
	return security.RoleAdmin
}

func adminSubmissionsAuditAction(r *http.Request) auditAction {
	if r.Method == http.MethodGet {
		return auditAction{}
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/marketplace/submissions/")
	switch {
	case strings.HasSuffix(rest, "/approve"):
		return auditAction{Action: "marketplace.submission.approve", Resource: rest, Mutates: true}
	case strings.HasSuffix(rest, "/reject"):
		return auditAction{Action: "marketplace.submission.reject", Resource: rest, Mutates: true}
	default:
		return auditAction{}
	}
}

func writeSubmissionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, marketplace.ErrSubmissionNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	case errors.Is(err, marketplace.ErrSubmissionAlreadyExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "submission_exists"})
	case errors.Is(err, marketplace.ErrSubmissionInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_submission"})
	case errors.Is(err, marketplace.ErrInvalidTransition):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_transition"})
	case errors.Is(err, marketplace.ErrSlugAlreadyExists):
		writeJSON(w, http.StatusOK, map[string]string{"warning": "slug_existed_idempotent"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
	}
}

func submissionToManifest(req submissionRequest) marketplace.Manifest {
	events := make([]marketplace.EventName, len(req.EventSubs))
	for i, e := range req.EventSubs {
		events[i] = marketplace.EventName(e)
	}
	perms := make([]marketplace.Permission, len(req.Permissions))
	for i, p := range req.Permissions {
		perms[i] = marketplace.Permission(p)
	}
	deps := make([]marketplace.DependencyRef, len(req.Dependencies))
	for i, d := range req.Dependencies {
		deps[i] = marketplace.DependencyRef{Slug: d.Slug, Constraint: d.Constraint}
	}
	return marketplace.Manifest{
		Slug:               req.Slug,
		Name:               req.Name,
		Version:            req.Version,
		Vendor:             req.Vendor,
		Description:        req.Description,
		Category:           req.Category,
		HomepageURL:        req.HomepageURL,
		EventSubscriptions: events,
		Permissions:        perms,
		Dependencies:       deps,
	}
}

func toSubmissionResponse(row marketplace.Submission) submissionResponse {
	return submissionResponse{
		ID:             row.ID,
		TenantID:       row.TenantID,
		SubmitterEmail: row.SubmitterEmail,
		Manifest:       toManifestResponse(row.Manifest),
		State:          string(row.State),
		ReviewNotes:    row.ReviewNotes,
		SubmittedAt:    row.SubmittedAt,
		ReviewedAt:     row.ReviewedAt,
		Reviewer:       row.Reviewer,
	}
}
