package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	wcwebhook "github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce/webhook"
	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
)

type syncStatusResponse = enginesync.Status

type conflictsResponse struct {
	Conflicts []enginesync.Conflict `json:"conflicts"`
}

type resolveConflictRequest struct {
	Resolution string `json:"resolution"`
	Note       string `json:"note,omitempty"`
}

func (s *server) syncHandler(w http.ResponseWriter, r *http.Request) {
	if s.syncEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "sync_not_configured"})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sync")
	path = strings.Trim(path, "/")
	switch {
	case path == "status" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, s.syncEngine.Status())
	case path == "conflicts" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, conflictsResponse{Conflicts: s.syncEngine.Conflicts()})
	case strings.HasPrefix(path, "conflicts/") && strings.HasSuffix(path, "/resolve") && r.Method == http.MethodPost:
		s.resolveConflict(w, r, path)
	case strings.HasPrefix(path, "products/") && strings.HasSuffix(path, "/publish") && r.Method == http.MethodPost:
		s.publishProduct(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) resolveConflict(w http.ResponseWriter, r *http.Request, path string) {
	id := strings.TrimPrefix(path, "conflicts/")
	id = strings.TrimSuffix(id, "/resolve")
	id = strings.Trim(id, "/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_conflict_id"})
		return
	}
	var req resolveConflictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	conflict, err := s.syncEngine.ResolveConflict(id, req.Resolution, req.Note)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, conflict)
}

func (s *server) publishProduct(w http.ResponseWriter, r *http.Request, path string) {
	idPart := strings.TrimPrefix(path, "products/")
	idPart = strings.TrimSuffix(idPart, "/publish")
	idPart = strings.Trim(idPart, "/")
	id, err := uuid.Parse(idPart)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	if err := s.syncEngine.PublishToWooCommerce(r.Context(), id); err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("publish product", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "publish_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

func (s *server) woocommerceOrderWebhookHandler(w http.ResponseWriter, r *http.Request) {
	wcwebhook.NewHandler(wcwebhook.Config{
		Secret:   s.webhookSecret,
		Resource: wcwebhook.ResourceOrders,
		Recorder: s.syncEngine,
	}).ServeHTTP(w, r)
}

func (s *server) woocommerceProductWebhookHandler(w http.ResponseWriter, r *http.Request) {
	wcwebhook.NewHandler(wcwebhook.Config{
		Secret:   s.webhookSecret,
		Resource: wcwebhook.ResourceProducts,
		Recorder: s.syncEngine,
	}).ServeHTTP(w, r)
}
