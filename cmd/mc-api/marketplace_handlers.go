package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
	"github.com/nfsarch33/agentic-ecommerce/internal/security"
	tenantpkg "github.com/nfsarch33/agentic-ecommerce/internal/tenant"
)

// manifestResponse is the wire shape returned by /api/v1/marketplace/plugins.
type manifestResponse struct {
	Slug               string                       `json:"slug"`
	Name               string                       `json:"name"`
	Version            string                       `json:"version"`
	Vendor             string                       `json:"vendor"`
	Description        string                       `json:"description,omitempty"`
	Category           string                       `json:"category,omitempty"`
	HomepageURL        string                       `json:"homepage_url,omitempty"`
	EventSubscriptions []string                     `json:"event_subscriptions,omitempty"`
	Permissions        []string                     `json:"permissions,omitempty"`
	Dependencies       []manifestDependencyResponse `json:"dependencies,omitempty"`
}

type manifestDependencyResponse struct {
	Slug       string `json:"slug"`
	Constraint string `json:"constraint"`
}

type manifestListResponse struct {
	Plugins []manifestResponse `json:"plugins"`
	Total   int                `json:"total"`
	Page    int                `json:"page"`
	PerPage int                `json:"per_page"`
}

// installationResponse is the wire shape for per-tenant installation rows.
type installationResponse struct {
	TenantID         string `json:"tenant_id"`
	Slug             string `json:"slug"`
	InstalledVersion string `json:"installed_version"`
	State            string `json:"state"`
	InstalledAt      string `json:"installed_at"`
	ActivatedAt      string `json:"activated_at,omitempty"`
	UpdatedAt        string `json:"updated_at"`
}

type installationListResponse struct {
	Installations []installationResponse `json:"installations"`
	Total         int                    `json:"total"`
	Page          int                    `json:"page"`
	PerPage       int                    `json:"per_page"`
}

// marketplaceHandler routes /api/v1/marketplace/*. The router walks the
// path once and dispatches to a focused helper to keep cyclomatic
// complexity below 10 per function.
func (s *server) marketplaceHandler(w http.ResponseWriter, r *http.Request) {
	if s.marketplace == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "marketplace_unconfigured"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/marketplace")
	rest = strings.TrimPrefix(rest, "/")
	switch {
	case rest == "plugins" && r.Method == http.MethodGet:
		s.listMarketplacePlugins(w, r)
	case strings.HasPrefix(rest, "plugins/"):
		s.dispatchMarketplacePlugin(w, r, strings.TrimPrefix(rest, "plugins/"))
	case strings.HasPrefix(rest, "installations/"):
		s.dispatchMarketplaceInstallation(w, r, strings.TrimPrefix(rest, "installations/"))
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

// dispatchMarketplacePlugin handles all /api/v1/marketplace/plugins/{slug}* paths.
func (s *server) dispatchMarketplacePlugin(w http.ResponseWriter, r *http.Request, rest string) {
	slug, action := splitPluginPath(rest)
	if !marketplace.IsValidSlug(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_slug"})
		return
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		s.getMarketplacePlugin(w, r, slug)
	case action == "" && r.Method == http.MethodDelete:
		s.uninstallMarketplacePlugin(w, r, slug)
	case action == "install" && r.Method == http.MethodPost:
		s.installMarketplacePlugin(w, r, slug)
	case action == "activate" && r.Method == http.MethodPost:
		s.activateMarketplacePlugin(w, r, slug)
	case action == "deactivate" && r.Method == http.MethodPost:
		s.deactivateMarketplacePlugin(w, r, slug)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

// dispatchMarketplaceInstallation routes /api/v1/marketplace/installations/{slug}*.
func (s *server) dispatchMarketplaceInstallation(w http.ResponseWriter, r *http.Request, rest string) {
	slug, action := splitPluginPath(rest)
	if !marketplace.IsValidSlug(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_slug"})
		return
	}
	if action != "settings" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getInstallationSettings(w, r, slug)
	case http.MethodPatch:
		s.updateInstallationSettings(w, r, slug)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

// splitPluginPath extracts the slug and the optional action from the
// path remainder after the marketplace prefix.
func splitPluginPath(rest string) (string, string) {
	parts := strings.SplitN(rest, "/", 2)
	slug := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	return slug, action
}

func (s *server) listMarketplacePlugins(w http.ResponseWriter, r *http.Request) {
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	manifests, total, err := s.marketplace.Catalog().ListManifests(r.Context(), page, perPage)
	if err != nil {
		s.log.Error("list marketplace plugins", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	out := make([]manifestResponse, len(manifests))
	for i, m := range manifests {
		out[i] = toManifestResponse(m)
	}
	writeJSON(w, http.StatusOK, manifestListResponse{Plugins: out, Total: total, Page: page, PerPage: perPage})
}

func (s *server) getMarketplacePlugin(w http.ResponseWriter, r *http.Request, slug string) {
	manifest, err := s.marketplace.Catalog().GetManifest(r.Context(), slug)
	if err != nil {
		writeMarketplaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toManifestResponse(manifest))
}

func (s *server) installMarketplacePlugin(w http.ResponseWriter, r *http.Request, slug string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	manifest, err := s.marketplace.Catalog().GetManifest(r.Context(), slug)
	if err != nil {
		writeMarketplaceError(w, err)
		return
	}
	row, err := s.marketplace.Install(r.Context(), tenantID, manifest)
	if err != nil {
		writeMarketplaceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toInstallationResponse(row))
}

func (s *server) activateMarketplacePlugin(w http.ResponseWriter, r *http.Request, slug string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	row, err := s.marketplace.Activate(r.Context(), tenantID, slug)
	if err != nil {
		writeMarketplaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toInstallationResponse(row))
}

func (s *server) deactivateMarketplacePlugin(w http.ResponseWriter, r *http.Request, slug string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	row, err := s.marketplace.Deactivate(r.Context(), tenantID, slug)
	if err != nil {
		writeMarketplaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toInstallationResponse(row))
}

func (s *server) uninstallMarketplacePlugin(w http.ResponseWriter, r *http.Request, slug string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	if err := s.marketplace.Uninstall(r.Context(), tenantID, slug); err != nil {
		writeMarketplaceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// settingsResponse is a thin wrapper exposing the per-(tenant, plugin)
// settings blob the registry stores. v2.4.0 keeps this as raw JSON to
// keep the surface small; v2.5.0 will type-narrow per known plugin.
type settingsResponse struct {
	Settings map[string]any `json:"settings"`
}

func (s *server) getInstallationSettings(w http.ResponseWriter, r *http.Request, slug string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	if _, err := s.marketplace.Get(r.Context(), tenantID, slug); err != nil {
		writeMarketplaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse{Settings: s.marketplace.Settings(tenantID, slug)})
}

func (s *server) updateInstallationSettings(w http.ResponseWriter, r *http.Request, slug string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	if _, err := s.marketplace.Get(r.Context(), tenantID, slug); err != nil {
		writeMarketplaceError(w, err)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	s.marketplace.SetSettings(tenantID, slug, body)
	writeJSON(w, http.StatusOK, settingsResponse{Settings: body})
}

// writeMarketplaceError translates marketplace + tenant sentinel
// errors into HTTP statuses. Keep the table small and explicit.
func writeMarketplaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, marketplace.ErrPluginNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	case errors.Is(err, marketplace.ErrPluginAlreadyInstalled):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already_installed"})
	case errors.Is(err, marketplace.ErrSlugInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_slug"})
	case errors.Is(err, marketplace.ErrSlugAlreadyExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "slug_exists"})
	case errors.Is(err, marketplace.ErrInvalidTransition):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_transition"})
	case errors.Is(err, marketplace.ErrSemverConflict):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "semver_conflict"})
	case errors.Is(err, marketplace.ErrSemverInvalid):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_semver"})
	case errors.Is(err, marketplace.ErrSandboxBudgetExceeded):
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "sandbox_budget_exceeded"})
	case errors.Is(err, marketplace.ErrUnknownEvent):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "unknown_event"})
	case errors.Is(err, marketplace.ErrManifestInvalid):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_manifest"})
	case errors.Is(err, marketplace.ErrCrossTenantAccess):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross_tenant_access"})
	case errors.Is(err, marketplace.ErrDependencyCycle):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "dependency_cycle"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
	}
}

func toManifestResponse(m marketplace.Manifest) manifestResponse {
	events := make([]string, len(m.EventSubscriptions))
	for i, e := range m.EventSubscriptions {
		events[i] = string(e)
	}
	perms := make([]string, len(m.Permissions))
	for i, p := range m.Permissions {
		perms[i] = string(p)
	}
	deps := make([]manifestDependencyResponse, len(m.Dependencies))
	for i, d := range m.Dependencies {
		deps[i] = manifestDependencyResponse{Slug: d.Slug, Constraint: d.Constraint}
	}
	return manifestResponse{
		Slug:               m.Slug,
		Name:               m.Name,
		Version:            m.Version,
		Vendor:             m.Vendor,
		Description:        m.Description,
		Category:           m.Category,
		HomepageURL:        m.HomepageURL,
		EventSubscriptions: events,
		Permissions:        perms,
		Dependencies:       deps,
	}
}

func toInstallationResponse(ins marketplace.Installation) installationResponse {
	return installationResponse{
		TenantID:         ins.TenantID,
		Slug:             ins.Slug,
		InstalledVersion: ins.InstalledVersion,
		State:            string(ins.State),
		InstalledAt:      ins.InstalledAt,
		ActivatedAt:      ins.ActivatedAt,
		UpdatedAt:        ins.UpdatedAt,
	}
}

// marketplaceRole returns the RBAC role required for a marketplace
// request. Listing the catalogue is read-only; install/activate/etc.
// require operator. This mirrors the digital handlers' pattern.
func marketplaceRole(r *http.Request) security.Role {
	if r.Method == http.MethodGet {
		return security.RoleViewer
	}
	return security.RoleOperator
}

// marketplaceAuditAction tags mutating marketplace requests for audit.
func marketplaceAuditAction(r *http.Request) auditAction {
	if r.Method == http.MethodGet {
		return auditAction{}
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/marketplace/")
	switch {
	case strings.HasSuffix(rest, "/install"):
		return auditAction{Action: "marketplace.install", Resource: rest, Mutates: true}
	case strings.HasSuffix(rest, "/activate"):
		return auditAction{Action: "marketplace.activate", Resource: rest, Mutates: true}
	case strings.HasSuffix(rest, "/deactivate"):
		return auditAction{Action: "marketplace.deactivate", Resource: rest, Mutates: true}
	case strings.HasSuffix(rest, "/settings"):
		return auditAction{Action: "marketplace.settings_update", Resource: rest, Mutates: true}
	case r.Method == http.MethodDelete:
		return auditAction{Action: "marketplace.uninstall", Resource: rest, Mutates: true}
	default:
		return auditAction{}
	}
}

// _ keeps the tenantpkg import alive when handler-only files reference it.
var _ tenantpkg.ID
