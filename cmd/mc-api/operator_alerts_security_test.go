package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/security"
)

func TestOperatorAlertMutationsRequireOperatorRole(t *testing.T) {
	t.Run("acknowledge", func(t *testing.T) {
		assertOperatorAlertMutationRole(t, "/api/v1/operator/alerts/alert-1/acknowledge?tenant_id=load-test-tenant")
	})

	t.Run("resolve", func(t *testing.T) {
		assertOperatorAlertMutationRole(t, "/api/v1/operator/alerts/alert-1/resolve?tenant_id=load-test-tenant&action=approve")
	})
}

func TestOperatorAlertMutationsEmitAuditEvents(t *testing.T) {
	t.Run("acknowledge", func(t *testing.T) {
		assertOperatorAlertMutationAudit(
			t,
			"/api/v1/operator/alerts/alert-1/acknowledge?tenant_id=load-test-tenant",
			"operator_alert.acknowledge",
		)
	})

	t.Run("resolve", func(t *testing.T) {
		assertOperatorAlertMutationAudit(
			t,
			"/api/v1/operator/alerts/alert-1/resolve?tenant_id=load-test-tenant&action=approve",
			"operator_alert.resolve",
		)
	})
}

func TestOperatorAlertMutationResponsesCarryTransitionTimestamps(t *testing.T) {
	t.Run("acknowledge", func(t *testing.T) {
		assertOperatorAlertMutationTimestamp(
			t,
			"/api/v1/operator/alerts/alert-1/acknowledge?tenant_id=load-test-tenant",
			"acknowledged_at",
		)
	})

	t.Run("resolve", func(t *testing.T) {
		assertOperatorAlertMutationTimestamp(
			t,
			"/api/v1/operator/alerts/alert-1/resolve?tenant_id=load-test-tenant&action=approve",
			"resolved_at",
		)
	})
}

func assertOperatorAlertMutationRole(t *testing.T, path string) {
	t.Helper()

	t.Run("viewer_forbidden", func(t *testing.T) {
		assertOperatorAlertMutationStatus(t, path, "viewer@example.com", security.RoleViewer, http.StatusForbidden)
	})
	t.Run("operator_allowed", func(t *testing.T) {
		assertOperatorAlertMutationStatus(t, path, "operator@example.com", security.RoleOperator, http.StatusOK)
	})
	t.Run("admin_allowed", func(t *testing.T) {
		assertOperatorAlertMutationStatus(t, path, "admin@example.com", security.RoleAdmin, http.StatusOK)
	})
}

func assertOperatorAlertMutationAudit(t *testing.T, path, action string) {
	t.Helper()

	var logs bytes.Buffer
	srv := secureTestServer(t, &logs)
	defer srv.Close()
	prepareOperatorAlertResolveState(t, srv, path)

	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+mintTestAccessToken(t, srv, "operator@example.com", security.RoleOperator))
	req.Header.Set("X-Tenant-Id", "load-test-tenant")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	logLine := logs.String()
	requireLogContains(t, logLine, `"msg":"audit.event"`)
	requireLogContains(t, logLine, `"actor":"operator@example.com"`)
	requireLogContains(t, logLine, `"role":"operator"`)
	requireLogContains(t, logLine, fmt.Sprintf(`"action":"%s"`, action))
	requireLogContains(t, logLine, `"resource":"alert-1"`)
	requireLogContains(t, logLine, `"tenant_id":"load-test-tenant"`)
	requireLogContains(t, logLine, `"status":200`)
}

func assertOperatorAlertMutationTimestamp(t *testing.T, path, field string) {
	t.Helper()

	srv := secureTestServer(t, nil)
	defer srv.Close()
	prepareOperatorAlertResolveState(t, srv, path)

	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+mintTestAccessToken(t, srv, "operator@example.com", security.RoleOperator))
	req.Header.Set("X-Tenant-Id", "load-test-tenant")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	decodeJSONPayload(t, rec.Body.Bytes(), &payload)
	if got := strings.TrimSpace(stringValue(payload[field])); got == "" {
		t.Fatalf("%s missing from operator-alert mutation response: %#v", field, payload)
	}
}

func assertOperatorAlertMutationStatus(t *testing.T, path, subject string, role security.Role, wantStatus int) {
	t.Helper()

	srv := secureTestServer(t, nil)
	defer srv.Close()
	prepareOperatorAlertResolveState(t, srv, path)

	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+mintTestAccessToken(t, srv, subject, role))
	req.Header.Set("X-Tenant-Id", "load-test-tenant")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != wantStatus {
		t.Fatalf("%s status = %d, want %d; body=%s", subject, rec.Code, wantStatus, rec.Body.String())
	}
}

func prepareOperatorAlertResolveState(t *testing.T, srv *server, path string) {
	t.Helper()
	if !strings.Contains(path, "/resolve") {
		return
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/alert-1/acknowledge?tenant_id=load-test-tenant", nil)
	req.Header.Set("Authorization", "Bearer "+mintTestAccessToken(t, srv, "operator@example.com", security.RoleOperator))
	req.Header.Set("X-Tenant-Id", "load-test-tenant")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("prepare resolve state status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func requireLogContains(t *testing.T, logLine, want string) {
	t.Helper()
	if !strings.Contains(logLine, want) {
		t.Fatalf("audit log missing %q:\n%s", want, logLine)
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	text, _ := value.(string)
	return text
}
