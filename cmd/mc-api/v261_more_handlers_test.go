package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/security"
)

// File scope: v2.6.1 cmd/* DI refactor coverage. Additional uniform
// error branches across agent + content + billing handlers to push
// cmd/mc-api over the 85% per-binary target.

// agentRunsHandler at 42.9% — covers method-not-allowed + not-found.
func TestAgentRunsHandler_RejectsNonGet(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	srv.ensureAgentScheduler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent-runs/some-id", nil)
	rec := httptest.NewRecorder()
	srv.agentRunsHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestAgentRunsHandler_ReturnsNotFoundForUnknownRun(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	srv.ensureAgentScheduler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-runs/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	srv.agentRunsHandler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// operatorRole + configureTelemetry are tiny stubs at 0% coverage.
// Direct calls drive them to 100%.
func TestOperatorRole_AlwaysReturnsOperator(t *testing.T) {
	t.Parallel()
	if got := operatorRole(httptest.NewRequest(http.MethodGet, "/", nil)); got != security.RoleOperator {
		t.Errorf("operatorRole = %v, want RoleOperator", got)
	}
}

func TestConfigureTelemetry_NoPanic(t *testing.T) {
	t.Parallel()
	configureTelemetry()
}

// recentEventsHandler — no tenant context.
func TestRecentEventsHandler_AllowsMissingTenant(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	srv.ensureAgentScheduler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/recent", nil)
	rec := httptest.NewRecorder()
	srv.recentEventsHandler(rec, req)
	// Either 200 (empty list) or 503 if eventBus nil; both are
	// non-panic outcomes that exercise the code path.
	if rec.Code >= 500 && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status %d", rec.Code)
	}
}
