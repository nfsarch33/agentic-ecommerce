package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
)

func newMarketplaceTestServer(t *testing.T) *server {
	t.Helper()
	cat := inmemory.NewMarketplaceCatalog()
	if err := cat.RegisterManifest(context.Background(), marketplace.Manifest{
		Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe",
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	svc, err := marketplace.NewService(marketplace.ServiceConfig{
		Catalog:       cat,
		Installations: inmemory.NewMarketplaceInstallations(),
		Subscriptions: inmemory.NewMarketplaceSubscriptions(),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return &server{marketplace: svc}
}

func doMarketplaceRequest(t *testing.T, srv *server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	srv.marketplaceHandler(rec, req)
	return rec
}

func TestMarketplaceListPlugins(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodGet, "/api/v1/marketplace/plugins", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp manifestListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("total = %d, want 1", resp.Total)
	}
}

func TestMarketplaceGetPluginNotFound(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodGet, "/api/v1/marketplace/plugins/ghost", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMarketplaceLifecycleE2E(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	// Install
	rec := doMarketplaceRequest(t, srv, http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/install", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("install status = %d, body=%s", rec.Code, rec.Body.String())
	}
	// Activate
	rec = doMarketplaceRequest(t, srv, http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/activate", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("activate status = %d", rec.Code)
	}
	var ins installationResponse
	if err := json.NewDecoder(rec.Body).Decode(&ins); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ins.State != "active" {
		t.Fatalf("state = %s, want active", ins.State)
	}
	// Deactivate
	rec = doMarketplaceRequest(t, srv, http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/deactivate", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate status = %d", rec.Code)
	}
	// Settings update
	body, _ := json.Marshal(map[string]any{"webhook": "https://example.com"})
	rec = doMarketplaceRequest(t, srv, http.MethodPatch, "/api/v1/marketplace/installations/stripe-payments/settings", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings PATCH status = %d", rec.Code)
	}
	// Settings read
	rec = doMarketplaceRequest(t, srv, http.MethodGet, "/api/v1/marketplace/installations/stripe-payments/settings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings GET status = %d", rec.Code)
	}
	var settings settingsResponse
	if err := json.NewDecoder(rec.Body).Decode(&settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if settings.Settings["webhook"] != "https://example.com" {
		t.Fatalf("settings round-trip failed: %v", settings.Settings)
	}
	// Uninstall
	rec = doMarketplaceRequest(t, srv, http.MethodDelete, "/api/v1/marketplace/plugins/stripe-payments", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("uninstall status = %d", rec.Code)
	}
}

func TestMarketplaceTenantIsolation(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	// Tenant A installs.
	rec := doMarketplaceRequest(t, srv, http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/install", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("tenant A install: %d", rec.Code)
	}
	// Tenant B activate must fail because their installation does not exist.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/activate", nil)
	req.Header.Set("X-Tenant-ID", "tenant-b")
	rec = httptest.NewRecorder()
	srv.marketplaceHandler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tenant B should see 404, got %d", rec.Code)
	}
}

func TestMarketplaceInvalidSlugRejected(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodGet, "/api/v1/marketplace/plugins/INVALID", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestMarketplaceMissingTenantHeaderRejected(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/install", nil)
	srv.marketplaceHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestMarketplaceUnconfigured(t *testing.T) {
	t.Parallel()
	srv := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/plugins", nil)
	srv.marketplaceHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestMarketplaceMethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodPut, "/api/v1/marketplace/plugins", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestMarketplaceInstallationsBadActionRoutes404(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodGet, "/api/v1/marketplace/installations/stripe-payments/garbage", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestMarketplaceInstallationsInvalidSlugRejected(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodGet, "/api/v1/marketplace/installations/INVALID/settings", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestMarketplaceInstallationsSettingsMethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	// Install first so the path is valid.
	rec := doMarketplaceRequest(t, srv, http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/install", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("install: %d", rec.Code)
	}
	rec = doMarketplaceRequest(t, srv, http.MethodPost, "/api/v1/marketplace/installations/stripe-payments/settings", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestMarketplaceSettingsRequiresInstallation(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodGet, "/api/v1/marketplace/installations/stripe-payments/settings", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	rec = doMarketplaceRequest(t, srv, http.MethodPatch, "/api/v1/marketplace/installations/stripe-payments/settings", []byte("{}"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestMarketplaceSettingsBadJSON(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	if rec := doMarketplaceRequest(t, srv, http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/install", nil); rec.Code != http.StatusCreated {
		t.Fatalf("install: %d", rec.Code)
	}
	rec := doMarketplaceRequest(t, srv, http.MethodPatch, "/api/v1/marketplace/installations/stripe-payments/settings", []byte("not json"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestMarketplaceUninstallInvalidSlug(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodDelete, "/api/v1/marketplace/plugins/INVALID", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestBuildMarketplaceServiceSeedsCatalog(t *testing.T) {
	t.Parallel()
	svc, err := buildMarketplaceService()
	if err != nil {
		t.Fatalf("buildMarketplaceService: %v", err)
	}
	manifests, total, err := svc.Catalog().ListManifests(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("ListManifests: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 seed manifests, got %d", total)
	}
	if len(manifests) != 3 {
		t.Fatalf("manifests len = %d", len(manifests))
	}
}

func TestMarketplaceAuditActionAllPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		method  string
		path    string
		want    string
		mutates bool
	}{
		{http.MethodGet, "/api/v1/marketplace/plugins", "", false},
		{http.MethodPost, "/api/v1/marketplace/plugins/x/install", "marketplace.install", true},
		{http.MethodPost, "/api/v1/marketplace/plugins/x/activate", "marketplace.activate", true},
		{http.MethodPost, "/api/v1/marketplace/plugins/x/deactivate", "marketplace.deactivate", true},
		{http.MethodPatch, "/api/v1/marketplace/installations/x/settings", "marketplace.settings_update", true},
		{http.MethodDelete, "/api/v1/marketplace/plugins/x", "marketplace.uninstall", true},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			got := marketplaceAuditAction(req)
			if got.Mutates != tc.mutates {
				t.Fatalf("mutates = %v, want %v", got.Mutates, tc.mutates)
			}
			if got.Action != tc.want {
				t.Fatalf("action = %q, want %q", got.Action, tc.want)
			}
		})
	}
}

func TestMarketplaceUninstallNotFound(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodDelete, "/api/v1/marketplace/plugins/stripe-payments", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown installation, got %d", rec.Code)
	}
}

func TestMarketplaceActivateRequiresInstall(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/activate", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestMarketplaceDeactivateRequiresInstall(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	rec := doMarketplaceRequest(t, srv, http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/deactivate", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestMarketplaceInstallReusesExistingManifest(t *testing.T) {
	t.Parallel()
	srv := newMarketplaceTestServer(t)
	// Install for tenant-a, then attempt with tenant-b which uses a fresh slot.
	rec := doMarketplaceRequest(t, srv, http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/install", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("install A: %d", rec.Code)
	}
	// tenant-b: separate header
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins/stripe-payments/install", nil)
	req.Header.Set("X-Tenant-ID", "tenant-b")
	rec = httptest.NewRecorder()
	srv.marketplaceHandler(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("install B: %d", rec.Code)
	}
}

func TestToManifestResponseHandlesEmptyEvents(t *testing.T) {
	t.Parallel()
	got := toManifestResponse(marketplace.Manifest{Slug: "x", Name: "X", Version: "1.0.0", Vendor: "V"})
	if got.Slug != "x" {
		t.Fatalf("slug = %s", got.Slug)
	}
	if got.EventSubscriptions == nil {
		t.Fatalf("EventSubscriptions should be empty slice, not nil")
	}
}

func TestToInstallationResponseRoundTrip(t *testing.T) {
	t.Parallel()
	in := marketplace.Installation{
		TenantID: "t", Slug: "s", InstalledVersion: "1.0.0", State: marketplace.StateActive,
		InstalledAt: "2026-05-08T10:00:00Z", ActivatedAt: "2026-05-08T10:01:00Z", UpdatedAt: "2026-05-08T10:01:00Z",
	}
	got := toInstallationResponse(in)
	if got.State != "active" {
		t.Fatalf("state = %s", got.State)
	}
	if got.ActivatedAt != in.ActivatedAt {
		t.Fatalf("activated_at = %s", got.ActivatedAt)
	}
}

func TestMarketplaceErrorTranslation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want int
	}{
		{marketplace.ErrPluginNotFound, http.StatusNotFound},
		{marketplace.ErrPluginAlreadyInstalled, http.StatusConflict},
		{marketplace.ErrSlugInvalid, http.StatusBadRequest},
		{marketplace.ErrSlugAlreadyExists, http.StatusConflict},
		{marketplace.ErrInvalidTransition, http.StatusUnprocessableEntity},
		{marketplace.ErrSemverConflict, http.StatusUnprocessableEntity},
		{marketplace.ErrSemverInvalid, http.StatusUnprocessableEntity},
		{marketplace.ErrSandboxBudgetExceeded, http.StatusTooManyRequests},
		{marketplace.ErrUnknownEvent, http.StatusUnprocessableEntity},
		{marketplace.ErrManifestInvalid, http.StatusUnprocessableEntity},
		{marketplace.ErrCrossTenantAccess, http.StatusForbidden},
		{marketplace.ErrDependencyCycle, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeMarketplaceError(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("err=%v got status %d, want %d", tc.err, rec.Code, tc.want)
			}
		})
	}
}

func TestMarketplaceRoleAndAudit(t *testing.T) {
	t.Parallel()
	get := httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/plugins", nil)
	if role := marketplaceRole(get); role != "viewer" {
		t.Fatalf("GET role = %s, want viewer", role)
	}
	post := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins/x/install", nil)
	if role := marketplaceRole(post); role != "operator" {
		t.Fatalf("POST role = %s, want operator", role)
	}
	if action := marketplaceAuditAction(post); !action.Mutates || !strings.Contains(action.Action, "install") {
		t.Fatalf("install audit = %+v", action)
	}
	if action := marketplaceAuditAction(httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/plugins", nil)); action.Mutates {
		t.Fatalf("GET should not mutate audit")
	}
}
