package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/security"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type loginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Role         string `json:"role"`
}

func (s *server) authHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/auth"), "/")
	switch {
	case path == "login" && r.Method == http.MethodPost:
		s.login(w, r)
	case path == "refresh" && r.Method == http.MethodPost:
		s.refresh(w, r)
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
	if !constantTimeStringEqual(req.Username, s.cfg.adminUsername) || !constantTimeStringEqual(req.Password, s.cfg.adminPassword) {
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
	})
}
