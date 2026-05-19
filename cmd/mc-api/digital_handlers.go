package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/digital"
	digitaldomain "github.com/nfsarch33/helixon-ec/internal/domain/digital"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"github.com/nfsarch33/helixon-ec/internal/security"
)

// digitalProductRequest is the JSON body for POST/PATCH /digital-products.
type digitalProductRequest struct {
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	FilePath    string `json:"file_path"`
	FileSize    int64  `json:"file_size"`
	ContentType string `json:"content_type,omitempty"`
	Checksum    string `json:"checksum,omitempty"`
	Version     string `json:"version"`
}

// digitalProductResponse is the JSON shape returned for digital products.
type digitalProductResponse struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	SKU         string    `json:"sku"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	FilePath    string    `json:"file_path"`
	FileSize    int64     `json:"file_size"`
	ContentType string    `json:"content_type,omitempty"`
	Checksum    string    `json:"checksum,omitempty"`
	Version     string    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type digitalProductsListResponse struct {
	Products []digitalProductResponse `json:"products"`
	Total    int                      `json:"total"`
	Page     int                      `json:"page"`
	PerPage  int                      `json:"per_page"`
}

// licenseRequest is the JSON body for POST /licenses.
type licenseRequest struct {
	ProductID      string `json:"product_id"`
	CustomerID     string `json:"customer_id"`
	Source         string `json:"source,omitempty"`
	MaxActivations int    `json:"max_activations,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
}

// licenseResponse is the JSON shape returned for licences.
type licenseResponse struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	ProductID      string    `json:"product_id"`
	CustomerID     string    `json:"customer_id"`
	Key            string    `json:"key"`
	State          string    `json:"state"`
	IssuedAt       time.Time `json:"issued_at"`
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
	MaxActivations int       `json:"max_activations"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type licensesListResponse struct {
	Licenses []licenseResponse `json:"licenses"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PerPage  int               `json:"per_page"`
}

type downloadResponse struct {
	URL         string    `json:"url"`
	ExpiresAt   time.Time `json:"expires_at"`
	UsesAllowed int       `json:"uses_allowed"`
}

// digitalProductsHandler routes /api/v1/digital-products*.
func (s *server) digitalProductsHandler(w http.ResponseWriter, r *http.Request) {
	if s.digitalSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "digital_unconfigured"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/digital-products")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" && r.Method == http.MethodGet:
		s.listDigitalProducts(w, r)
	case path == "" && r.Method == http.MethodPost:
		s.createDigitalProduct(w, r)
	case strings.HasSuffix(path, "/download") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(path, "/download")
		s.adminDigitalProductDownload(w, r, id)
	case path != "" && r.Method == http.MethodGet:
		s.getDigitalProduct(w, r, path)
	case path != "" && r.Method == http.MethodPatch:
		s.updateDigitalProduct(w, r, path)
	case path != "" && r.Method == http.MethodDelete:
		s.deleteDigitalProduct(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

// licensesHandler routes /api/v1/licenses*.
func (s *server) licensesHandler(w http.ResponseWriter, r *http.Request) {
	if s.digitalSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "digital_unconfigured"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/licenses")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" && r.Method == http.MethodGet:
		s.listLicenses(w, r)
	case path == "" && r.Method == http.MethodPost:
		s.createLicense(w, r)
	case strings.HasSuffix(path, "/revoke") && r.Method == http.MethodPost:
		s.revokeLicense(w, r, strings.TrimSuffix(path, "/revoke"))
	case path != "" && r.Method == http.MethodGet:
		s.getLicense(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

// meDigitalLibraryHandler routes /api/v1/me/licenses*.
func (s *server) meDigitalLibraryHandler(w http.ResponseWriter, r *http.Request) {
	if s.digitalSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "digital_unconfigured"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/me/licenses")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" && r.Method == http.MethodGet:
		s.listMyLicenses(w, r)
	case strings.HasSuffix(path, "/download") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(path, "/download")
		s.customerLicenseDownload(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) listDigitalProducts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	list, err := s.digitalProductRepo.List(r.Context(), tenantID, page, perPage)
	if err != nil {
		s.log.Error("list digital products", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	products := make([]digitalProductResponse, len(list.Products))
	for i, p := range list.Products {
		products[i] = toDigitalProductResponse(p)
	}
	writeJSON(w, http.StatusOK, digitalProductsListResponse{
		Products: products, Total: list.Total, Page: page, PerPage: perPage,
	})
}

func (s *server) getDigitalProduct(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	product, err := s.digitalProductRepo.Get(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, port.ErrDigitalProductNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get digital product", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, toDigitalProductResponse(product))
}

func (s *server) createDigitalProduct(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	var req digitalProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	product, err := digitaldomain.NewDigitalProduct(digitaldomain.DigitalProductInput{
		TenantID:    tenantID,
		SKU:         req.SKU,
		Name:        req.Name,
		Description: req.Description,
		FilePath:    req.FilePath,
		FileSize:    req.FileSize,
		ContentType: req.ContentType,
		Checksum:    req.Checksum,
		Version:     req.Version,
	})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if err := s.digitalProductRepo.Create(r.Context(), tenantID, product); err != nil {
		s.log.Error("create digital product", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusCreated, toDigitalProductResponse(product))
}

func (s *server) updateDigitalProduct(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	existing, err := s.digitalProductRepo.Get(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, port.ErrDigitalProductNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get digital product for update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	var req digitalProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if err := existing.Update(digitaldomain.DigitalProductInput{
		Name:        req.Name,
		Description: req.Description,
		FilePath:    req.FilePath,
		FileSize:    req.FileSize,
		ContentType: req.ContentType,
		Checksum:    req.Checksum,
		Version:     req.Version,
	}, time.Now().UTC()); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if err := s.digitalProductRepo.Update(r.Context(), tenantID, existing); err != nil {
		s.log.Error("update digital product", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, toDigitalProductResponse(existing))
}

func (s *server) deleteDigitalProduct(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	if err := s.digitalProductRepo.Delete(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, port.ErrDigitalProductNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("delete digital product", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminDigitalProductDownload mints a download URL on behalf of an
// admin testing the storefront flow. Customers use the /me/licenses
// endpoint instead.
func (s *server) adminDigitalProductDownload(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	productID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	customerStr := r.URL.Query().Get("customer_id")
	customerID, err := uuid.Parse(customerStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_customer_id"})
		return
	}
	grant, err := s.accessGrantRepo.GetByCustomerProduct(r.Context(), tenantID, customerID, productID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_granted"})
		return
	}
	out, _, err := s.digitalSvc.IssueDownload(r.Context(), tenantID, grant.LicenseID(), uuid.Nil, downloadTokenTTL, downloadTokenUses)
	if err != nil {
		s.log.Error("admin download token", "error", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, downloadResponse{
		URL: out.URL, ExpiresAt: out.Token.ExpiresAt(), UsesAllowed: out.Token.UsesAllowed(),
	})
}

func (s *server) listLicenses(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	list, err := s.licenseRepo.List(r.Context(), tenantID, page, perPage)
	if err != nil {
		s.log.Error("list licenses", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	licenses := make([]licenseResponse, len(list.Licenses))
	for i, lic := range list.Licenses {
		licenses[i] = toLicenseResponse(lic)
	}
	writeJSON(w, http.StatusOK, licensesListResponse{
		Licenses: licenses, Total: list.Total, Page: page, PerPage: perPage,
	})
}

func (s *server) getLicense(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	lic, err := s.licenseRepo.Get(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, port.ErrLicenseNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get license", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, toLicenseResponse(lic))
}

func (s *server) createLicense(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	var req licenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_product_id"})
		return
	}
	customerID, err := uuid.Parse(req.CustomerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_customer_id"})
		return
	}
	source := digitaldomain.SourceAdmin
	if req.Source != "" {
		s2, parseErr := digitaldomain.ParseSource(req.Source)
		if parseErr != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_source"})
			return
		}
		source = s2
	}
	var expires time.Time
	if req.ExpiresAt != "" {
		t, perr := time.Parse(time.RFC3339, req.ExpiresAt)
		if perr != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_expires_at"})
			return
		}
		expires = t
	}
	res, err := s.digitalSvc.IssueLicense(r.Context(), digital.IssueLicenseRequest{
		TenantID:       tenantID,
		CustomerID:     customerID,
		ProductID:      productID,
		Source:         source,
		MaxActivations: req.MaxActivations,
		ExpiresAt:      expires,
	})
	if err != nil {
		if errors.Is(err, port.ErrDigitalProductNotFound) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "product_not_found"})
			return
		}
		s.log.Error("issue license", "error", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, toLicenseResponse(res.License))
}

func (s *server) revokeLicense(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	lic, err := s.digitalSvc.RevokeLicense(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, port.ErrLicenseNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		if errors.Is(err, digitaldomain.ErrInvalidTransition) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_transition"})
			return
		}
		s.log.Error("revoke license", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, toLicenseResponse(lic))
}

func (s *server) listMyLicenses(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	customerID, ok := s.customerOrFail(w, r)
	if !ok {
		return
	}
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	list, err := s.licenseRepo.ListByCustomer(r.Context(), tenantID, customerID, page, perPage)
	if err != nil {
		s.log.Error("list my licenses", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	licenses := make([]licenseResponse, len(list.Licenses))
	for i, lic := range list.Licenses {
		licenses[i] = toLicenseResponse(lic)
	}
	writeJSON(w, http.StatusOK, licensesListResponse{
		Licenses: licenses, Total: list.Total, Page: page, PerPage: perPage,
	})
}

func (s *server) customerLicenseDownload(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	customerID, ok := s.customerOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	out, _, err := s.digitalSvc.IssueDownload(r.Context(), tenantID, id, customerID, downloadTokenTTL, downloadTokenUses)
	if err != nil {
		switch {
		case errors.Is(err, port.ErrLicenseNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		case errors.Is(err, digitaldomain.ErrTenantMismatch):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		case errors.Is(err, digitaldomain.ErrLicenseRevoked) || errors.Is(err, digitaldomain.ErrLicenseExpired):
			writeJSON(w, http.StatusGone, map[string]string{"error": err.Error()})
		default:
			s.log.Error("customer download token", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		}
		return
	}
	writeJSON(w, http.StatusOK, downloadResponse{
		URL: out.URL, ExpiresAt: out.Token.ExpiresAt(), UsesAllowed: out.Token.UsesAllowed(),
	})
}

func toDigitalProductResponse(p digitaldomain.DigitalProduct) digitalProductResponse {
	return digitalProductResponse{
		ID:          p.ID().String(),
		TenantID:    p.TenantID(),
		SKU:         p.SKU(),
		Name:        p.Name(),
		Description: p.Description(),
		FilePath:    p.FilePath(),
		FileSize:    p.FileSize(),
		ContentType: p.ContentType(),
		Checksum:    p.Checksum(),
		Version:     p.Version(),
		CreatedAt:   p.CreatedAt(),
		UpdatedAt:   p.UpdatedAt(),
	}
}

func toLicenseResponse(lic digitaldomain.License) licenseResponse {
	resp := licenseResponse{
		ID:             lic.ID().String(),
		TenantID:       lic.TenantID(),
		ProductID:      lic.ProductID().String(),
		CustomerID:     lic.CustomerID().String(),
		Key:            lic.Key(),
		State:          string(lic.State()),
		IssuedAt:       lic.IssuedAt(),
		MaxActivations: lic.MaxActivations(),
		UpdatedAt:      lic.UpdatedAt(),
	}
	if !lic.ExpiresAt().IsZero() {
		resp.ExpiresAt = lic.ExpiresAt()
	}
	return resp
}

// downloadTokenTTL and downloadTokenUses are tunable defaults. The
// values match the v2.3.0 plan: 5 minute window, 3 uses, generous
// enough to absorb retries on flaky networks but tight enough to limit
// blast radius from leaked URLs.
const (
	downloadTokenTTL  = 5 * time.Minute
	downloadTokenUses = 3
)

// digitalProductsRole / licensesRole / meDigitalAuditAction track the
// RBAC + audit metadata for the digital endpoints. Admin endpoints
// require operator+; the /me endpoints allow any authenticated viewer
// because the handler scopes by the actor's derived customer id.
func digitalProductsRole(r *http.Request) security.Role {
	if r.Method == http.MethodGet {
		return security.RoleViewer
	}
	return security.RoleOperator
}

func licensesRole(r *http.Request) security.Role {
	if r.Method == http.MethodGet {
		return security.RoleViewer
	}
	return security.RoleOperator
}

func digitalProductsAuditAction(r *http.Request) auditAction {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/digital-products"), "/")
	switch {
	case path == "" && r.Method == http.MethodPost:
		return auditAction{Action: "digital_product.create", Resource: "digital-product", Mutates: true}
	case path != "" && r.Method == http.MethodPatch:
		return auditAction{Action: "digital_product.update", Resource: path, Mutates: true}
	case path != "" && r.Method == http.MethodDelete:
		return auditAction{Action: "digital_product.delete", Resource: path, Mutates: true}
	case strings.HasSuffix(path, "/download") && r.Method == http.MethodGet:
		return auditAction{Action: "digital_product.download", Resource: strings.TrimSuffix(path, "/download"), Mutates: false}
	default:
		return auditAction{}
	}
}

func licensesAuditAction(r *http.Request) auditAction {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/licenses"), "/")
	switch {
	case path == "" && r.Method == http.MethodPost:
		return auditAction{Action: "license.create", Resource: "license", Mutates: true}
	case strings.HasSuffix(path, "/revoke") && r.Method == http.MethodPost:
		return auditAction{Action: "license.revoke", Resource: strings.TrimSuffix(path, "/revoke"), Mutates: true}
	default:
		return auditAction{}
	}
}

func meDigitalAuditAction(r *http.Request) auditAction {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/me/licenses"), "/")
	if strings.HasSuffix(path, "/download") {
		return auditAction{Action: "me.digital.download", Resource: strings.TrimSuffix(path, "/download"), Mutates: false}
	}
	return auditAction{Action: "me.digital.list", Resource: "me-licenses", Mutates: false}
}
