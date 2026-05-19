package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/security"
)

type loginRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type loginResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	TokenType    string          `json:"token_type"`
	ExpiresIn    int64           `json:"expires_in"`
	Role         string          `json:"role"`
	Session      sessionResponse `json:"session"`
}

type sessionResponse struct {
	User      sessionUserResponse `json:"user"`
	ExpiresAt string              `json:"expires_at"`
}

type sessionUserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (s *server) authHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/auth"), "/")
	switch {
	case path == "login" && r.Method == http.MethodPost:
		s.login(w, r)
	case path == "refresh" && r.Method == http.MethodPost:
		s.refresh(w, r)
	case path == "me" && r.Method == http.MethodGet:
		s.me(w, r)
	case path == "logout" && r.Method == http.MethodPost:
		s.logout(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if s.tokenManager == nil || s.sessions == nil || s.cfg.adminUsername == "" || s.cfg.adminPassword == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_not_configured"})
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = strings.TrimSpace(req.Email)
	}
	if !constantTimeStringEqual(username, s.cfg.adminUsername) || !constantTimeStringEqual(req.Password, s.cfg.adminPassword) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
		return
	}
	s.writeTokenPair(w, r, security.Principal{Subject: s.cfg.adminUsername, Role: s.cfg.adminRole})
}

func (s *server) refresh(w http.ResponseWriter, r *http.Request) {
	if s.tokenManager == nil || s.sessions == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_not_configured"})
		return
	}
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	hash := security.HashRefreshToken(req.RefreshToken)
	session, err := s.sessions.Get(r.Context(), hash)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_refresh_token"})
		return
	}
	_ = s.sessions.Revoke(r.Context(), hash)
	s.writeTokenPair(w, r, security.Principal{Subject: session.Subject, Role: session.Role})
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := s.accessClaimsFromRequest(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorised"})
		return
	}
	writeJSON(w, http.StatusOK, sessionFromClaims(claims))
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.accessClaimsFromRequest(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorised"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) accessClaimsFromRequest(r *http.Request) (security.AccessClaims, bool) {
	if s.tokenManager == nil {
		return security.AccessClaims{}, false
	}
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(value, "Bearer ") {
		return security.AccessClaims{}, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	if token == "" {
		return security.AccessClaims{}, false
	}
	claims, err := s.tokenManager.VerifyAccessToken(token)
	return claims, err == nil
}

func (s *server) writeTokenPair(w http.ResponseWriter, r *http.Request, principal security.Principal) {
	accessToken, err := s.tokenManager.MintAccessToken(principal)
	if err != nil {
		s.log.Error("mint access token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	refreshToken, refreshHash, err := security.NewRefreshToken()
	if err != nil {
		s.log.Error("mint refresh token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	now := time.Now().UTC()
	if err := s.sessions.Save(r.Context(), security.RefreshSession{
		TokenHash: refreshHash,
		Subject:   principal.Subject,
		Role:      principal.Role,
		CreatedAt: now,
		ExpiresAt: now.Add(s.cfg.refreshTTL),
	}); err != nil {
		s.log.Error("save refresh session", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.tokenManager.AccessTTL().Seconds()),
		Role:         string(principal.Role),
		Session: sessionResponse{
			User: sessionUserResponse{
				ID:    principal.Subject,
				Email: principal.Subject,
				Role:  string(principal.Role),
			},
			ExpiresAt: now.Add(s.tokenManager.AccessTTL()).Format(time.RFC3339),
		},
	})
}

func sessionFromClaims(claims security.AccessClaims) sessionResponse {
	return sessionResponse{
		User: sessionUserResponse{
			ID:    claims.Subject,
			Email: claims.Subject,
			Role:  string(claims.Role),
		},
		ExpiresAt: claims.ExpiresAt.Format(time.RFC3339),
	}
}
