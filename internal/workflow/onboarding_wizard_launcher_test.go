// File scope: v3.9.1 Existing #10 -- onboarding wizard launcher
// adapter unit tests.
package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/api/handler"
)

func TestOnboardingWizardLauncher_NilClientError(t *testing.T) {
	t.Parallel()
	l := NewOnboardingWizardLauncher(nil, "")
	err := l.Launch(context.Background(), handler.OnboardingWizard{})
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if !strings.Contains(err.Error(), "unconfigured") {
		t.Fatalf("expected unconfigured error, got %v", err)
	}
}

func TestOnboardingWizardLauncher_RequiresIdentity(t *testing.T) {
	t.Parallel()
	l := NewOnboardingWizardLauncher(&stubTemporalClient{}, "")
	err := l.Launch(context.Background(), handler.OnboardingWizard{})
	if err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("expected identity-required error, got %v", err)
	}
}

func TestOnboardingWizardLauncher_DefaultsTaskQueue(t *testing.T) {
	t.Parallel()
	l := NewOnboardingWizardLauncher(&stubTemporalClient{}, "")
	if l.taskQueue != OnboardingTaskQueue {
		t.Fatalf("taskQueue=%q want=%q", l.taskQueue, OnboardingTaskQueue)
	}
}

func TestOnboardingWizardLauncher_HonoursCustomTaskQueue(t *testing.T) {
	t.Parallel()
	l := NewOnboardingWizardLauncher(&stubTemporalClient{}, "custom-queue")
	if l.taskQueue != "custom-queue" {
		t.Fatalf("taskQueue=%q want=custom-queue", l.taskQueue)
	}
}

func TestOnboardingWizardLauncher_LaunchesWithIdentity(t *testing.T) {
	t.Parallel()
	stub := &stubTemporalClient{}
	l := NewOnboardingWizardLauncher(stub, "")
	wiz := handler.OnboardingWizard{
		TenantID: "tenant-1",
		WizardID: "wiz-1",
		Identity: &handler.WizardIdentity{
			TenantName:   "Acme",
			OwnerEmail:   "ops@acme.example",
			Country:      "AU",
			BusinessType: "company",
		},
	}
	if err := l.Launch(context.Background(), wiz); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if stub.invocations != 1 {
		t.Fatalf("invocations=%d want=1", stub.invocations)
	}
	if stub.lastWorkflowName != "TenantOnboardingWorkflow" {
		t.Fatalf("workflow=%q", stub.lastWorkflowName)
	}
	if stub.lastInput.TenantSlug != "tenant-1" {
		t.Fatalf("tenant_slug=%q want=tenant-1", stub.lastInput.TenantSlug)
	}
}

func TestOnboardingWizardLauncher_PropagatesExecuteError(t *testing.T) {
	t.Parallel()
	stub := &stubTemporalClient{err: errors.New("temporal down")}
	l := NewOnboardingWizardLauncher(stub, "")
	wiz := handler.OnboardingWizard{
		TenantID: "tenant-1",
		WizardID: "wiz-1",
		Identity: &handler.WizardIdentity{
			TenantName: "Acme", OwnerEmail: "ops@acme.example", Country: "AU", BusinessType: "company",
		},
	}
	if err := l.Launch(context.Background(), wiz); err == nil {
		t.Fatal("expected propagated execute error")
	}
}
