package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
)

func (s *server) mediaHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/media")
	path = strings.Trim(path, "/")

	switch {
	case path == "source" && r.Method == http.MethodPost:
		s.sourceMedia(w, r)
	case path == "process" && r.Method == http.MethodPost:
		s.processMedia(w, r)
	case strings.HasSuffix(path, "/validate") && r.Method == http.MethodPost:
		mediaID := strings.TrimSuffix(path, "/validate")
		s.validateMedia(w, r, strings.Trim(mediaID, "/"))
	case path != "" && r.Method == http.MethodGet:
		s.getMedia(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) sourceMedia(w http.ResponseWriter, r *http.Request) {
	if s.mediaService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "media_service_not_configured"})
		return
	}
	var req intelligence.SourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	asset, err := s.mediaService.Source(r.Context(), req)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, asset)
}

func (s *server) processMedia(w http.ResponseWriter, r *http.Request) {
	if s.mediaService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "media_service_not_configured"})
		return
	}
	var req intelligence.ProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(req.MediaID) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "media_id_required"})
		return
	}
	asset, err := s.mediaService.Process(r.Context(), req)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *server) validateMedia(w http.ResponseWriter, r *http.Request, mediaID string) {
	if s.mediaService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "media_service_not_configured"})
		return
	}
	if mediaID == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "media_id_required"})
		return
	}
	qa, err := s.mediaService.Validate(r.Context(), mediaID)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, qa)
}

func (s *server) getMedia(w http.ResponseWriter, _ *http.Request, mediaID string) {
	if s.mediaService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "media_service_not_configured"})
		return
	}
	asset, ok := s.mediaService.Get(strings.Trim(mediaID, "/"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func writeMediaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, intelligence.ErrInvalidSourceURL):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_source_url"})
	case errors.Is(err, intelligence.ErrHTTPClientRequired):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "media_http_client_not_configured"})
	case errors.Is(err, intelligence.ErrMediaNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	case errors.Is(err, intelligence.ErrSourceFailed):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "source_failed"})
	case errors.Is(err, intelligence.ErrStoreRequired):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "media_store_not_configured"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
	}
}
