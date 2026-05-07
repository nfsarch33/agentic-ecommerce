package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/internal/compliance"
	tenantpkg "github.com/nfsarch33/agentic-ecommerce/internal/tenant"
)

type tenantSettingsResponse = tenantpkg.Settings

type customRulesResponse struct {
	Rules []compliance.CustomRule `json:"rules"`
}

func (s *server) withTenantRequired(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := s.tenantIDForRequest(r, true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
			return
		}
		if corr := requestCorrelationFromContext(r.Context()); corr != nil {
			corr.TenantID = string(tenantID)
		}
		next(w, r.WithContext(tenantpkg.WithID(r.Context(), tenantID)))
	}
}

func (s *server) tenantIDForRequest(r *http.Request, required bool) (tenantpkg.ID, error) {
	if actor, ok := r.Context().Value(actorContextKey{}).(requestActor); ok && strings.TrimSpace(actor.TenantID) != "" {
		return tenantpkg.RequireID(tenantpkg.ID(actor.TenantID))
	}
	if s != nil && s.tokenManager != nil {
		if claims, ok := s.accessClaimsFromRequest(r); ok && strings.TrimSpace(claims.TenantID) != "" {
			return tenantpkg.RequireID(tenantpkg.ID(claims.TenantID))
		}
	}
	if header := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); header != "" {
		return tenantpkg.RequireID(tenantpkg.ID(header))
	}
	if required {
		return "", tenantpkg.ErrTenantRequired
	}
	return tenantpkg.Default, nil
}

func (s *server) ensureTenantServices() {
	if s.tenantService == nil {
		s.tenantService = tenantpkg.NewService(tenantpkg.NewInMemoryRepository())
	}
	if s.customRuleStore == nil {
		s.customRuleStore = compliance.NewInMemoryCustomRuleStore()
	}
	if s.complianceHistory == nil {
		s.complianceHistory = compliance.NewInMemoryHistoryStore()
	}
}

func (s *server) tenantSettingsHandler(w http.ResponseWriter, r *http.Request) {
	s.ensureTenantServices()
	tenantID, err := tenantpkg.RequiredFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := s.tenantService.GetSettings(r.Context(), tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, tenantSettingsResponse(settings))
	case http.MethodPut:
		var settings tenantpkg.Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		settings.TenantID = tenantID
		if err := s.tenantService.PutSettings(r.Context(), settings); err != nil {
			if errors.Is(err, tenantpkg.ErrTenantRequired) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
				return
			}
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		settings, err = s.tenantService.GetSettings(r.Context(), tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, tenantSettingsResponse(settings))
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) customRulesHandler(w http.ResponseWriter, r *http.Request) {
	s.ensureTenantServices()
	tenantID, err := tenantpkg.RequiredFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/compliance/custom-rules"), "/")
	switch {
	case path == "" && r.Method == http.MethodGet:
		rules, err := s.customRuleStore.ListCustomRules(r.Context(), string(tenantID))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, customRulesResponse{Rules: rules})
	case path == "" && r.Method == http.MethodPost:
		rule, ok := decodeCustomRuleRequest(w, r)
		if !ok {
			return
		}
		rule.TenantID = string(tenantID)
		created, err := s.customRuleStore.CreateCustomRule(r.Context(), rule)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, created)
	case path != "" && r.Method == http.MethodPut:
		rule, ok := decodeCustomRuleRequest(w, r)
		if !ok {
			return
		}
		rule.TenantID = string(tenantID)
		rule.ID = path
		updated, err := s.customRuleStore.UpdateCustomRule(r.Context(), rule)
		if err != nil {
			if errors.Is(err, compliance.ErrCustomRuleNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
				return
			}
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case path != "" && r.Method == http.MethodDelete:
		if err := s.customRuleStore.DeleteCustomRule(r.Context(), string(tenantID), path); err != nil {
			if errors.Is(err, compliance.ErrCustomRuleNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
				return
			}
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func decodeCustomRuleRequest(w http.ResponseWriter, r *http.Request) (compliance.CustomRule, bool) {
	var rule compliance.CustomRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return compliance.CustomRule{}, false
	}
	return rule, true
}

func (s *server) complianceReportsHandler(w http.ResponseWriter, r *http.Request) {
	s.ensureTenantServices()
	tenantID, err := tenantpkg.RequiredFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return
	}
	filter := compliance.SummaryFilter{TenantID: string(tenantID)}
	switch {
	case r.URL.Path == "/api/v1/compliance/reports/summary" && r.Method == http.MethodGet:
		summary, err := s.complianceHistory.Summary(r.Context(), filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, summary)
	case r.URL.Path == "/api/v1/compliance/reports/export" && r.Method == http.MethodGet:
		format := compliance.ExportFormat(strings.TrimSpace(r.URL.Query().Get("format")))
		if format == "" {
			format = compliance.ExportFormatJSON
		}
		payload, contentType, err := compliance.ExportHistory(r.Context(), s.complianceHistory, filter, format)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", "attachment; filename=compliance-report."+string(format))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}
