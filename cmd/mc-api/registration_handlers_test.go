package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegistrationHandlerSubmitVerifyOnboarding(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	body := `{"email":"alice@example.com","slug_requested":"tenant-a","plan_requested":"starter"}`
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit code = %d body=%s", rec.Code, rec.Body.String())
	}
	var submitResp struct {
		Registration struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"registration"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&submitResp); err != nil {
		t.Fatalf("decode submit: %v", err)
	}
	token, ok := mostRecentToken(srv)
	if !ok || token == "" {
		t.Fatalf("no token recorded by notifier")
	}
	verifyBody, _ := json.Marshal(map[string]string{"token": token})
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register/verify", bytes.NewReader(verifyBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("verify code = %d body=%s", rec.Code, rec.Body.String())
	}

	onboardBody, _ := json.Marshal(map[string]string{
		"registration_id": submitResp.Registration.ID,
		"company_name":    "Acme Co",
		"plan":            "starter",
	})
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register/onboarding", bytes.NewReader(onboardBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("onboarding code = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegistrationHandlerInvalidJSON(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(`{`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRegistrationHandlerVerifyTokenRequired(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register/verify", bytes.NewBufferString(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRegistrationHandlerOnboardingValidation(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	body := `{"registration_id":"","company_name":""}`
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register/onboarding", bytes.NewBufferString(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRegistrationHandlerMethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/register", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestRegistrationHandlerSubmitInvalidEmail(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	body := `{"email":"not-email","slug_requested":"tenant-a"}`
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRegistrationHandlerVerifyInvalidToken(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	body := `{"token":"x.y.z"}`
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/register/verify", bytes.NewBufferString(body)))
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401/404 for bad token, got %d body=%s", rec.Code, rec.Body.String())
	}
}
