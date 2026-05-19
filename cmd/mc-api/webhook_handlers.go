package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	tenantpkg "github.com/nfsarch33/helixon-ec/internal/tenant"
	"github.com/nfsarch33/helixon-ec/internal/webhook/outbound"
)

type createWebhookRequest struct {
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
	Secret     string   `json:"secret"`
	SecretRef  string   `json:"secret_ref,omitempty"`
	Enabled    *bool    `json:"enabled,omitempty"`
}

type webhookRegistrationResponse struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id,omitempty"`
	URL        string    `json:"url"`
	EventTypes []string  `json:"event_types"`
	SecretRef  string    `json:"secret_ref,omitempty"`
	SecretHash string    `json:"secret_hash"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

type webhookListResponse struct {
	Webhooks []webhookRegistrationResponse `json:"webhooks"`
}

type testWebhookRequest struct {
	EventType string `json:"event_type,omitempty"`
}

type webhookDeliveryResultResponse struct {
	ID        string    `json:"id,omitempty"`
	WebhookID string    `json:"webhook_id"`
	EventID   string    `json:"event_id"`
	EventType string    `json:"event_type"`
	Success   bool      `json:"success"`
	Status    int       `json:"status"`
	Attempts  int       `json:"attempts"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type webhookTestResponse struct {
	Delivery webhookDeliveryResultResponse `json:"delivery"`
}

func (s *server) webhooksHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/webhooks"), "/")
	switch {
	case path == "" && r.Method == http.MethodPost:
		s.createWebhook(w, r)
	case path == "" && r.Method == http.MethodGet:
		s.listWebhooks(w, r)
	case strings.HasSuffix(path, "/test") && r.Method == http.MethodPost:
		s.testWebhook(w, r, strings.TrimSuffix(path, "/test"))
	case path != "" && r.Method == http.MethodDelete:
		s.deleteWebhook(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) createWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req createWebhookRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	eventTypes := make([]eventbus.EventType, len(req.EventTypes))
	for i, value := range req.EventTypes {
		eventTypes[i] = eventbus.EventType(strings.TrimSpace(value))
	}
	tenantID, scoped, tenantErr := s.tenantIDForScopedRequest(r)
	if tenantErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return
	}
	reg, err := s.ensureWebhookService().Register(r.Context(), outbound.CreateRegistrationInput{
		TenantID:   tenantValue(tenantID, scoped),
		URL:        req.URL,
		EventTypes: eventTypes,
		Secret:     req.Secret,
		SecretRef:  req.SecretRef,
		Enabled:    req.Enabled,
	})
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWebhookRegistrationResponse(reg))
}

func (s *server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	tenantID, scoped, tenantErr := s.tenantIDForScopedRequest(r)
	if tenantErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return
	}
	var (
		registrations []outbound.Registration
		err           error
	)
	if scoped {
		registrations, err = s.ensureWebhookService().ListForTenant(r.Context(), string(tenantID))
	} else {
		registrations, err = s.ensureWebhookService().List(r.Context())
	}
	if err != nil {
		s.log.Error("list webhooks", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	out := make([]webhookRegistrationResponse, len(registrations))
	for i, reg := range registrations {
		out[i] = toWebhookRegistrationResponse(reg)
	}
	writeJSON(w, http.StatusOK, webhookListResponse{Webhooks: out})
}

func (s *server) deleteWebhook(w http.ResponseWriter, r *http.Request, id string) {
	tenantID, scoped, tenantErr := s.tenantIDForScopedRequest(r)
	if tenantErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return
	}
	var err error
	if scoped {
		err = s.ensureWebhookService().DeleteForTenant(r.Context(), id, string(tenantID))
	} else {
		err = s.ensureWebhookService().Delete(r.Context(), id)
	}
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) testWebhook(w http.ResponseWriter, r *http.Request, id string) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req testWebhookRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	tenantID, scoped, tenantErr := s.tenantIDForScopedRequest(r)
	if tenantErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return
	}
	var result outbound.DeliveryResult
	var err error
	if scoped {
		result, err = s.ensureWebhookService().TestForTenant(r.Context(), id, string(tenantID), eventbus.EventType(strings.TrimSpace(req.EventType)))
	} else {
		result, err = s.ensureWebhookService().Test(r.Context(), id, eventbus.EventType(strings.TrimSpace(req.EventType)))
	}
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, webhookTestResponse{Delivery: toWebhookDeliveryResultResponse(result)})
}

func writeWebhookError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, outbound.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	case errors.Is(err, outbound.ErrInvalidInput):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_webhook"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
	}
}

func (s *server) ensureWebhookService() *outbound.Service {
	if s.webhookService == nil {
		s.webhookService = outbound.NewService(outbound.ServiceConfig{})
	}
	return s.webhookService
}

func toWebhookRegistrationResponse(reg outbound.Registration) webhookRegistrationResponse {
	eventTypes := make([]string, len(reg.EventTypes))
	for i, eventType := range reg.EventTypes {
		eventTypes[i] = string(eventType)
	}
	return webhookRegistrationResponse{
		ID:         reg.ID,
		TenantID:   reg.TenantID,
		URL:        reg.URL,
		EventTypes: eventTypes,
		SecretRef:  reg.SecretRef,
		SecretHash: reg.SecretHash,
		Enabled:    reg.Enabled,
		CreatedAt:  reg.CreatedAt,
	}
}

func tenantValue(tenantID tenantpkg.ID, scoped bool) string {
	if !scoped {
		return ""
	}
	return string(tenantID)
}

func toWebhookDeliveryResultResponse(result outbound.DeliveryResult) webhookDeliveryResultResponse {
	return webhookDeliveryResultResponse{
		ID:        result.ID,
		WebhookID: result.WebhookID,
		EventID:   result.EventID,
		EventType: string(result.EventType),
		Success:   result.Success,
		Status:    result.Status,
		Attempts:  result.Attempts,
		Error:     result.Error,
		CreatedAt: result.CreatedAt,
	}
}
