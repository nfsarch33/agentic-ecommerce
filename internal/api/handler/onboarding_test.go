// File scope: v3.9.1 Existing #10 -- onboarding wizard handler RED
// tests + 4-step happy-path coverage + Temporal launcher contract.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

type recordingOnboardingMetrics struct {
	mu         sync.Mutex
	steps      []string
	completion []float64
}

func (r *recordingOnboardingMetrics) RecordWizardStepCompleted(tenantID string, step int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, tenantID+"|"+intToStr(step))
}

func (r *recordingOnboardingMetrics) ObserveWizardCompletionDuration(durationSec float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completion = append(r.completion, durationSec)
}

type capturingLauncher struct {
	mu      sync.Mutex
	wizards []OnboardingWizard
	err     error
}

func (l *capturingLauncher) Launch(_ context.Context, w OnboardingWizard) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.wizards = append(l.wizards, w)
	return l.err
}

func (l *capturingLauncher) snapshot() []OnboardingWizard {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]OnboardingWizard, len(l.wizards))
	copy(out, l.wizards)
	return out
}

type capturingOnboardingPublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (p *capturingOnboardingPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *capturingOnboardingPublisher) snapshot() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.events))
	copy(out, p.events)
	return out
}

func newOnboardingHarness(t *testing.T) (*OnboardingHandler, *InMemoryOnboardingRepository, *recordingOnboardingMetrics, *capturingLauncher, *capturingOnboardingPublisher) {
	t.Helper()
	repo := NewInMemoryOnboardingRepository()
	metrics := &recordingOnboardingMetrics{}
	launcher := &capturingLauncher{}
	pub := &capturingOnboardingPublisher{}
	clk := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	step := 0
	h, err := NewOnboardingHandler(nil, OnboardingHandlerConfig{
		Repository: repo,
		Now: func() time.Time {
			step++
			return clk.Add(time.Duration(step) * time.Second)
		},
		Metrics:          metrics,
		Publisher:        pub,
		WorkflowLauncher: launcher,
		WizardIDFunc:     func() string { return "wiz-deterministic" },
	})
	if err != nil {
		t.Fatalf("NewOnboardingHandler: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	return h, repo, metrics, launcher, pub
}

func TestOnboarding_StartCreatesWizard(t *testing.T) {
	t.Parallel()
	h, repo, _, _, _ := newOnboardingHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/start?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		WizardID    string `json:"wizard_id"`
		CurrentStep int    `json:"current_step"`
	}
	must(t, json.Unmarshal(rec.Body.Bytes(), &out))
	if out.WizardID != "wiz-deterministic" {
		t.Fatalf("wizard_id=%q want=wiz-deterministic", out.WizardID)
	}
	if out.CurrentStep != 1 {
		t.Fatalf("current_step=%d want=1", out.CurrentStep)
	}
	stored, err := repo.Get(context.Background(), "tenant-1", "wiz-deterministic")
	must(t, err)
	if stored.CurrentStep != 1 {
		t.Fatalf("stored current_step=%d want=1", stored.CurrentStep)
	}
}

func TestOnboarding_HappyPath4Steps(t *testing.T) {
	t.Parallel()
	h, repo, metrics, launcher, pub := newOnboardingHarness(t)
	startWizard(t, h)

	submitStep(t, h, 1, identityBody("Acme Pty Ltd", "ops@acme.example", "AU", "company"))
	submitStep(t, h, 2, channelsBody([]string{"tiktok", "rednote", "facebook", "woocommerce"}))
	submitStep(t, h, 3, complianceBody([]string{"au_consumer_law", "au_privacy_act"}))
	submitStep(t, h, 4, seedingBody("1688", 25))

	stored, err := repo.Get(context.Background(), "tenant-1", "wiz-deterministic")
	must(t, err)
	if stored.CurrentStep != 5 {
		t.Fatalf("after 4 steps current=%d want=5", stored.CurrentStep)
	}
	if len(stored.CompletedSteps) != 4 {
		t.Fatalf("completed_steps=%v want=[1 2 3 4]", stored.CompletedSteps)
	}

	completeReq := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/complete?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, completeReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", rec.Code, rec.Body.String())
	}

	final, err := repo.Get(context.Background(), "tenant-1", "wiz-deterministic")
	must(t, err)
	if !final.Completed {
		t.Fatalf("wizard not marked completed")
	}
	if launcher.snapshot() == nil || len(launcher.snapshot()) != 1 {
		t.Fatalf("workflow launcher not invoked once: %v", launcher.snapshot())
	}
	events := pub.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 TenantOnboarded event; got %d", len(events))
	}
	if events[0].Type != eventbus.TenantOnboarded {
		t.Fatalf("event type=%q want=%q", events[0].Type, eventbus.TenantOnboarded)
	}
	if got, _ := events[0].Payload["country"].(string); got != "AU" {
		t.Fatalf("event country=%q want=AU", got)
	}
	if len(metrics.steps) != 4 {
		t.Fatalf("expected 4 step metric records, got %d", len(metrics.steps))
	}
	if len(metrics.completion) != 1 {
		t.Fatalf("expected 1 completion duration record, got %d", len(metrics.completion))
	}
}

func TestOnboarding_StateReturnsCurrentSnapshot(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	startWizard(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/wiz-deterministic/state?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out OnboardingWizard
	must(t, json.Unmarshal(rec.Body.Bytes(), &out))
	if out.WizardID != "wiz-deterministic" {
		t.Fatalf("wizard_id=%q want=wiz-deterministic", out.WizardID)
	}
}

func TestOnboarding_OutOfOrderStepRejected(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	startWizard(t, h)
	// Submit step 2 before step 1 -> conflict.
	body := channelsBody([]string{"tiktok"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/step/2?tenant_id=tenant-1", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOnboarding_InvalidStepNumberRejected(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	startWizard(t, h)
	body := identityBody("x", "y@z.com", "AU", "company")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/step/9?tenant_id=tenant-1", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOnboarding_InvalidPayloadRejected(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	startWizard(t, h)
	bad := []byte(`{"tenant_name":"","owner_email":"","country":"","business_type":"unknown"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/step/1?tenant_id=tenant-1", bytes.NewReader(bad))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOnboarding_CompleteRejectsIncomplete(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	startWizard(t, h)
	submitStep(t, h, 1, identityBody("Acme", "x@example.com", "AU", "company"))
	completeReq := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/complete?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, completeReq)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for incomplete wizard, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOnboarding_TenantIsolationEnforced(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	// tenant-1 starts a wizard.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/start?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", rec.Code, rec.Body.String())
	}
	// tenant-evil tries to read it via header (which trumps query).
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/wiz-deterministic/state?tenant_id=tenant-1", nil)
	req2.Header.Set("X-Tenant-Id", "tenant-evil")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant read, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestOnboarding_ClosedReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	if err := h.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/start?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOnboarding_PerformanceWizardCompletionWithin10Min(t *testing.T) {
	t.Parallel()
	h, _, _, _, _ := newOnboardingHarness(t)
	wallStart := time.Now()
	startWizard(t, h)
	submitStep(t, h, 1, identityBody("Acme", "x@example.com", "AU", "company"))
	submitStep(t, h, 2, channelsBody([]string{"tiktok"}))
	submitStep(t, h, 3, complianceBody([]string{"au_consumer_law"}))
	submitStep(t, h, 4, seedingBody("woocommerce", 0))
	completeReq := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/wiz-deterministic/complete?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, completeReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", rec.Code, rec.Body.String())
	}
	wallDur := time.Since(wallStart)
	if wallDur > 10*time.Minute {
		t.Fatalf("wizard completion %s exceeds 10-minute budget", wallDur)
	}
}

// --- helpers ---

func startWizard(t *testing.T, h *OnboardingHandler) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/start?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func submitStep(t *testing.T, h *OnboardingHandler, step int, body []byte) {
	t.Helper()
	url := "/api/v1/onboarding/wiz-deterministic/step/" + intToStr(step) + "?tenant_id=tenant-1"
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("step %d status=%d body=%s", step, rec.Code, rec.Body.String())
	}
}

func identityBody(name, email, country, businessType string) []byte {
	body, _ := json.Marshal(WizardIdentity{
		TenantName:   name,
		OwnerEmail:   email,
		Country:      country,
		BusinessType: businessType,
	})
	return body
}

func channelsBody(channels []string) []byte {
	body, _ := json.Marshal(WizardChannels{Channels: channels})
	return body
}

func complianceBody(flags []string) []byte {
	body, _ := json.Marshal(WizardCompliance{Compliance: flags})
	return body
}

func seedingBody(source string, items int) []byte {
	body, _ := json.Marshal(WizardSeeding{Source: source, ItemCount: items})
	return body
}

func intToStr(i int) string {
	switch i {
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	case 4:
		return "4"
	case 5:
		return "5"
	}
	// Fallback for arbitrary ints (used by error-path tests).
	return strings.TrimSpace(strconvItoaSafe(i))
}

func strconvItoaSafe(i int) string {
	// Stand-alone helper so the file does not need to import strconv
	// twice (the rest of the suite uses the central import in
	// operator_alerts_test.go via standard library helpers).
	return formatIntDecimal(i)
}

func formatIntDecimal(i int) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
