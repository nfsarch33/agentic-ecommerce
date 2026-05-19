// File scope: v3.9.1 Existing #10 -- onboarding wizard -> Temporal
// workflow launcher.
//
// The v3.9.1 wizard handler stores per-step state in Postgres (table
// onboarding_wizards, migration 0023). On wizard completion the
// handler calls into the launcher defined here, which projects the
// wizard state into a TenantOnboardingInput and starts the existing
// v3.0.0 TenantOnboardingWorkflow.
//
// The launcher implements handler.OnboardingWorkflowLauncher via a
// tiny adapter so the api/handler package does NOT depend on the
// Temporal SDK directly (cite go-clean-architecture: ports and
// adapters; the handler depends on a small port, the cmd/* root
// wires the concrete adapter).
//
// Reuse evidence:
//   - Reuses TenantOnboardingWorkflow from v3.0.0 verbatim.
//   - Reuses temporal.Client.SignalWithStartWorkflow contract.
//   - No new external deps.
package workflow

import (
	"context"

	"github.com/nfsarch33/helixon-ec/internal/api/handler"
	"go.temporal.io/sdk/client"
)

// OnboardingTaskQueue is the canonical task queue the EC-3.0.0
// tenant_onboarding worker subscribes to. The v3.9.1 launcher uses
// the same queue so existing workers pick the workflow up.
const OnboardingTaskQueue = "ec-tenant-onboarding"

// TemporalWorkflowExecutor is the small port the v3.9.1 launcher
// depends on. The production wiring satisfies it via Temporal SDK
// client.Client.ExecuteWorkflow; tests pass a fake.
type TemporalWorkflowExecutor interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error)
}

// OnboardingWizardLauncher is the v3.9.1 adapter that converts a
// completed handler.OnboardingWizard into a TenantOnboardingWorkflow
// invocation.
type OnboardingWizardLauncher struct {
	client       TemporalWorkflowExecutor
	taskQueue    string
	workflowName string
}

// NewOnboardingWizardLauncher returns a launcher backed by the
// supplied Temporal client. Pass `nil` only in tests/dev; production
// composition roots wire the real client.Client.
func NewOnboardingWizardLauncher(c TemporalWorkflowExecutor, taskQueue string) *OnboardingWizardLauncher {
	if taskQueue == "" {
		taskQueue = OnboardingTaskQueue
	}
	return &OnboardingWizardLauncher{
		client:       c,
		taskQueue:    taskQueue,
		workflowName: "TenantOnboardingWorkflow",
	}
}

// Launch implements handler.OnboardingWorkflowLauncher. Maps the
// wizard state to TenantOnboardingInput and starts the workflow.
// Cyclomatic 4.
func (l *OnboardingWizardLauncher) Launch(ctx context.Context, w handler.OnboardingWizard) error {
	if l == nil || l.client == nil {
		return ErrLauncherUnconfigured
	}
	if w.Identity == nil {
		return ErrWizardIdentityNil
	}
	input := TenantOnboardingInput{
		RegistrationID: w.WizardID,
		TenantSlug:     w.TenantID,
		TenantName:     w.Identity.TenantName,
		Plan:           "v391-default",
		OwnerEmail:     w.Identity.OwnerEmail,
		CompanyName:    w.Identity.TenantName,
	}
	options := client.StartWorkflowOptions{
		ID:        "onboarding-" + w.WizardID,
		TaskQueue: l.taskQueue,
	}
	_, err := l.client.ExecuteWorkflow(ctx, options, l.workflowName, input)
	return err
}

// Compile-time guard.
var _ handler.OnboardingWorkflowLauncher = (*OnboardingWizardLauncher)(nil)
