package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
)

type mediaApproveRequest struct {
	Reviewer string `json:"reviewer"`
	Note     string `json:"note,omitempty"`
}

type mediaRejectRequest struct {
	Reviewer string `json:"reviewer"`
	Note     string `json:"note"`
}

type mediaRoute struct {
	match  func(string, string) (string, bool)
	handle func(*server, http.ResponseWriter, *http.Request, string)
}

var mediaRoutes = []mediaRoute{
	{match: matchMediaExact("source", http.MethodPost), handle: func(s *server, w http.ResponseWriter, r *http.Request, _ string) { s.sourceMedia(w, r) }},
	{match: matchMediaExact("process", http.MethodPost), handle: func(s *server, w http.ResponseWriter, r *http.Request, _ string) { s.processMedia(w, r) }},
	{match: matchMediaSuffix("/approve", http.MethodPost), handle: func(s *server, w http.ResponseWriter, r *http.Request, mediaID string) { s.approveMedia(w, r, mediaID) }},
	{match: matchMediaSuffix("/reject", http.MethodPost), handle: func(s *server, w http.ResponseWriter, r *http.Request, mediaID string) { s.rejectMedia(w, r, mediaID) }},
	{match: matchMediaSuffix("/validate", http.MethodPost), handle: func(s *server, w http.ResponseWriter, r *http.Request, mediaID string) { s.validateMedia(w, r, mediaID) }},
	{match: matchMediaGet(), handle: func(s *server, w http.ResponseWriter, r *http.Request, mediaID string) { s.getMedia(w, r, mediaID) }},
}

type mediaErrorMapping struct {
	status int
	code   string
	match  func(error) bool
}

var mediaErrorMappings = []mediaErrorMapping{
	{status: http.StatusUnprocessableEntity, code: "invalid_source_url", match: func(err error) bool { return errors.Is(err, intelligence.ErrInvalidSourceURL) }},
	{status: http.StatusServiceUnavailable, code: "media_http_client_not_configured", match: func(err error) bool { return errors.Is(err, intelligence.ErrHTTPClientRequired) }},
	{status: http.StatusNotFound, code: "not_found", match: func(err error) bool { return errors.Is(err, intelligence.ErrMediaNotFound) }},
	{status: http.StatusConflict, code: "state_conflict", match: isMediaLifecycleConflict},
	{status: http.StatusBadGateway, code: "source_failed", match: func(err error) bool { return errors.Is(err, intelligence.ErrSourceFailed) }},
	{status: http.StatusServiceUnavailable, code: "media_store_not_configured", match: func(err error) bool { return errors.Is(err, intelligence.ErrStoreRequired) }},
}

func (s *server) mediaHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/media")
	path = strings.Trim(path, "/")

	for _, route := range mediaRoutes {
		if mediaID, ok := route.match(path, r.Method); ok {
			route.handle(s, w, r, mediaID)
			return
		}
	}
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
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
	asset, replayed, err := s.mediaService.SourceWithReplay(r.Context(), req)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, asset)
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

func (s *server) approveMedia(w http.ResponseWriter, r *http.Request, mediaID string) {
	s.reviewMedia(w, r, mediaID, false, s.mediaService.Approve)
}

func (s *server) rejectMedia(w http.ResponseWriter, r *http.Request, mediaID string) {
	s.reviewMedia(w, r, mediaID, true, s.mediaService.Reject)
}

func (s *server) reviewMedia(w http.ResponseWriter, r *http.Request, mediaID string, requireNote bool, action func(context.Context, string, intelligence.ReviewRequest) (intelligence.Asset, error)) {
	if !s.mediaActionReady(w, mediaID) {
		return
	}

	review, ok := decodeReviewRequest(w, r, requireNote)
	if !ok {
		return
	}

	asset, err := action(r.Context(), mediaID, review)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *server) mediaActionReady(w http.ResponseWriter, mediaID string) bool {
	if s.mediaService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "media_service_not_configured"})
		return false
	}
	if mediaID == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "media_id_required"})
		return false
	}
	return true
}

func decodeReviewRequest(w http.ResponseWriter, r *http.Request, requireNote bool) (intelligence.ReviewRequest, bool) {
	var req mediaRejectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return intelligence.ReviewRequest{}, false
	}
	if strings.TrimSpace(req.Reviewer) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "reviewer_required"})
		return intelligence.ReviewRequest{}, false
	}
	if requireNote && strings.TrimSpace(req.Note) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "review_note_required"})
		return intelligence.ReviewRequest{}, false
	}
	return intelligence.ReviewRequest{
		Reviewer: req.Reviewer,
		Note:     req.Note,
	}, true
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
	for _, mapping := range mediaErrorMappings {
		if mapping.match(err) {
			writeJSON(w, mapping.status, map[string]string{"error": mapping.code})
			return
		}
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
}

func matchMediaExact(target, method string) func(string, string) (string, bool) {
	return func(path, gotMethod string) (string, bool) {
		if path == target && gotMethod == method {
			return "", true
		}
		return "", false
	}
}

func matchMediaSuffix(suffix, method string) func(string, string) (string, bool) {
	return func(path, gotMethod string) (string, bool) {
		if gotMethod != method || !strings.HasSuffix(path, suffix) {
			return "", false
		}
		return strings.Trim(strings.TrimSuffix(path, suffix), "/"), true
	}
}

func matchMediaGet() func(string, string) (string, bool) {
	return func(path, method string) (string, bool) {
		if path == "" || method != http.MethodGet {
			return "", false
		}
		return path, true
	}
}

func isMediaLifecycleConflict(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, intelligence.ErrLifecycleConflict) || err.Error() == intelligence.ErrLifecycleConflict.Error()
}
