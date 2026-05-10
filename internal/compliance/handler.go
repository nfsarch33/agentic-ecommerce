// File scope: v4.9.0 Story 2 -- GDPR/CCPA compliance HTTP endpoints.
//
// Endpoints:
//
//	POST /api/v1/compliance/delete-request
//	GET  /api/v1/compliance/export/{subject_id}
//
// Decomposition (HARD GATE: complex_fn=4):
//   - ServeHTTP       -> route (cyclomatic 3)
//   - handleDelete    -> parse + call (cyclomatic 3)
//   - handleExport    -> parse + call (cyclomatic 3)
//   - parseSubjectID  -> extract from path (cyclomatic 2)
package compliance

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// ComplianceHandler serves the compliance API.
type ComplianceHandler struct {
	svc          *Service
	tenantHeader string
	logger       *slog.Logger
}

// NewComplianceHandler constructs the handler.
func NewComplianceHandler(svc *Service, logger *slog.Logger) *ComplianceHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ComplianceHandler{
		svc:          svc,
		tenantHeader: "X-Tenant-Id",
		logger:       logger,
	}
}

// ServeHTTP routes compliance requests.
func (h *ComplianceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/delete-request"):
		h.handleDelete(w, r)
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/export/"):
		h.handleExport(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("unsupported: %s %s", r.Method, r.URL.Path))
	}
}

type deleteRequest struct {
	SubjectID string `json:"subject_id"`
}

func (h *ComplianceHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get(h.tenantHeader))
	if tenantID == "" {
		h.writeError(w, http.StatusBadRequest, fmt.Errorf("missing %s header", h.tenantHeader))
		return
	}
	var req deleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}
	if req.SubjectID == "" {
		h.writeError(w, http.StatusBadRequest, fmt.Errorf("subject_id required"))
		return
	}
	if err := h.svc.RightToDelete(r.Context(), tenantID, req.SubjectID); err != nil {
		h.logger.Error("compliance.delete_failed", "tenant_id", tenantID, "subject_id", req.SubjectID, "error", err)
		h.writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "subject_id": req.SubjectID})
}

func (h *ComplianceHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get(h.tenantHeader))
	if tenantID == "" {
		h.writeError(w, http.StatusBadRequest, fmt.Errorf("missing %s header", h.tenantHeader))
		return
	}
	subjectID := parseSubjectID(r.URL.Path)
	if subjectID == "" {
		h.writeError(w, http.StatusBadRequest, fmt.Errorf("subject_id required in path"))
		return
	}
	bundle, err := h.svc.DataExport(r.Context(), tenantID, subjectID)
	if err != nil {
		h.logger.Error("compliance.export_failed", "tenant_id", tenantID, "subject_id", subjectID, "error", err)
		h.writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.writeJSON(w, http.StatusOK, bundle)
}

func parseSubjectID(path string) string {
	const prefix = "/export/"
	idx := strings.LastIndex(path, prefix)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(path[idx+len(prefix):])
}

func (h *ComplianceHandler) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *ComplianceHandler) writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
