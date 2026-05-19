package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nfsarch33/helixon-ec/internal/registration"
	"github.com/nfsarch33/helixon-ec/internal/tenant"
)

// registrationSubmitRequest is the JSON body for POST /register.
type registrationSubmitRequest struct {
	Email         string `json:"email"`
	SlugRequested string `json:"slug_requested"`
	PlanRequested string `json:"plan_requested,omitempty"`
}

// registrationVerifyRequest is the JSON body for POST /register/verify.
type registrationVerifyRequest struct {
	Token string `json:"token"`
}

// registrationOnboardingRequest is the JSON body for POST /register/onboarding.
type registrationOnboardingRequest struct {
	RegistrationID string `json:"registration_id"`
	CompanyName    string `json:"company_name"`
	Plan           string `json:"plan,omitempty"`
}

// registrationResponse is the wire shape for a Request value.
type registrationResponse struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	SlugRequested string `json:"slug_requested"`
	PlanRequested string `json:"plan_requested"`
	Status        string `json:"status"`
	TenantID      string `json:"tenant_id,omitempty"`
	CompanyName   string `json:"company_name,omitempty"`
}

// registrationOnboardingResponse pairs the activated registration
// with the freshly provisioned tenant.
type registrationOnboardingResponse struct {
	Registration registrationResponse `json:"registration"`
	Tenant       tenantAdminResponse  `json:"tenant"`
}

// registrationHandler routes /register* paths. The handler is wired
// in main.mux without RBAC because the endpoints are intentionally
// public; rate limiting is preserved.
func (s *server) registrationHandler(w http.ResponseWriter, r *http.Request) {
	if s.registrationSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "registration_unconfigured"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/register")
	rest = strings.TrimPrefix(rest, "/")
	switch {
	case rest == "" && r.Method == http.MethodPost:
		s.handleRegistrationSubmit(w, r)
	case rest == "verify" && r.Method == http.MethodPost:
		s.handleRegistrationVerify(w, r)
	case rest == "onboarding" && r.Method == http.MethodPost:
		s.handleRegistrationOnboarding(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) handleRegistrationSubmit(w http.ResponseWriter, r *http.Request) {
	var req registrationSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	out, err := s.registrationSvc.Submit(r.Context(), registration.SubmitInput{
		Email:         req.Email,
		SlugRequested: req.SlugRequested,
		PlanRequested: req.PlanRequested,
	})
	if err != nil {
		writeRegistrationError(w, err)
		return
	}
	resp := struct {
		Registration registrationResponse `json:"registration"`
		Message      string               `json:"message"`
	}{
		Registration: toRegistrationResponse(out.Request),
		Message:      "Check your email to verify the address.",
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (s *server) handleRegistrationVerify(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		var req registrationVerifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		token = strings.TrimSpace(req.Token)
	}
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token_required"})
		return
	}
	verified, err := s.registrationSvc.Verify(r.Context(), token)
	if err != nil {
		writeRegistrationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRegistrationResponse(verified))
}

func (s *server) handleRegistrationOnboarding(w http.ResponseWriter, r *http.Request) {
	var req registrationOnboardingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if req.RegistrationID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "registration_id_required"})
		return
	}
	if strings.TrimSpace(req.CompanyName) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "company_name_required"})
		return
	}
	final, t, err := s.registrationSvc.CompleteOnboarding(r.Context(), req.RegistrationID, req.CompanyName, req.Plan)
	if err != nil {
		writeRegistrationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, registrationOnboardingResponse{
		Registration: toRegistrationResponse(final),
		Tenant:       toTenantAdminResponse(t),
	})
}

// writeRegistrationError translates registration sentinel errors to
// HTTP statuses.
func writeRegistrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, registration.ErrEmailRequired):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email_required"})
	case errors.Is(err, registration.ErrSlugRequired):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug_required"})
	case errors.Is(err, registration.ErrSlugTaken):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "slug_taken"})
	case errors.Is(err, registration.ErrTokenInvalid):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token_invalid"})
	case errors.Is(err, registration.ErrTokenExpired):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token_expired"})
	case errors.Is(err, registration.ErrAlreadyVerified):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already_verified"})
	case errors.Is(err, registration.ErrAlreadyActive):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already_active"})
	case errors.Is(err, registration.ErrInvalidTransition):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_transition"})
	case errors.Is(err, registration.ErrRequestNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "registration_not_found"})
	case errors.Is(err, tenant.ErrTenantSlugAlreadyExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "slug_taken"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
	}
}

func toRegistrationResponse(r registration.Request) registrationResponse {
	return registrationResponse{
		ID:            r.ID,
		Email:         r.Email,
		SlugRequested: r.SlugRequested,
		PlanRequested: r.PlanRequested,
		Status:        string(r.Status),
		TenantID:      r.TenantID,
		CompanyName:   r.CompanyName,
	}
}
