// File scope: HTTP handler for `POST /api/v1/uiauto/captcha/<event_id>/resolved`.
// The handler is operator-auth-only: a JWT bearer token (verified by
// an injected Authenticator) must be present.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): ServeHTTP splits into authenticate + extractEventID +
// resolve helpers; per-function cyclomatic stays under 6.
package captcha

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// PathPrefix is the canonical mount point for the resolution
// endpoint. Wired in cmd/mc-api or equivalent composition root.
const PathPrefix = "/api/v1/uiauto/captcha/"

// resolvedSuffix is the path suffix for the resolution endpoint.
const resolvedSuffix = "/resolved"

// Authenticator is the operator-auth port. The composition root
// wires the existing JWT middleware via the small interface so
// this package stays decoupled from auth implementation.
type Authenticator interface {
	// Authenticate inspects the Authorization header and returns
	// the resolved operator subject (or an error). Empty subject
	// + nil error means anonymous (rejected by default).
	Authenticate(r *http.Request) (string, error)
}

// AllowAllAuthenticator is a test-only stub. Production must NOT
// use it.
type AllowAllAuthenticator struct{}

// Authenticate returns "anon" so the handler proceeds.
func (AllowAllAuthenticator) Authenticate(_ *http.Request) (string, error) { return "anon", nil }

// HandlerConfig wires the resolution handler.
type HandlerConfig struct {
	Detector      *Detector
	Authenticator Authenticator
}

// Handler implements http.Handler for the resolution endpoint.
type Handler struct {
	detector      *Detector
	authenticator Authenticator
}

// NewHandler constructs a Handler. Both fields required.
func NewHandler(cfg HandlerConfig) (*Handler, error) {
	if cfg.Detector == nil {
		return nil, errors.New("captcha handler: detector required")
	}
	if cfg.Authenticator == nil {
		return nil, errors.New("captcha handler: authenticator required")
	}
	return &Handler{detector: cfg.Detector, authenticator: cfg.Authenticator}, nil
}

// ServeHTTP routes the request. Decomposes into auth + extract +
// resolve helpers.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if subject, err := h.authenticator.Authenticate(r); err != nil || subject == "" {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	eventID, ok := extractEventID(r.URL.Path)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "missing_event_id")
		return
	}
	if err := h.detector.Resolve(eventID); err != nil {
		if errors.Is(err, ErrCAPTCHAResolutionInvalid) {
			writeJSONError(w, http.StatusNotFound, "event_not_found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "resolve_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"event_id": eventID, "status": "resolved"})
}

// extractEventID returns the slug between PathPrefix and
// resolvedSuffix.
func extractEventID(path string) (string, bool) {
	if !strings.HasPrefix(path, PathPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, PathPrefix)
	if !strings.HasSuffix(rest, resolvedSuffix) {
		return "", false
	}
	id := strings.TrimSuffix(rest, resolvedSuffix)
	if id == "" {
		return "", false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
