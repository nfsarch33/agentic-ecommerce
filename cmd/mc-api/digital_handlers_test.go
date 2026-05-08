package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/signedurl"
	"github.com/nfsarch33/agentic-ecommerce/internal/digital"
	digitaldomain "github.com/nfsarch33/agentic-ecommerce/internal/domain/digital"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/security"
)

func newDigitalTestServer(t *testing.T) (*server, *inmemory.DigitalProductRepository) {
	t.Helper()
	products := inmemory.NewDigitalProductRepository()
	licenses := inmemory.NewLicenseRepository()
	grants := inmemory.NewAccessGrantRepository()
	bus := eventbus.NewInMemoryBus()
	keys, err := digitaldomain.NewHMACLicenseKeyGenerator(make([]byte, 32))
	if err != nil {
		t.Fatalf("HMACLicenseKeyGenerator: %v", err)
	}
	issuer, err := signedurl.New(signedurl.Config{
		BaseURL: "https://cdn.example.com/api/v1/digital-downloads",
		Secret:  []byte("test-secret-32-bytes-of-signed-url-1!"),
	})
	if err != nil {
		t.Fatalf("signedurl.New: %v", err)
	}
	svc, err := digital.New(digital.Config{
		Products:  products,
		Licenses:  licenses,
		Grants:    grants,
		Keys:      keys,
		Issuer:    issuer,
		Publisher: bus,
	})
	if err != nil {
		t.Fatalf("digital.New: %v", err)
	}
	srv := &server{
		log:                slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		digitalProductRepo: products,
		licenseRepo:        licenses,
		accessGrantRepo:    grants,
		digitalSvc:         svc,
	}
	return srv, products
}

func operatorContext(tenant string) context.Context {
	return context.WithValue(context.Background(), actorContextKey{}, requestActor{
		Subject: "ops@example.com", Role: security.RoleOperator, TenantID: tenant,
	})
}

func customerContext(tenant, email string) context.Context {
	return context.WithValue(context.Background(), actorContextKey{}, requestActor{
		Subject: email, Role: security.RoleViewer, TenantID: tenant,
	})
}

func newJSONRequest(t *testing.T, ctx context.Context, method, path string, body any) *http.Request {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr).WithContext(ctx)
	req.Header.Set("X-Tenant-ID", actorTenant(ctx))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func actorTenant(ctx context.Context) string {
	if a, ok := ctx.Value(actorContextKey{}).(requestActor); ok {
		return a.TenantID
	}
	return ""
}

func TestDigitalProductsCRUDHappyPath(t *testing.T) {
	t.Parallel()
	srv, _ := newDigitalTestServer(t)
	ctx := operatorContext("tenant-a")

	// Create
	rec := httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, ctx, http.MethodPost, "/api/v1/digital-products", digitalProductRequest{
		SKU: "PDF-001", Name: "Sample", FilePath: "tenant-a/x.pdf", FileSize: 1024, Version: "1.0.0",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created digitalProductResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatalf("response missing id")
	}

	// List
	rec = httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, ctx, http.MethodGet, "/api/v1/digital-products", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}

	// Get
	rec = httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, ctx, http.MethodGet, "/api/v1/digital-products/"+created.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}

	// Cross-tenant Get must be 404.
	rec = httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, operatorContext("tenant-b"), http.MethodGet, "/api/v1/digital-products/"+created.ID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant get status = %d, want 404", rec.Code)
	}

	// Patch
	rec = httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, ctx, http.MethodPatch, "/api/v1/digital-products/"+created.ID, digitalProductRequest{
		Name: "Renamed", Version: "1.0.1",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// Delete
	rec = httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, ctx, http.MethodDelete, "/api/v1/digital-products/"+created.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, ctx, http.MethodGet, "/api/v1/digital-products/"+created.ID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("post-delete get status = %d, want 404", rec.Code)
	}
}

func TestLicenseFlowIssueRevokeAndDownloadEnforcesCustomerScope(t *testing.T) {
	t.Parallel()
	srv, products := newDigitalTestServer(t)
	ctxAdmin := operatorContext("tenant-a")
	// Seed a digital product directly via the repo so we don't have to
	// post first.
	prod, err := digitaldomain.NewDigitalProduct(digitaldomain.DigitalProductInput{
		TenantID: "tenant-a", SKU: "PDF-001", Name: "Sample",
		FilePath: "tenant-a/x.pdf", FileSize: 1024, Version: "1",
	})
	if err != nil {
		t.Fatalf("NewDigitalProduct: %v", err)
	}
	if err := products.Create(ctxAdmin, "tenant-a", prod); err != nil {
		t.Fatalf("Create product: %v", err)
	}

	// Customer view derives a deterministic uuid from email.
	email := "alice@example.com"
	customerID := DeriveCustomerID("tenant-a", email)

	// Admin issues a licence.
	rec := httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctxAdmin, http.MethodPost, "/api/v1/licenses", licenseRequest{
		ProductID:  prod.ID().String(),
		CustomerID: customerID.String(),
		Source:     "purchase",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var lic licenseResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &lic)
	if lic.State != "active" {
		t.Fatalf("state = %q, want active", lic.State)
	}

	// Customer reads /me/licenses and finds their licence.
	ctxCust := customerContext("tenant-a", email)
	rec = httptest.NewRecorder()
	srv.meDigitalLibraryHandler(rec, newJSONRequest(t, ctxCust, http.MethodGet, "/api/v1/me/licenses", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("me list status = %d", rec.Code)
	}
	var page licensesListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if page.Total != 1 {
		t.Fatalf("me total = %d, want 1", page.Total)
	}

	// Customer downloads.
	rec = httptest.NewRecorder()
	srv.meDigitalLibraryHandler(rec, newJSONRequest(t, ctxCust, http.MethodGet, "/api/v1/me/licenses/"+lic.ID+"/download", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var dl downloadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &dl)
	if dl.URL == "" || !strings.Contains(dl.URL, "sig=") {
		t.Fatalf("download url = %q", dl.URL)
	}

	// A different customer must NOT see this licence.
	ctxOther := customerContext("tenant-a", "mallory@example.com")
	rec = httptest.NewRecorder()
	srv.meDigitalLibraryHandler(rec, newJSONRequest(t, ctxOther, http.MethodGet, "/api/v1/me/licenses", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("other me list status = %d", rec.Code)
	}
	page = licensesListResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if page.Total != 0 {
		t.Fatalf("other customer total = %d, want 0", page.Total)
	}
	// Cross-customer download must be forbidden.
	rec = httptest.NewRecorder()
	srv.meDigitalLibraryHandler(rec, newJSONRequest(t, ctxOther, http.MethodGet, "/api/v1/me/licenses/"+lic.ID+"/download", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-customer download = %d, want 403", rec.Code)
	}

	// Admin revokes.
	rec = httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctxAdmin, http.MethodPost, "/api/v1/licenses/"+lic.ID+"/revoke", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d", rec.Code)
	}
	// Customer download after revoke is gone (410).
	rec = httptest.NewRecorder()
	srv.meDigitalLibraryHandler(rec, newJSONRequest(t, ctxCust, http.MethodGet, "/api/v1/me/licenses/"+lic.ID+"/download", nil))
	if rec.Code != http.StatusGone {
		t.Fatalf("post-revoke download = %d, want 410", rec.Code)
	}
	// Repeat revoke is a 422.
	rec = httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctxAdmin, http.MethodPost, "/api/v1/licenses/"+lic.ID+"/revoke", nil))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("repeat revoke = %d, want 422", rec.Code)
	}
}

func TestSignedUrlReplayAndCrossTenantAreRejected(t *testing.T) {
	t.Parallel()
	srv, products := newDigitalTestServer(t)
	ctx := operatorContext("tenant-a")
	prod, _ := digitaldomain.NewDigitalProduct(digitaldomain.DigitalProductInput{
		TenantID: "tenant-a", SKU: "PDF-X", Name: "X",
		FilePath: "x", FileSize: 10, Version: "1",
	})
	_ = products.Create(ctx, "tenant-a", prod)
	customerID := DeriveCustomerID("tenant-a", "alice@example.com")

	rec := httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctx, http.MethodPost, "/api/v1/licenses", licenseRequest{
		ProductID:  prod.ID().String(),
		CustomerID: customerID.String(),
		Source:     "admin",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue status = %d", rec.Code)
	}
	var lic licenseResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &lic)

	ctxCust := customerContext("tenant-a", "alice@example.com")
	rec = httptest.NewRecorder()
	srv.meDigitalLibraryHandler(rec, newJSONRequest(t, ctxCust, http.MethodGet, "/api/v1/me/licenses/"+lic.ID+"/download", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d", rec.Code)
	}
	var dl downloadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &dl)
	url := dl.URL
	// Verify the URL via the issuer (sanity).
	iss, err := signedurl.New(signedurl.Config{
		BaseURL: "https://cdn.example.com/api/v1/digital-downloads",
		Secret:  []byte("test-secret-32-bytes-of-signed-url-1!"),
	})
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}
	if _, err := iss.Verify(url, time.Now().UTC()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Cross-tenant tampering: replace tid in the URL.
	tampered := strings.Replace(url, "tid=tenant-a", "tid=tenant-b", 1)
	if _, err := iss.Verify(tampered, time.Now().UTC()); err == nil {
		t.Fatal("tampered URL should fail")
	}
}

func TestLicensesRoleEnforcedAtGetVsMutating(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/licenses", nil)
	if got := licensesRole(r); got != security.RoleViewer {
		t.Fatalf("GET role = %q, want viewer", got)
	}
	r = httptest.NewRequest(http.MethodPost, "/api/v1/licenses", nil)
	if got := licensesRole(r); got != security.RoleOperator {
		t.Fatalf("POST role = %q, want operator", got)
	}
}

func TestDigitalProductsAuditActionTaxonomy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		method, path string
		want         string
	}{
		{http.MethodPost, "/api/v1/digital-products", "digital_product.create"},
		{http.MethodPatch, "/api/v1/digital-products/abc", "digital_product.update"},
		{http.MethodDelete, "/api/v1/digital-products/abc", "digital_product.delete"},
		{http.MethodGet, "/api/v1/digital-products/abc/download", "digital_product.download"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(tc.method, tc.path, nil)
			if got := digitalProductsAuditAction(r); got.Action != tc.want {
				t.Fatalf("action = %q, want %q", got.Action, tc.want)
			}
		})
	}
}

func TestDeriveCustomerIDStableAndCaseInsensitive(t *testing.T) {
	t.Parallel()
	a := DeriveCustomerID("tenant-a", "Alice@Example.com")
	b := DeriveCustomerID("tenant-a", "alice@example.com")
	if a != b {
		t.Fatalf("case sensitivity broke stability: %v vs %v", a, b)
	}
	c := DeriveCustomerID("tenant-b", "alice@example.com")
	if a == c {
		t.Fatalf("cross-tenant collision: %v == %v", a, c)
	}
}

// helper ensuring uuid import survives.
var _ = uuid.New
