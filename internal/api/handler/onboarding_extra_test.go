// File scope: v3.9.1 Existing #10 -- additional onboarding handler
// coverage tests (defaultWizardID, statusForLookup, error paths).
package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOnboarding_DefaultWizardIDIsUnique(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryOnboardingRepository()
	h, err := NewOnboardingHandler(nil, OnboardingHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewOnboardingHandler: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	ids := map[string]struct{}{}
	for i := 0; i < 5; i++ {
		id := h.defaultWizardID()
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate wizard id: %s", id)
		}
		if !strings.HasPrefix(id, "wiz-") {
			t.Fatalf("wizard id missing prefix: %s", id)
		}
		ids[id] = struct{}{}
	}
}

func TestOnboarding_RejectsMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/onboarding/wiz-x/state?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unsupported method/path, got %d", rec.Code)
	}
}

func TestOnboarding_StateMissingTenantReturns400(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/wiz-x/state", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOnboarding_StateNotFound(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/wiz-missing/state?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestOnboarding_StepWithoutBodyRejected(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	startWizard(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/step/1?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d", rec.Code)
	}
}

func TestOnboarding_StepInvalidChannelRejected(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	startWizard(t, h)
	submitStep(t, h, 1, identityBody("Acme", "x@example.com", "AU", "company"))
	bad := []byte(`{"channels":["myspace"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/step/2?tenant_id=tenant-1", bytesReader(bad))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid channel, got %d", rec.Code)
	}
}

func TestOnboarding_StepInvalidComplianceRejected(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	startWizard(t, h)
	submitStep(t, h, 1, identityBody("Acme", "x@example.com", "AU", "company"))
	submitStep(t, h, 2, channelsBody([]string{"tiktok"}))
	bad := []byte(`{"compliance":["unknown"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/step/3?tenant_id=tenant-1", bytesReader(bad))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid compliance, got %d", rec.Code)
	}
}

func TestOnboarding_StepInvalidSeedSourceRejected(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	startWizard(t, h)
	submitStep(t, h, 1, identityBody("Acme", "x@example.com", "AU", "company"))
	submitStep(t, h, 2, channelsBody([]string{"tiktok"}))
	submitStep(t, h, 3, complianceBody([]string{"au_consumer_law"}))
	bad := []byte(`{"source":"alibaba","item_count":-1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/step/4?tenant_id=tenant-1", bytesReader(bad))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid seed source, got %d", rec.Code)
	}
}

func TestOnboarding_CompleteMissingTenant(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-x/complete", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOnboarding_StepInvalidJSONRejected(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	startWizard(t, h)
	bad := []byte(`{"tenant_name":"x"`) // truncated JSON
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/step/1?tenant_id=tenant-1", bytesReader(bad))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", rec.Code)
	}
}

func TestOnboarding_StatusForLookupBadRequest(t *testing.T) {
	t.Parallel()
	h := &OnboardingHandler{}
	if got := h.statusForLookup(ErrOnboardingTenantMissing); got != http.StatusBadRequest {
		t.Fatalf("statusForLookup(tenant missing)=%d want=400", got)
	}
	if got := h.statusForLookup(ErrWizardNotFound); got != http.StatusNotFound {
		t.Fatalf("statusForLookup(not found)=%d want=404", got)
	}
}

// bytesReader is a tiny shim for the test files that already use
// bytes.NewReader inline; keeping it local removes the need to add
// the import to every fixture.
func bytesReader(b []byte) *strings.Reader {
	return strings.NewReader(string(b))
}
