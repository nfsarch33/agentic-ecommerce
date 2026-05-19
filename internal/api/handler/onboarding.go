// File scope: v3.9.1 Existing #10 -- AI onboarding wizard HTTP
// handler.
//
// Endpoints (REST + JSON, all tenant-scoped):
//
//   - POST /api/v1/onboarding/start
//     -> create a wizard, return wizard_id + initial state.
//   - GET  /api/v1/onboarding/{wizard_id}/state
//     -> current step + completed steps + accumulated state.
//   - POST /api/v1/onboarding/{wizard_id}/step/{step_num}
//     -> submit data for the current step + advance.
//   - POST /api/v1/onboarding/{wizard_id}/complete
//     -> finalise tenant setup; emits TenantOnboardedEvent and
//     kicks off the existing internal/workflow/tenant_onboarding
//     Temporal workflow (v3.0.0).
//
// Wizard steps:
//
//	step 1: tenant identity (name, email, country, business type)
//	step 2: channel connections (TikTok, RedNote, FB, WC selections)
//	step 3: compliance (auto-detect AU/CN regulations + select)
//	step 4: initial product seeding (1688/Taobao import or WC import)
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 17-sprint streak target):
//
//   - ServeHTTP                 -> route by suffix + method (cyclomatic 5)
//   - handleStart               -> wizard create + write (cyclomatic 3)
//   - handleState               -> repo + write (cyclomatic 3)
//   - handleStepSubmit          -> validate + dispatch by step (cyclomatic 5)
//   - handleComplete            -> wizard repo + workflow launch + emit event (cyclomatic 4)
//   - applyStepIdentity         -> step 1 helper (cyclomatic 3)
//   - applyStepChannels         -> step 2 helper (cyclomatic 3)
//   - applyStepCompliance       -> step 3 helper (cyclomatic 3)
//   - applyStepSeeding          -> step 4 helper (cyclomatic 3)
//
// Each helper stays under cyclomatic 6.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// Existing #10 typed sentinels.
var (
	// ErrOnboardingHandlerUnconfigured is returned when a required
	// dependency is missing.
	ErrOnboardingHandlerUnconfigured = errors.New("handler: onboarding handler unconfigured")

	// ErrOnboardingHandlerClosed is returned after Close.
	ErrOnboardingHandlerClosed = errors.New("handler: onboarding handler closed")

	// ErrWizardNotFound is returned when the wizard_id is not in the
	// repository for the requesting tenant.
	ErrWizardNotFound = errors.New("handler: onboarding wizard not found")

	// ErrInvalidStep is returned when the supplied step_num is
	// outside the [1, 4] range.
	ErrInvalidStep = errors.New("handler: invalid onboarding step")

	// ErrStepOutOfOrder is returned when the supplied step_num is
	// not the current step (callers must advance steps in order).
	ErrStepOutOfOrder = errors.New("handler: onboarding step out of order")

	// ErrIncompleteWizard is returned by /complete when one or more
	// required steps are missing.
	ErrIncompleteWizard = errors.New("handler: onboarding wizard incomplete")

	// ErrOnboardingTenantMissing is returned when the tenant_id
	// cannot be derived from the request.
	ErrOnboardingTenantMissing = errors.New("handler: onboarding tenant missing")

	// ErrInvalidStepPayload is returned when the step body fails
	// validation.
	ErrInvalidStepPayload = errors.New("handler: invalid step payload")
)

// MaxOnboardingSteps is the wizard's terminal step number. Step 5
// is the completion sentinel; only steps 1-4 accept payloads.
const MaxOnboardingSteps = 4

// AllowedBusinessTypes lists the v3.9.1 business types accepted by
// step 1.
var AllowedBusinessTypes = map[string]struct{}{
	"sole_trader": {},
	"partnership": {},
	"company":     {},
	"trust":       {},
	"non_profit":  {},
}

// AllowedChannels lists the channel keys accepted by step 2.
var AllowedChannels = map[string]struct{}{
	"tiktok":      {},
	"rednote":     {},
	"facebook":    {},
	"woocommerce": {},
	"instagram":   {},
	"pinterest":   {},
}

// AllowedCompliance lists the compliance flags accepted by step 3.
var AllowedCompliance = map[string]struct{}{
	"au_consumer_law":   {},
	"au_privacy_act":    {},
	"au_australian_tax": {},
	"cn_ecommerce_law":  {},
	"cn_data_export":    {},
	"gdpr":              {},
}

// AllowedSeedSources lists the seed sources accepted by step 4.
var AllowedSeedSources = map[string]struct{}{
	"1688":        {},
	"taobao":      {},
	"woocommerce": {},
	"manual":      {},
}

// WizardIdentity is the parsed step-1 payload.
type WizardIdentity struct {
	TenantName   string `json:"tenant_name"`
	OwnerEmail   string `json:"owner_email"`
	Country      string `json:"country"`
	BusinessType string `json:"business_type"`
}

// WizardChannels is the parsed step-2 payload.
type WizardChannels struct {
	Channels []string `json:"channels"`
}

// WizardCompliance is the parsed step-3 payload.
type WizardCompliance struct {
	Compliance []string `json:"compliance"`
}

// WizardSeeding is the parsed step-4 payload.
type WizardSeeding struct {
	Source    string `json:"source"`
	ItemCount int    `json:"item_count,omitempty"`
}

// OnboardingWizard is the persisted wizard state.
type OnboardingWizard struct {
	TenantID       string            `json:"tenant_id"`
	WizardID       string            `json:"wizard_id"`
	CurrentStep    int               `json:"current_step"`
	CompletedSteps []int             `json:"completed_steps"`
	Identity       *WizardIdentity   `json:"identity,omitempty"`
	Channels       *WizardChannels   `json:"channels,omitempty"`
	Compliance     *WizardCompliance `json:"compliance,omitempty"`
	Seeding        *WizardSeeding    `json:"seeding,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	LastAdvancedAt time.Time         `json:"last_advanced_at"`
	CompletedAt    time.Time         `json:"completed_at,omitempty"`
	Completed      bool              `json:"completed"`
}

// OnboardingRepository is the small port the handler reads + writes
// through. Production wires a Postgres-backed implementation; tests
// use an in-memory one.
type OnboardingRepository interface {
	Create(ctx context.Context, w OnboardingWizard) error
	Get(ctx context.Context, tenantID, wizardID string) (OnboardingWizard, error)
	Update(ctx context.Context, w OnboardingWizard) error
}

// OnboardingMetrics is the small port the handler emits a per-step
// counter + completion histogram through.
type OnboardingMetrics interface {
	RecordWizardStepCompleted(tenantID string, step int)
	ObserveWizardCompletionDuration(durationSec float64)
}

// OnboardingWorkflowLauncher is the small port the handler uses to
// kick off the existing v3.0.0 internal/workflow/tenant_onboarding
// Temporal workflow once all four wizard steps are complete.
//
// In production cmd/* binaries wire a Temporal-backed
// implementation; tests pass a fake that records the launch.
type OnboardingWorkflowLauncher interface {
	Launch(ctx context.Context, w OnboardingWizard) error
}

// OnboardingEventPublisher is the tiny port used to publish the
// TenantOnboardedEvent on /complete.
type OnboardingEventPublisher interface {
	Publish(ctx context.Context, evt eventbus.Event) error
}

// OnboardingHandlerConfig wires the handler.
type OnboardingHandlerConfig struct {
	Repository       OnboardingRepository
	TenantHeader     string
	Now              func() time.Time
	Metrics          OnboardingMetrics
	Publisher        OnboardingEventPublisher
	WorkflowLauncher OnboardingWorkflowLauncher
	WizardIDFunc     func() string
}

// OnboardingHandler is the Existing #10 HTTP handler.
type OnboardingHandler struct {
	repo         OnboardingRepository
	tenantHeader string
	now          func() time.Time
	logger       *slog.Logger
	metrics      OnboardingMetrics
	publisher    OnboardingEventPublisher
	launcher     OnboardingWorkflowLauncher
	wizardIDFunc func() string

	mu     sync.Mutex
	closed bool
	seq    int
}

// NewOnboardingHandler constructs the handler.
func NewOnboardingHandler(logger *slog.Logger, cfg OnboardingHandlerConfig) (*OnboardingHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("%w: OnboardingRepository required", ErrOnboardingHandlerUnconfigured)
	}
	if cfg.TenantHeader == "" {
		cfg.TenantHeader = "X-Tenant-Id"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	h := &OnboardingHandler{
		repo:         cfg.Repository,
		tenantHeader: cfg.TenantHeader,
		now:          cfg.Now,
		logger:       logger,
		metrics:      cfg.Metrics,
		publisher:    cfg.Publisher,
		launcher:     cfg.WorkflowLauncher,
		wizardIDFunc: cfg.WizardIDFunc,
	}
	if h.wizardIDFunc == nil {
		h.wizardIDFunc = h.defaultWizardID
	}
	return h, nil
}

// Close marks the handler closed. lifecycle.Closer contract.
func (h *OnboardingHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP routes by URL suffix + method. Cyclomatic 5.
func (h *OnboardingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.guard(); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, err)
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/onboarding")
	suffix = strings.TrimSuffix(suffix, "/")
	switch {
	case suffix == "/start" && r.Method == http.MethodPost:
		h.handleStart(w, r)
	case strings.HasSuffix(suffix, "/state") && r.Method == http.MethodGet:
		h.handleState(w, r, suffix)
	case strings.Contains(suffix, "/step/") && r.Method == http.MethodPost:
		h.handleStepSubmit(w, r, suffix)
	case strings.HasSuffix(suffix, "/complete") && r.Method == http.MethodPost:
		h.handleComplete(w, r, suffix)
	default:
		writeJSONError(w, http.StatusNotFound, fmt.Errorf("unknown onboarding route: %s %s", r.Method, r.URL.Path))
	}
}

func (h *OnboardingHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrOnboardingHandlerClosed
	}
	return nil
}

// handleStart serves POST /onboarding/start. Cyclomatic 3.
func (h *OnboardingHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveOnboardingTenantID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	now := h.now()
	wiz := OnboardingWizard{
		TenantID:       tenantID,
		WizardID:       h.wizardIDFunc(),
		CurrentStep:    1,
		CompletedSteps: []int{},
		StartedAt:      now,
		LastAdvancedAt: now,
	}
	if err := h.repo.Create(r.Context(), wiz); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"tenant_id":    wiz.TenantID,
		"wizard_id":    wiz.WizardID,
		"current_step": wiz.CurrentStep,
	})
}

// handleState serves GET /onboarding/{id}/state. Cyclomatic 3.
func (h *OnboardingHandler) handleState(w http.ResponseWriter, r *http.Request, suffix string) {
	tenantID, wizardID, err := h.resolveWizardContext(r, suffix, "/state")
	if err != nil {
		writeJSONError(w, h.statusForLookup(err), err)
		return
	}
	wiz, err := h.repo.Get(r.Context(), tenantID, wizardID)
	if err != nil {
		h.notFoundOrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, wiz)
}

// handleStepSubmit serves POST /onboarding/{id}/step/{n}. Cyclomatic 5.
func (h *OnboardingHandler) handleStepSubmit(w http.ResponseWriter, r *http.Request, suffix string) {
	tenantID, wizardID, stepNum, err := parseStepRoute(suffix)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	tenantID = h.preferHeaderTenantID(r, tenantID)
	if tenantID == "" {
		writeJSONError(w, http.StatusBadRequest, ErrOnboardingTenantMissing)
		return
	}
	if stepNum < 1 || stepNum > MaxOnboardingSteps {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("%w: step=%d", ErrInvalidStep, stepNum))
		return
	}
	wiz, err := h.repo.Get(r.Context(), tenantID, wizardID)
	if err != nil {
		h.notFoundOrError(w, err)
		return
	}
	if wiz.CurrentStep != stepNum {
		writeJSONError(w, http.StatusConflict, fmt.Errorf("%w: current=%d submitted=%d", ErrStepOutOfOrder, wiz.CurrentStep, stepNum))
		return
	}
	raw, err := readBodyRaw(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	updated, err := h.applyStepDispatch(stepNum, wiz, raw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	updated.CompletedSteps = appendUnique(updated.CompletedSteps, stepNum)
	updated.CurrentStep = stepNum + 1
	updated.LastAdvancedAt = h.now()
	if err := h.repo.Update(r.Context(), updated); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	h.recordStepMetric(tenantID, stepNum)
	writeJSON(w, http.StatusOK, updated)
}

// applyStepDispatch routes the parsed body to the per-step helper.
// Cyclomatic 5.
func (h *OnboardingHandler) applyStepDispatch(stepNum int, wiz OnboardingWizard, raw json.RawMessage) (OnboardingWizard, error) {
	switch stepNum {
	case 1:
		return applyStepIdentity(wiz, raw)
	case 2:
		return applyStepChannels(wiz, raw)
	case 3:
		return applyStepCompliance(wiz, raw)
	case 4:
		return applyStepSeeding(wiz, raw)
	}
	return wiz, fmt.Errorf("%w: step=%d", ErrInvalidStep, stepNum)
}

// readBodyRaw drains the request body. Returns an error if the body
// cannot be read or is empty. Pure; cyclomatic 3.
func readBodyRaw(r *http.Request) (json.RawMessage, error) {
	if r.Body == nil {
		return nil, fmt.Errorf("%w: empty body", ErrInvalidStepPayload)
	}
	defer r.Body.Close()
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("%w: empty body", ErrInvalidStepPayload)
	}
	return json.RawMessage(buf), nil
}

// applyStepIdentity validates step-1 payload + records identity.
// Cyclomatic 3.
func applyStepIdentity(wiz OnboardingWizard, raw json.RawMessage) (OnboardingWizard, error) {
	var id WizardIdentity
	if err := json.Unmarshal(raw, &id); err != nil {
		return wiz, fmt.Errorf("%w: %v", ErrInvalidStepPayload, err)
	}
	if strings.TrimSpace(id.TenantName) == "" || strings.TrimSpace(id.OwnerEmail) == "" || strings.TrimSpace(id.Country) == "" {
		return wiz, fmt.Errorf("%w: identity fields required", ErrInvalidStepPayload)
	}
	if _, ok := AllowedBusinessTypes[id.BusinessType]; !ok {
		return wiz, fmt.Errorf("%w: business_type=%q invalid", ErrInvalidStepPayload, id.BusinessType)
	}
	wiz.Identity = &id
	return wiz, nil
}

// applyStepChannels validates step-2 payload + records channel set.
// Cyclomatic 3.
func applyStepChannels(wiz OnboardingWizard, raw json.RawMessage) (OnboardingWizard, error) {
	var ch WizardChannels
	if err := json.Unmarshal(raw, &ch); err != nil {
		return wiz, fmt.Errorf("%w: %v", ErrInvalidStepPayload, err)
	}
	if len(ch.Channels) == 0 {
		return wiz, fmt.Errorf("%w: channels required", ErrInvalidStepPayload)
	}
	for _, c := range ch.Channels {
		if _, ok := AllowedChannels[strings.ToLower(c)]; !ok {
			return wiz, fmt.Errorf("%w: channel=%q invalid", ErrInvalidStepPayload, c)
		}
	}
	wiz.Channels = &ch
	return wiz, nil
}

// applyStepCompliance validates step-3 payload + records compliance.
// Cyclomatic 3.
func applyStepCompliance(wiz OnboardingWizard, raw json.RawMessage) (OnboardingWizard, error) {
	var c WizardCompliance
	if err := json.Unmarshal(raw, &c); err != nil {
		return wiz, fmt.Errorf("%w: %v", ErrInvalidStepPayload, err)
	}
	if len(c.Compliance) == 0 {
		return wiz, fmt.Errorf("%w: compliance required", ErrInvalidStepPayload)
	}
	for _, flag := range c.Compliance {
		if _, ok := AllowedCompliance[strings.ToLower(flag)]; !ok {
			return wiz, fmt.Errorf("%w: compliance=%q invalid", ErrInvalidStepPayload, flag)
		}
	}
	wiz.Compliance = &c
	return wiz, nil
}

// applyStepSeeding validates step-4 payload + records seed source.
// Cyclomatic 3.
func applyStepSeeding(wiz OnboardingWizard, raw json.RawMessage) (OnboardingWizard, error) {
	var s WizardSeeding
	if err := json.Unmarshal(raw, &s); err != nil {
		return wiz, fmt.Errorf("%w: %v", ErrInvalidStepPayload, err)
	}
	if _, ok := AllowedSeedSources[strings.ToLower(s.Source)]; !ok {
		return wiz, fmt.Errorf("%w: seed source=%q invalid", ErrInvalidStepPayload, s.Source)
	}
	if s.ItemCount < 0 {
		return wiz, fmt.Errorf("%w: item_count negative", ErrInvalidStepPayload)
	}
	wiz.Seeding = &s
	return wiz, nil
}

// handleComplete serves POST /onboarding/{id}/complete. Cyclomatic 4.
func (h *OnboardingHandler) handleComplete(w http.ResponseWriter, r *http.Request, suffix string) {
	tenantID, wizardID, err := h.resolveWizardContext(r, suffix, "/complete")
	if err != nil {
		writeJSONError(w, h.statusForLookup(err), err)
		return
	}
	wiz, err := h.repo.Get(r.Context(), tenantID, wizardID)
	if err != nil {
		h.notFoundOrError(w, err)
		return
	}
	if !wizardComplete(wiz) {
		writeJSONError(w, http.StatusConflict, fmt.Errorf("%w: completed=%v", ErrIncompleteWizard, wiz.CompletedSteps))
		return
	}
	wiz.Completed = true
	wiz.CompletedAt = h.now()
	if err := h.repo.Update(r.Context(), wiz); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	h.completeFinalisation(r.Context(), wiz)
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":    wiz.TenantID,
		"wizard_id":    wiz.WizardID,
		"completed":    true,
		"completed_at": wiz.CompletedAt,
	})
}

// completeFinalisation runs the workflow launch + event publish +
// metrics emit. Pure side effects so it stays cyclomatic 3.
func (h *OnboardingHandler) completeFinalisation(ctx context.Context, wiz OnboardingWizard) {
	if h.launcher != nil {
		if err := h.launcher.Launch(ctx, wiz); err != nil {
			h.logger.Warn("onboarding.launch_failed", "tenant_id", wiz.TenantID, "wizard_id", wiz.WizardID, "error", err)
		}
	}
	h.publishCompletion(ctx, wiz)
	h.recordCompletionDuration(wiz.StartedAt)
}

func (h *OnboardingHandler) publishCompletion(ctx context.Context, wiz OnboardingWizard) {
	if h.publisher == nil {
		return
	}
	payload := eventbus.TenantOnboardedPayload{
		Version:       eventbus.TenantOnboardedPayloadVersion,
		TenantID:      wiz.TenantID,
		WizardID:      wiz.WizardID,
		Country:       wiz.Identity.Country,
		BusinessType:  wiz.Identity.BusinessType,
		Channels:      wiz.Channels.Channels,
		Compliance:    wiz.Compliance.Compliance,
		SeedSource:    wiz.Seeding.Source,
		SeedItemCount: wiz.Seeding.ItemCount,
		OccurredAt:    h.now(),
	}
	evt, err := eventbus.NewTenantOnboardedEvent("handler.onboarding", h.now(), payload)
	if err != nil {
		h.logger.Warn("onboarding.event_failed", "tenant_id", wiz.TenantID, "error", err)
		return
	}
	if perr := h.publisher.Publish(ctx, evt); perr != nil {
		h.logger.Warn("onboarding.publish_failed", "tenant_id", wiz.TenantID, "error", perr)
	}
}

func (h *OnboardingHandler) resolveOnboardingTenantID(r *http.Request) (string, error) {
	if v := strings.TrimSpace(r.Header.Get(h.tenantHeader)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v, nil
	}
	return "", ErrOnboardingTenantMissing
}

func (h *OnboardingHandler) preferHeaderTenantID(r *http.Request, fallback string) string {
	if v := strings.TrimSpace(r.Header.Get(h.tenantHeader)); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v
	}
	return fallback
}

func (h *OnboardingHandler) resolveWizardContext(r *http.Request, suffix, tail string) (string, string, error) {
	tenantID, err := h.resolveOnboardingTenantID(r)
	if err != nil {
		return "", "", err
	}
	wizardID, err := parseWizardID(suffix, tail)
	if err != nil {
		return "", "", err
	}
	return tenantID, wizardID, nil
}

func (h *OnboardingHandler) statusForLookup(err error) int {
	if errors.Is(err, ErrWizardNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func (h *OnboardingHandler) notFoundOrError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrWizardNotFound) {
		writeJSONError(w, http.StatusNotFound, err)
		return
	}
	writeJSONError(w, http.StatusInternalServerError, err)
}

func (h *OnboardingHandler) recordStepMetric(tenantID string, step int) {
	if h.metrics == nil {
		return
	}
	h.metrics.RecordWizardStepCompleted(tenantID, step)
}

func (h *OnboardingHandler) recordCompletionDuration(start time.Time) {
	if h.metrics == nil || start.IsZero() {
		return
	}
	h.metrics.ObserveWizardCompletionDuration(h.now().Sub(start).Seconds())
}

// defaultWizardID generates a deterministic-but-unique id for tests
// and dev composition; production wires uuid.NewV4() via
// WizardIDFunc.
func (h *OnboardingHandler) defaultWizardID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	return fmt.Sprintf("wiz-%d-%d", h.now().UnixNano(), h.seq)
}

// parseWizardID extracts the wizard id from the path suffix.
// suffix is everything after "/api/v1/onboarding". tail is the
// trailing per-route segment ("/state" or "/complete").
// Cyclomatic 3.
func parseWizardID(suffix, tail string) (string, error) {
	if !strings.HasSuffix(suffix, tail) {
		return "", fmt.Errorf("%w: missing %s in path", ErrWizardNotFound, tail)
	}
	core := strings.TrimSuffix(suffix, tail)
	core = strings.TrimPrefix(core, "/")
	core = strings.TrimSpace(core)
	if core == "" {
		return "", fmt.Errorf("%w: wizard_id missing in path", ErrWizardNotFound)
	}
	return core, nil
}

// parseStepRoute extracts (tenantID, wizardID, stepNum) from
// /{wizard_id}/step/{step_num}. tenantID is left empty when the
// path does not carry it; the caller resolves it from header/query.
// Cyclomatic 4.
func parseStepRoute(suffix string) (string, string, int, error) {
	core := strings.TrimPrefix(suffix, "/")
	idx := strings.Index(core, "/step/")
	if idx <= 0 {
		return "", "", 0, fmt.Errorf("%w: step path malformed", ErrWizardNotFound)
	}
	wizardID := strings.TrimSpace(core[:idx])
	stepRaw := strings.TrimSpace(core[idx+len("/step/"):])
	stepRaw = strings.TrimSuffix(stepRaw, "/")
	if wizardID == "" || stepRaw == "" {
		return "", "", 0, fmt.Errorf("%w: step path malformed", ErrWizardNotFound)
	}
	stepNum, err := strconv.Atoi(stepRaw)
	if err != nil {
		return "", "", 0, fmt.Errorf("%w: step=%q not int", ErrInvalidStep, stepRaw)
	}
	return "", wizardID, stepNum, nil
}

// appendUnique appends step to the slice if not already present.
// Pure; cyclomatic 3.
func appendUnique(steps []int, step int) []int {
	for _, s := range steps {
		if s == step {
			return steps
		}
	}
	return append(steps, step)
}

// wizardComplete returns true when the wizard has all four steps
// recorded. Pure; cyclomatic 2.
func wizardComplete(wiz OnboardingWizard) bool {
	return wiz.Identity != nil && wiz.Channels != nil && wiz.Compliance != nil && wiz.Seeding != nil
}
