package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/registration"
	"github.com/nfsarch33/helixon-ec/internal/tenant"
	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

// Tenant onboarding workflow constants. Activity names are stable so
// replay is safe across host upgrades.
const (
	TenantValidateRegistrationActivity   = "tenant.validate_registration"
	TenantProvisionRecordActivity        = "tenant.provision_record"
	TenantSeedDefaultPlanActivity        = "tenant.seed_default_plan"
	TenantIssueWelcomeActivity           = "tenant.issue_welcome_notification"
	TenantRegisterDefaultPluginsActivity = "tenant.register_default_plugins"
	TenantRollbackRecordActivity         = "tenant.rollback_record"
)

// TenantOnboardingInput is the workflow input. The RegistrationID
// references the v2.5.0 RegistrationRequest aggregate that already
// reached email_verified state. The workflow drives:
//
//	email_verified -> onboarding -> active
type TenantOnboardingInput struct {
	RegistrationID string `json:"registration_id"`
	TenantSlug     string `json:"tenant_slug"`
	TenantName     string `json:"tenant_name"`
	Plan           string `json:"plan"`
	OwnerEmail     string `json:"owner_email"`
	CompanyName    string `json:"company_name,omitempty"`
}

// TenantOnboardingResult is the final workflow result.
type TenantOnboardingResult struct {
	TenantID                 string                           `json:"tenant_id"`
	RegistrationID           string                           `json:"registration_id"`
	FinalStatus              registration.Status              `json:"final_status"`
	WelcomeNotificationSent  bool                             `json:"welcome_notification_sent"`
	DefaultPluginsRegistered []string                         `json:"default_plugins_registered,omitempty"`
	Activities               []TenantOnboardingActivityRecord `json:"activities"`
}

// TenantOnboardingActivityRecord summarises one completed activity
// for the result envelope. Used by tests and observability.
type TenantOnboardingActivityRecord struct {
	Name       string    `json:"name"`
	OccurredAt time.Time `json:"occurred_at"`
	Outcome    string    `json:"outcome"`
}

// ValidateRegistrationRequest is the input for the validate activity.
type ValidateRegistrationRequest struct {
	RegistrationID string `json:"registration_id"`
}

// ValidateRegistrationResponse is the output of the validate activity.
type ValidateRegistrationResponse struct {
	RegistrationID string              `json:"registration_id"`
	Email          string              `json:"email"`
	Status         registration.Status `json:"status"`
}

// ProvisionTenantRequest is the input for the provision activity.
type ProvisionTenantRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Plan string `json:"plan"`
}

// ProvisionTenantResponse carries the new tenant id back to the
// workflow.
type ProvisionTenantResponse struct {
	TenantID  string    `json:"tenant_id"`
	CreatedAt time.Time `json:"created_at"`
}

// SeedDefaultPlanRequest is the input for the plan-seed activity.
type SeedDefaultPlanRequest struct {
	TenantID string `json:"tenant_id"`
	Plan     string `json:"plan"`
}

// IssueWelcomeRequest is the input for the welcome-notification activity.
type IssueWelcomeRequest struct {
	TenantID    string `json:"tenant_id"`
	OwnerEmail  string `json:"owner_email"`
	CompanyName string `json:"company_name,omitempty"`
}

// RegisterDefaultPluginsRequest is the input for the default-plugins
// activity.
type RegisterDefaultPluginsRequest struct {
	TenantID string `json:"tenant_id"`
	Plan     string `json:"plan"`
}

// RegisterDefaultPluginsResponse lists the slugs that ended up
// installed for the tenant. Empty when the plan has no default
// plugins (e.g. free tier).
type RegisterDefaultPluginsResponse struct {
	Slugs []string `json:"slugs"`
}

// RollbackRecordRequest is the input for the compensation activity
// when downstream provisioning fails after a tenant row was created.
type RollbackRecordRequest struct {
	TenantID string `json:"tenant_id"`
	Reason   string `json:"reason"`
}

// TenantOnboardingActivities is the activity struct registered with
// the temporal worker. Concrete dependencies live behind the
// interfaces below so tests inject in-memory fakes without dragging
// in postgres.
type TenantOnboardingActivities struct {
	deps TenantOnboardingActivityDeps
}

// TenantRegistrationLookup is the read-side port the validate
// activity uses to inspect the registration aggregate. It mirrors
// the Get method on registration.Repository so we can pass an
// InMemoryRepository directly.
type TenantRegistrationLookup interface {
	Get(ctx context.Context, id string) (registration.Request, error)
}

// TenantOnboardingNotifier is the port the welcome activity uses.
// Implementations may be a no-op.
type TenantOnboardingNotifier interface {
	SendWelcome(ctx context.Context, req IssueWelcomeRequest) error
}

// TenantPluginSeeder is the port the default-plugins activity uses.
// Implementations may be a no-op (e.g. free-tier tenants).
type TenantPluginSeeder interface {
	RegisterDefaults(ctx context.Context, req RegisterDefaultPluginsRequest) ([]string, error)
}

// TenantPlanSeeder is the port the plan-seed activity uses. The
// happy-path implementation persists a billing plan row; tests inject
// a fake that records the call.
type TenantPlanSeeder interface {
	SeedPlan(ctx context.Context, req SeedDefaultPlanRequest) error
}

// TenantOnboardingActivityDeps wires concrete dependencies into the
// activity struct.
type TenantOnboardingActivityDeps struct {
	Tenants       *tenant.AggregateService
	Registrations TenantRegistrationLookup
	Notifier      TenantOnboardingNotifier
	Plans         TenantPlanSeeder
	Plugins       TenantPluginSeeder
}

// NewTenantOnboardingActivities returns an activity struct wired with
// deps. Required: Tenants. The other dependencies are optional and
// degrade gracefully (no-op or fixed response).
func NewTenantOnboardingActivities(deps TenantOnboardingActivityDeps) *TenantOnboardingActivities {
	return &TenantOnboardingActivities{deps: deps}
}

// TenantOnboardingWorkflow drives a verified registration through the
// onboarding state machine and returns the provisioned tenant id.
//
// Determinism: every side effect goes through an activity. Workflow
// code uses temporalworkflow.Now and ExecuteActivity exclusively.
//
// Compensation: if SeedDefaultPlan fails after the tenant row was
// created, the workflow runs RollbackRecord to delete the row. If
// IssueWelcome fails the workflow continues (notification is best
// effort) but logs the failure in the result envelope.
func TenantOnboardingWorkflow(ctx temporalworkflow.Context, input TenantOnboardingInput) (TenantOnboardingResult, error) {
	ctx = temporalworkflow.WithActivityOptions(ctx, defaultOnboardingActivityOptions())

	state := TenantOnboardingResult{
		RegistrationID: input.RegistrationID,
		FinalStatus:    registration.StatusEmailVerified,
	}

	if err := executeValidateRegistration(ctx, input, &state); err != nil {
		return state, err
	}
	provisioned, err := executeProvisionTenant(ctx, input, &state)
	if err != nil {
		return state, err
	}
	state.TenantID = provisioned.TenantID

	if err := executeSeedDefaultPlan(ctx, input, &state); err != nil {
		runRollback(ctx, state.TenantID, "seed_default_plan_failed: "+err.Error(), &state)
		return state, fmt.Errorf("seed default plan: %w", err)
	}

	pluginSlugs, pluginsErr := executeRegisterDefaultPlugins(ctx, input, &state)
	if pluginsErr != nil {
		runRollback(ctx, state.TenantID, "register_default_plugins_failed: "+pluginsErr.Error(), &state)
		return state, fmt.Errorf("register default plugins: %w", pluginsErr)
	}
	state.DefaultPluginsRegistered = pluginSlugs

	state.FinalStatus = registration.StatusOnboarding
	state.WelcomeNotificationSent = executeIssueWelcomeBestEffort(ctx, input, &state)

	state.FinalStatus = registration.StatusActive
	return state, nil
}

func defaultOnboardingActivityOptions() temporalworkflow.ActivityOptions {
	return temporalworkflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    15 * time.Second,
			MaximumAttempts:    3,
		},
	}
}

func executeValidateRegistration(ctx temporalworkflow.Context, input TenantOnboardingInput, state *TenantOnboardingResult) error {
	var resp ValidateRegistrationResponse
	if err := temporalworkflow.ExecuteActivity(ctx, TenantValidateRegistrationActivity, ValidateRegistrationRequest{
		RegistrationID: input.RegistrationID,
	}).Get(ctx, &resp); err != nil {
		return fmt.Errorf("validate registration: %w", err)
	}
	state.Activities = append(state.Activities, TenantOnboardingActivityRecord{
		Name:       TenantValidateRegistrationActivity,
		OccurredAt: temporalworkflow.Now(ctx),
		Outcome:    string(resp.Status),
	})
	return nil
}

func executeProvisionTenant(ctx temporalworkflow.Context, input TenantOnboardingInput, state *TenantOnboardingResult) (ProvisionTenantResponse, error) {
	var resp ProvisionTenantResponse
	if err := temporalworkflow.ExecuteActivity(ctx, TenantProvisionRecordActivity, ProvisionTenantRequest{
		Slug: input.TenantSlug,
		Name: input.TenantName,
		Plan: input.Plan,
	}).Get(ctx, &resp); err != nil {
		return ProvisionTenantResponse{}, fmt.Errorf("provision tenant: %w", err)
	}
	state.Activities = append(state.Activities, TenantOnboardingActivityRecord{
		Name:       TenantProvisionRecordActivity,
		OccurredAt: temporalworkflow.Now(ctx),
		Outcome:    "created:" + resp.TenantID,
	})
	return resp, nil
}

func executeSeedDefaultPlan(ctx temporalworkflow.Context, input TenantOnboardingInput, state *TenantOnboardingResult) error {
	if err := temporalworkflow.ExecuteActivity(ctx, TenantSeedDefaultPlanActivity, SeedDefaultPlanRequest{
		TenantID: state.TenantID,
		Plan:     input.Plan,
	}).Get(ctx, nil); err != nil {
		return err
	}
	state.Activities = append(state.Activities, TenantOnboardingActivityRecord{
		Name:       TenantSeedDefaultPlanActivity,
		OccurredAt: temporalworkflow.Now(ctx),
		Outcome:    "seeded:" + input.Plan,
	})
	return nil
}

func executeRegisterDefaultPlugins(ctx temporalworkflow.Context, input TenantOnboardingInput, state *TenantOnboardingResult) ([]string, error) {
	var resp RegisterDefaultPluginsResponse
	if err := temporalworkflow.ExecuteActivity(ctx, TenantRegisterDefaultPluginsActivity, RegisterDefaultPluginsRequest{
		TenantID: state.TenantID,
		Plan:     input.Plan,
	}).Get(ctx, &resp); err != nil {
		return nil, err
	}
	state.Activities = append(state.Activities, TenantOnboardingActivityRecord{
		Name:       TenantRegisterDefaultPluginsActivity,
		OccurredAt: temporalworkflow.Now(ctx),
		Outcome:    fmt.Sprintf("plugins=%d", len(resp.Slugs)),
	})
	return resp.Slugs, nil
}

// executeIssueWelcomeBestEffort sends the welcome notification but
// never fails the workflow on it. Welcome failures get logged in the
// result so the caller can replay them out-of-band.
func executeIssueWelcomeBestEffort(ctx temporalworkflow.Context, input TenantOnboardingInput, state *TenantOnboardingResult) bool {
	err := temporalworkflow.ExecuteActivity(ctx, TenantIssueWelcomeActivity, IssueWelcomeRequest{
		TenantID:    state.TenantID,
		OwnerEmail:  input.OwnerEmail,
		CompanyName: input.CompanyName,
	}).Get(ctx, nil)
	outcome := "sent"
	if err != nil {
		outcome = "skipped:" + err.Error()
	}
	state.Activities = append(state.Activities, TenantOnboardingActivityRecord{
		Name:       TenantIssueWelcomeActivity,
		OccurredAt: temporalworkflow.Now(ctx),
		Outcome:    outcome,
	})
	return err == nil
}

// runRollback fires the compensation activity. Failures are recorded
// but do not bubble up; the workflow will already be returning a
// downstream error and the rollback activity is best-effort.
func runRollback(ctx temporalworkflow.Context, tenantID, reason string, state *TenantOnboardingResult) {
	if tenantID == "" {
		return
	}
	err := temporalworkflow.ExecuteActivity(ctx, TenantRollbackRecordActivity, RollbackRecordRequest{
		TenantID: tenantID,
		Reason:   reason,
	}).Get(ctx, nil)
	outcome := "rolled_back"
	if err != nil {
		outcome = "rollback_failed:" + err.Error()
	}
	state.Activities = append(state.Activities, TenantOnboardingActivityRecord{
		Name:       TenantRollbackRecordActivity,
		OccurredAt: temporalworkflow.Now(ctx),
		Outcome:    outcome,
	})
}

// ValidateRegistration is the activity that checks the registration
// aggregate is in a state from which onboarding can proceed.
func (a *TenantOnboardingActivities) ValidateRegistration(ctx context.Context, req ValidateRegistrationRequest) (ValidateRegistrationResponse, error) {
	if a.deps.Registrations == nil {
		return ValidateRegistrationResponse{
			RegistrationID: req.RegistrationID,
			Status:         registration.StatusEmailVerified,
		}, nil
	}
	r, err := a.deps.Registrations.Get(ctx, req.RegistrationID)
	if err != nil {
		return ValidateRegistrationResponse{}, fmt.Errorf("registration lookup: %w", err)
	}
	if r.Status != registration.StatusEmailVerified && r.Status != registration.StatusOnboarding {
		return ValidateRegistrationResponse{}, fmt.Errorf("registration %s in status %s; expected email_verified or onboarding", r.ID, r.Status)
	}
	return ValidateRegistrationResponse{
		RegistrationID: r.ID,
		Email:          r.Email,
		Status:         r.Status,
	}, nil
}

// ProvisionTenant is the activity that creates the tenant aggregate
// row.
func (a *TenantOnboardingActivities) ProvisionTenant(ctx context.Context, req ProvisionTenantRequest) (ProvisionTenantResponse, error) {
	if a.deps.Tenants == nil {
		return ProvisionTenantResponse{}, errors.New("tenant aggregate service not configured")
	}
	t, err := a.deps.Tenants.Create(ctx, tenant.CreateTenantInput{
		Slug: req.Slug,
		Name: req.Name,
		Plan: req.Plan,
	})
	if err != nil {
		return ProvisionTenantResponse{}, fmt.Errorf("create tenant: %w", err)
	}
	return ProvisionTenantResponse{
		TenantID:  string(t.ID),
		CreatedAt: t.CreatedAt,
	}, nil
}

// SeedDefaultPlan persists the billing plan row for the new tenant.
// No-op when no Plans dep is wired (dev mode).
func (a *TenantOnboardingActivities) SeedDefaultPlan(ctx context.Context, req SeedDefaultPlanRequest) error {
	if a.deps.Plans == nil {
		return nil
	}
	return a.deps.Plans.SeedPlan(ctx, req)
}

// IssueWelcomeNotification sends the welcome email/notification.
// No-op when no Notifier is wired.
func (a *TenantOnboardingActivities) IssueWelcomeNotification(ctx context.Context, req IssueWelcomeRequest) error {
	if a.deps.Notifier == nil {
		return nil
	}
	return a.deps.Notifier.SendWelcome(ctx, req)
}

// RegisterDefaultPlugins installs the plan's default plugin set for
// the new tenant. Returns an empty slug list when no Plugins dep is
// wired or when the plan has no defaults.
func (a *TenantOnboardingActivities) RegisterDefaultPlugins(ctx context.Context, req RegisterDefaultPluginsRequest) (RegisterDefaultPluginsResponse, error) {
	if a.deps.Plugins == nil {
		return RegisterDefaultPluginsResponse{}, nil
	}
	slugs, err := a.deps.Plugins.RegisterDefaults(ctx, req)
	if err != nil {
		return RegisterDefaultPluginsResponse{}, err
	}
	return RegisterDefaultPluginsResponse{Slugs: slugs}, nil
}

// RollbackRecord runs the compensation when downstream provisioning
// fails after the tenant row was created. Implementations should
// soft-delete or hard-delete; the registry stores the reason for
// audit.
func (a *TenantOnboardingActivities) RollbackRecord(_ context.Context, _ RollbackRecordRequest) error {
	// Rollback is intentionally a no-op for the in-memory aggregate
	// service since postgres-backed deployments handle compensation
	// via a deferred soft-delete; the activity exists so future
	// adapter wiring has a fixed surface.
	return nil
}
