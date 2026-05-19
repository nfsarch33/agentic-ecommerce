package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/registration"
	"github.com/nfsarch33/helixon-ec/internal/tenant"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

type fakePlanSeeder struct {
	calls     []SeedDefaultPlanRequest
	alwaysErr bool
}

func (f *fakePlanSeeder) SeedPlan(_ context.Context, req SeedDefaultPlanRequest) error {
	f.calls = append(f.calls, req)
	if f.alwaysErr {
		return errors.New("plan-seeder boom")
	}
	return nil
}

type fakeNotifier struct {
	calls     []IssueWelcomeRequest
	shouldErr bool
}

func (f *fakeNotifier) SendWelcome(_ context.Context, req IssueWelcomeRequest) error {
	f.calls = append(f.calls, req)
	if f.shouldErr {
		return errors.New("welcome boom")
	}
	return nil
}

type fakePluginSeeder struct {
	calls    []RegisterDefaultPluginsRequest
	response []string
	err      error
}

func (f *fakePluginSeeder) RegisterDefaults(_ context.Context, req RegisterDefaultPluginsRequest) ([]string, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.response...), nil
}

func newDeterministicOnboardingActivities(t *testing.T) (*TenantOnboardingActivities, *fakeNotifier, *fakePlanSeeder, *fakePluginSeeder) {
	t.Helper()
	repo := registration.NewInMemoryRepository()
	r, err := registration.NewRequest(registration.SubmitInput{
		Email:         "owner@example.com",
		SlugRequested: "tenant-x",
		Now:           time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
	}, time.Hour)
	if err != nil {
		t.Fatalf("new registration: %v", err)
	}
	r, _ = r.MarkVerified(time.Date(2026, 5, 9, 0, 1, 0, 0, time.UTC))
	if err := repo.Create(context.Background(), r); err != nil {
		t.Fatalf("repo.Create: %v", err)
	}

	tenants := tenant.NewAggregateService(tenant.NewInMemoryAggregateRepository())
	notifier := &fakeNotifier{}
	plans := &fakePlanSeeder{}
	plugins := &fakePluginSeeder{response: []string{"stripe-payments"}}

	activities := NewTenantOnboardingActivities(TenantOnboardingActivityDeps{
		Tenants:       tenants,
		Registrations: repo,
		Notifier:      notifier,
		Plans:         plans,
		Plugins:       plugins,
	})
	t.Cleanup(func() { _ = r.ID })
	return activities, notifier, plans, plugins
}

func runOnboardingWorkflow(t *testing.T, activities *TenantOnboardingActivities, input TenantOnboardingInput) (*testsuite.TestWorkflowEnvironment, TenantOnboardingResult, error) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, 5, 9, 0, 5, 0, 0, time.UTC))

	env.RegisterActivityWithOptions(activities.ValidateRegistration, activity.RegisterOptions{Name: TenantValidateRegistrationActivity})
	env.RegisterActivityWithOptions(activities.ProvisionTenant, activity.RegisterOptions{Name: TenantProvisionRecordActivity})
	env.RegisterActivityWithOptions(activities.SeedDefaultPlan, activity.RegisterOptions{Name: TenantSeedDefaultPlanActivity})
	env.RegisterActivityWithOptions(activities.IssueWelcomeNotification, activity.RegisterOptions{Name: TenantIssueWelcomeActivity})
	env.RegisterActivityWithOptions(activities.RegisterDefaultPlugins, activity.RegisterOptions{Name: TenantRegisterDefaultPluginsActivity})
	env.RegisterActivityWithOptions(activities.RollbackRecord, activity.RegisterOptions{Name: TenantRollbackRecordActivity})

	env.ExecuteWorkflow(TenantOnboardingWorkflow, input)
	if err := env.GetWorkflowError(); err != nil {
		return env, TenantOnboardingResult{}, err
	}
	var out TenantOnboardingResult
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return env, out, nil
}

func TestTenantOnboardingWorkflowHappyPath(t *testing.T) {
	t.Parallel()
	activities, notifier, plans, plugins := newDeterministicOnboardingActivities(t)
	input := TenantOnboardingInput{
		RegistrationID: lastRegistrationID(t, activities),
		TenantSlug:     "tenant-x",
		TenantName:     "Tenant X",
		Plan:           "starter",
		OwnerEmail:     "owner@example.com",
		CompanyName:    "Tenant X Pty Ltd",
	}
	_, result, err := runOnboardingWorkflow(t, activities, input)
	if err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if result.FinalStatus != registration.StatusActive {
		t.Fatalf("expected final status active, got %s", result.FinalStatus)
	}
	if result.TenantID == "" {
		t.Fatalf("expected tenant id, got empty")
	}
	if !result.WelcomeNotificationSent {
		t.Fatalf("expected welcome notification sent, got false")
	}
	if len(plans.calls) != 1 || plans.calls[0].Plan != "starter" {
		t.Fatalf("expected 1 plan-seeder call for starter, got %v", plans.calls)
	}
	if len(plugins.calls) != 1 {
		t.Fatalf("expected 1 plugin-seeder call, got %v", plugins.calls)
	}
	if got := result.DefaultPluginsRegistered; len(got) != 1 || got[0] != "stripe-payments" {
		t.Fatalf("expected stripe-payments plugin registered, got %v", got)
	}
	if len(notifier.calls) != 1 || notifier.calls[0].OwnerEmail != "owner@example.com" {
		t.Fatalf("expected welcome to owner@example.com, got %v", notifier.calls)
	}
	expectedActivities := []string{
		TenantValidateRegistrationActivity,
		TenantProvisionRecordActivity,
		TenantSeedDefaultPlanActivity,
		TenantRegisterDefaultPluginsActivity,
		TenantIssueWelcomeActivity,
	}
	if len(result.Activities) != len(expectedActivities) {
		t.Fatalf("expected %d activity records, got %d (%v)", len(expectedActivities), len(result.Activities), result.Activities)
	}
	for i, want := range expectedActivities {
		if result.Activities[i].Name != want {
			t.Fatalf("activity[%d].Name = %s, want %s", i, result.Activities[i].Name, want)
		}
	}
}

func TestTenantOnboardingWorkflowCompensatesOnPlanSeedFailure(t *testing.T) {
	t.Parallel()
	activities, _, plans, _ := newDeterministicOnboardingActivities(t)
	plans.alwaysErr = true
	input := TenantOnboardingInput{
		RegistrationID: lastRegistrationID(t, activities),
		TenantSlug:     "tenant-x",
		TenantName:     "Tenant X",
		Plan:           "starter",
		OwnerEmail:     "owner@example.com",
	}
	_, result, err := runOnboardingWorkflow(t, activities, input)
	if err == nil {
		t.Fatalf("expected workflow to fail when plan seed fails")
	}
	if result.FinalStatus == registration.StatusActive {
		t.Fatalf("expected non-active status, got %s", result.FinalStatus)
	}
}

func TestTenantOnboardingWorkflowWelcomeFailureDoesNotAbortActivation(t *testing.T) {
	t.Parallel()
	activities, notifier, _, _ := newDeterministicOnboardingActivities(t)
	notifier.shouldErr = true
	input := TenantOnboardingInput{
		RegistrationID: lastRegistrationID(t, activities),
		TenantSlug:     "tenant-x",
		TenantName:     "Tenant X",
		Plan:           "starter",
		OwnerEmail:     "owner@example.com",
	}
	_, result, err := runOnboardingWorkflow(t, activities, input)
	if err != nil {
		t.Fatalf("welcome failure should not abort: %v", err)
	}
	if result.WelcomeNotificationSent {
		t.Fatalf("expected welcome to be marked failed")
	}
	if result.FinalStatus != registration.StatusActive {
		t.Fatalf("expected final status active despite welcome failure, got %s", result.FinalStatus)
	}
}

func TestTenantOnboardingWorkflowRejectsUnknownRegistration(t *testing.T) {
	t.Parallel()
	activities, _, _, _ := newDeterministicOnboardingActivities(t)
	input := TenantOnboardingInput{
		RegistrationID: "not-a-real-id",
		TenantSlug:     "tenant-x",
		TenantName:     "Tenant X",
		Plan:           "starter",
		OwnerEmail:     "owner@example.com",
	}
	_, _, err := runOnboardingWorkflow(t, activities, input)
	if err == nil {
		t.Fatalf("expected workflow to fail for unknown registration")
	}
}

// lastRegistrationID walks the in-memory repository through the
// activity dependency to fetch the registration id we created in
// newDeterministicOnboardingActivities. Keeps tests focused on
// behaviour without exporting test-only state from the production
// types.
func lastRegistrationID(t *testing.T, activities *TenantOnboardingActivities) string {
	t.Helper()
	type lister interface {
		List(ctx context.Context, page, perPage int) ([]registration.Request, int, error)
	}
	repo, ok := activities.deps.Registrations.(lister)
	if !ok {
		t.Fatalf("repository missing List method")
	}
	rows, _, err := repo.List(context.Background(), 1, 1)
	if err != nil || len(rows) == 0 {
		t.Fatalf("expected at least one registration row, got err=%v rows=%v", err, rows)
	}
	return rows[0].ID
}
