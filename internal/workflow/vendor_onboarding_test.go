package workflow

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

type fakeVendorNotifier struct {
	calls []VendorNotifyRequest
	err   error
}

func (f *fakeVendorNotifier) Notify(_ context.Context, req VendorNotifyRequest) error {
	f.calls = append(f.calls, req)
	return f.err
}

func newVendorOnboardingActivities(t *testing.T) (*VendorOnboardingActivities, *fakeVendorNotifier) {
	t.Helper()
	notifier := &fakeVendorNotifier{}
	seq := 0
	activities := NewVendorOnboardingActivities(VendorOnboardingDeps{
		ExistingVendorNames: func(_ context.Context, _ string) ([]string, error) {
			return []string{"Existing Vendor"}, nil
		},
		CreateVendor: func(_ context.Context, req VendorCreateRecordRequest) (VendorCreateRecordResponse, error) {
			seq++
			return VendorCreateRecordResponse{
				VendorID:  fmt.Sprintf("v-%d", seq),
				CreatedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
			}, nil
		},
		Notifier: notifier.Notify,
	})
	return activities, notifier
}

func runVendorOnboarding(t *testing.T, acts *VendorOnboardingActivities, input VendorOnboardingInput) (VendorOnboardingResult, error) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))

	env.RegisterActivityWithOptions(acts.Verify, activity.RegisterOptions{Name: VendorVerifyActivity})
	env.RegisterActivityWithOptions(acts.ApprovalGate, activity.RegisterOptions{Name: VendorApprovalGateActivity})
	env.RegisterActivityWithOptions(acts.CreateRecord, activity.RegisterOptions{Name: VendorCreateRecordActivity})
	env.RegisterActivityWithOptions(acts.Notify, activity.RegisterOptions{Name: VendorNotifyActivity})
	env.RegisterActivityWithOptions(acts.Rollback, activity.RegisterOptions{Name: VendorRollbackActivity})

	env.ExecuteWorkflow(VendorOnboardingWorkflow, input)
	if err := env.GetWorkflowError(); err != nil {
		return VendorOnboardingResult{}, err
	}
	var out VendorOnboardingResult
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return out, nil
}

func TestVendorOnboarding_AutoApproveSmallVendor(t *testing.T) {
	t.Parallel()
	acts, notifier := newVendorOnboardingActivities(t)

	result, err := runVendorOnboarding(t, acts, VendorOnboardingInput{
		TenantID:               "t1",
		VendorName:             "Small Vendor",
		ContactEmail:           "small@vendor.com",
		ProductCategory:        "accessories",
		CommissionAgreementBPS: 1000,
		EstimatedMonthlyVolume: 500000, // A$5K < A$10K threshold
	})
	if err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if result.Status != "approved" {
		t.Fatalf("status = %s, want approved", result.Status)
	}
	if result.VendorID == "" {
		t.Fatal("expected vendor_id, got empty")
	}
	if len(notifier.calls) != 1 || !notifier.calls[0].Approved {
		t.Fatalf("expected approval notification, got %v", notifier.calls)
	}
}

func TestVendorOnboarding_OperatorApproveLargeVendor(t *testing.T) {
	t.Parallel()
	acts, _ := newVendorOnboardingActivities(t)

	result, err := runVendorOnboarding(t, acts, VendorOnboardingInput{
		TenantID:               "t1",
		VendorName:             "Big Vendor",
		ContactEmail:           "big@vendor.com",
		ProductCategory:        "electronics",
		CommissionAgreementBPS: 2000,
		EstimatedMonthlyVolume: 5000000, // A$50K > A$10K threshold
	})
	if err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if result.Status != "approved" {
		t.Fatalf("status = %s, want approved", result.Status)
	}

	var gateOutcome string
	for _, a := range result.Activities {
		if a.Name == VendorApprovalGateActivity {
			gateOutcome = a.Outcome
		}
	}
	if gateOutcome != "approved=true method=operator" {
		t.Fatalf("gate outcome = %s, want operator approval", gateOutcome)
	}
}

func TestVendorOnboarding_RejectDuplicate(t *testing.T) {
	t.Parallel()
	acts, notifier := newVendorOnboardingActivities(t)

	result, err := runVendorOnboarding(t, acts, VendorOnboardingInput{
		TenantID:               "t1",
		VendorName:             "Existing Vendor", // duplicate
		ContactEmail:           "dup@vendor.com",
		ProductCategory:        "clothing",
		CommissionAgreementBPS: 1500,
		EstimatedMonthlyVolume: 200000,
	})
	if err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if result.Status != "rejected" {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	if result.Reason != "duplicate vendor name" {
		t.Fatalf("reason = %s, want duplicate vendor name", result.Reason)
	}
	if len(notifier.calls) != 1 || notifier.calls[0].Approved {
		t.Fatalf("expected rejection notification, got %v", notifier.calls)
	}
}

func TestVendorOnboarding_DuplicateDetection(t *testing.T) {
	t.Parallel()
	acts, _ := newVendorOnboardingActivities(t)

	result, err := runVendorOnboarding(t, acts, VendorOnboardingInput{
		TenantID:               "t1",
		VendorName:             "existing vendor", // case-insensitive match
		ContactEmail:           "test@vendor.com",
		ProductCategory:        "food",
		CommissionAgreementBPS: 1000,
		EstimatedMonthlyVolume: 100000,
	})
	if err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if result.Status != "rejected" {
		t.Fatalf("status = %s, want rejected (case-insensitive duplicate)", result.Status)
	}
}

func TestVendorOnboarding_SagaRollback(t *testing.T) {
	t.Parallel()
	notifier := &fakeVendorNotifier{}
	acts := NewVendorOnboardingActivities(VendorOnboardingDeps{
		ExistingVendorNames: func(_ context.Context, _ string) ([]string, error) {
			return nil, nil
		},
		CreateVendor: func(_ context.Context, _ VendorCreateRecordRequest) (VendorCreateRecordResponse, error) {
			return VendorCreateRecordResponse{}, errors.New("db connection failed")
		},
		Notifier: notifier.Notify,
	})

	_, err := runVendorOnboarding(t, acts, VendorOnboardingInput{
		TenantID:               "t1",
		VendorName:             "Failing Vendor",
		ContactEmail:           "fail@vendor.com",
		ProductCategory:        "misc",
		CommissionAgreementBPS: 1000,
		EstimatedMonthlyVolume: 200000,
	})
	if err == nil {
		t.Fatal("expected workflow to fail when vendor creation fails")
	}
}
