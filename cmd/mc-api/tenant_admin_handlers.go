package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/security"
	tenantpkg "github.com/nfsarch33/helixon-ec/internal/tenant"
)

// tenantAdminCreateRequest is the JSON body for POST /api/v1/tenants.
type tenantAdminCreateRequest struct {
	ID   string `json:"id,omitempty"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	Plan string `json:"plan,omitempty"`
}

// tenantAdminUpdateRequest patches name and/or plan.
type tenantAdminUpdateRequest struct {
	Name *string `json:"name,omitempty"`
	Plan *string `json:"plan,omitempty"`
}

// tenantAdminResponse is the wire shape for a Tenant aggregate.
type tenantAdminResponse struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Plan      string    `json:"plan"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type tenantAdminListResponse struct {
	Tenants []tenantAdminResponse `json:"tenants"`
	Total   int                   `json:"total"`
	Page    int                   `json:"page"`
	PerPage int                   `json:"per_page"`
}

// tenantAdminHandler routes /api/v1/tenants*. The router walks the
// path once and dispatches to a focused helper.
func (s *server) tenantAdminHandler(w http.ResponseWriter, r *http.Request) {
	if s.tenantAggregateSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "tenant_admin_unconfigured"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants")
	rest = strings.TrimPrefix(rest, "/")
	switch {
	case rest == "" && r.Method == http.MethodGet:
		s.listTenants(w, r)
	case rest == "" && r.Method == http.MethodPost:
		s.createTenant(w, r)
	default:
		s.dispatchTenant(w, r, rest)
	}
}

// dispatchTenant handles paths under /api/v1/tenants/{id}*.
func (s *server) dispatchTenant(w http.ResponseWriter, r *http.Request, rest string) {
	id, action := splitTenantPath(rest)
	switch {
	case action == "" && r.Method == http.MethodGet:
		s.getTenant(w, r, id)
	case action == "" && r.Method == http.MethodPatch:
		s.updateTenant(w, r, id)
	case action == "suspend" && r.Method == http.MethodPost:
		s.suspendTenant(w, r, id)
	case action == "activate" && r.Method == http.MethodPost:
		s.activateTenant(w, r, id)
	case action == "archive" && r.Method == http.MethodPost:
		s.archiveTenant(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

// splitTenantPath returns (id, action) from the path remainder after
// the /api/v1/tenants/ prefix.
func splitTenantPath(rest string) (string, string) {
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	return id, action
}

func (s *server) listTenants(w http.ResponseWriter, r *http.Request) {
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	tenants, total, err := s.tenantAggregateSvc.List(r.Context(), page, perPage)
	if err != nil {
		s.log.Error("list tenants", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	out := make([]tenantAdminResponse, len(tenants))
	for i, t := range tenants {
		out[i] = toTenantAdminResponse(t)
	}
	writeJSON(w, http.StatusOK, tenantAdminListResponse{Tenants: out, Total: total, Page: page, PerPage: perPage})
}

func (s *server) createTenant(w http.ResponseWriter, r *http.Request) {
	var req tenantAdminCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	t, err := s.tenantAggregateSvc.Create(r.Context(), tenantpkg.CreateTenantInput{
		ID:   tenantpkg.ID(req.ID),
		Slug: req.Slug,
		Name: req.Name,
		Plan: req.Plan,
	})
	if err != nil {
		writeTenantAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTenantAdminResponse(t))
}

func (s *server) getTenant(w http.ResponseWriter, r *http.Request, id string) {
	t, err := s.tenantAggregateSvc.Get(r.Context(), tenantpkg.ID(id))
	if err != nil {
		writeTenantAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTenantAdminResponse(t))
}

func (s *server) updateTenant(w http.ResponseWriter, r *http.Request, id string) {
	var req tenantAdminUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	name, plan := "", ""
	if req.Name != nil {
		name = *req.Name
	}
	if req.Plan != nil {
		plan = *req.Plan
	}
	t, err := s.tenantAggregateSvc.Update(r.Context(), tenantpkg.ID(id), name, plan)
	if err != nil {
		writeTenantAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTenantAdminResponse(t))
}

func (s *server) suspendTenant(w http.ResponseWriter, r *http.Request, id string) {
	t, err := s.tenantAggregateSvc.Suspend(r.Context(), tenantpkg.ID(id))
	if err != nil {
		writeTenantAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTenantAdminResponse(t))
}

func (s *server) activateTenant(w http.ResponseWriter, r *http.Request, id string) {
	t, err := s.tenantAggregateSvc.Activate(r.Context(), tenantpkg.ID(id))
	if err != nil {
		writeTenantAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTenantAdminResponse(t))
}

func (s *server) archiveTenant(w http.ResponseWriter, r *http.Request, id string) {
	t, err := s.tenantAggregateSvc.Archive(r.Context(), tenantpkg.ID(id))
	if err != nil {
		writeTenantAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTenantAdminResponse(t))
}

// writeTenantAdminError translates tenant aggregate sentinel errors
// into HTTP statuses. Keep the table small and explicit.
func writeTenantAdminError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tenantpkg.ErrTenantNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	case errors.Is(err, tenantpkg.ErrTenantSlugAlreadyExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "slug_exists"})
	case errors.Is(err, tenantpkg.ErrTenantSlugInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_slug"})
	case errors.Is(err, tenantpkg.ErrTenantQuotaExceeded):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "quota_exceeded"})
	case errors.Is(err, tenantpkg.ErrInvalidStatusTransition):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_transition"})
	case errors.Is(err, tenantpkg.ErrTenantRequired):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
	}
}

func toTenantAdminResponse(t tenantpkg.Tenant) tenantAdminResponse {
	return tenantAdminResponse{
		ID:        string(t.ID),
		Slug:      t.Slug,
		Name:      t.Name,
		Plan:      t.Plan,
		Status:    string(t.Status),
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// tenantAdminRole gates the /api/v1/tenants endpoints. Listing/reading
// tenants is operator+; creating, updating, and lifecycle transitions
// require admin (super-admin).
func tenantAdminRole(r *http.Request) security.Role {
	if r.Method == http.MethodGet {
		return security.RoleOperator
	}
	return security.RoleAdmin
}

// tenantAdminAuditAction tags mutating tenant requests for audit.
func tenantAdminAuditAction(r *http.Request) auditAction {
	if r.Method == http.MethodGet {
		return auditAction{}
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	switch {
	case strings.HasSuffix(rest, "/suspend"):
		return auditAction{Action: "tenant.suspend", Resource: rest, Mutates: true}
	case strings.HasSuffix(rest, "/activate"):
		return auditAction{Action: "tenant.activate", Resource: rest, Mutates: true}
	case strings.HasSuffix(rest, "/archive"):
		return auditAction{Action: "tenant.archive", Resource: rest, Mutates: true}
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tenants":
		return auditAction{Action: "tenant.create", Resource: "tenant", Mutates: true}
	case r.Method == http.MethodPatch:
		return auditAction{Action: "tenant.update", Resource: rest, Mutates: true}
	default:
		return auditAction{}
	}
}
