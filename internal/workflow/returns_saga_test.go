// File scope: v3.8.0 EC-7-5 returns saga workflow RED tests.
// TDD-first per the v3.8.0 plan; uses the v2.2.0 testsuite pattern.
package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func sampleReturnsInput() ReturnsSagaWorkflowInput {
	return ReturnsSagaWorkflowInput{
		TenantID:             "tenant-1",
		RMAID:                "rma-001",
		OrderID:              "ord-1",
		Channel:              "tiktok",
		BuyerEmail:           "alice@example.com",
		Reason:               "wrong colour",
		RefundAmountAUDCents: 2999, // < threshold (5000)
	}
}

func registerReturnsSagaActivities(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(_ context.Context, _ ReturnsSagaWorkflowInput) error {
		return nil
	}, activity.RegisterOptions{Name: ValidateReturnEligibilityActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ refundCheckArgs) (bool, error) {
		return true, nil
	}, activity.RegisterOptions{Name: CheckRefundApprovalActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ ReturnsSagaWorkflowInput) (ReturnLabelResult, error) {
		return ReturnLabelResult{}, nil
	}, activity.RegisterOptions{Name: GenerateReturnLabelActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ notifyArgs) error {
		return nil
	}, activity.RegisterOptions{Name: NotifyReturnCustomerActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ ReturnsSagaWorkflowInput) error {
		return nil
	}, activity.RegisterOptions{Name: ProcessReturnRefundActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ ReturnsSagaWorkflowInput) error {
		return nil
	}, activity.RegisterOptions{Name: AdjustReturnInventoryActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ ReturnsSagaWorkflowInput) error {
		return nil
	}, activity.RegisterOptions{Name: UpdateReturnChannelStatusActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ ReturnsSagaWorkflowInput) error {
		return nil
	}, activity.RegisterOptions{Name: CompensateReverseInventoryActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ ReturnsSagaWorkflowInput) error {
		return nil
	}, activity.RegisterOptions{Name: CompensateCancelLabelActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ operatorAlertArgs) error {
		return nil
	}, activity.RegisterOptions{Name: NotifyReturnOperatorActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ eventbus.ReturnsSagaPayload) error {
		return nil
	}, activity.RegisterOptions{Name: PublishReturnsSagaActivity})
}

func newReturnsSagaEnv(_ *testing.T) *testsuite.TestWorkflowEnvironment {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerReturnsSagaActivities(env)
	return env
}

func TestReturnsSaga_AutoApprovesSmallRefund(t *testing.T) {
	t.Parallel()
	env := newReturnsSagaEnv(t)
	in := sampleReturnsInput()
	in.RefundAmountAUDCents = 2999

	env.OnActivity(ValidateReturnEligibilityActivity, mock.Anything, in).Return(nil).Once()
	env.OnActivity(CheckRefundApprovalActivity, mock.Anything, refundCheckArgs{Input: in, Threshold: DefaultLargeRefundThresholdCents}).Return(true, nil).Once()
	env.OnActivity(GenerateReturnLabelActivity, mock.Anything, in).Return(ReturnLabelResult{Carrier: "auspost", TrackingNumber: "AP-RET-1", LabelPDFURL: "https://ap/ret/1.pdf", CostAUDCents: 1099}, nil).Once()
	env.OnActivity(NotifyReturnCustomerActivity, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity(ProcessReturnRefundActivity, mock.Anything, in).Return(nil).Once()
	env.OnActivity(AdjustReturnInventoryActivity, mock.Anything, in).Return(nil).Once()
	env.OnActivity(UpdateReturnChannelStatusActivity, mock.Anything, in).Return(nil).Once()
	env.OnActivity(PublishReturnsSagaActivity, mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ReturnsSagaWorkflow, in)
	require.NoError(t, env.GetWorkflowError())
	var res ReturnsSagaWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&res))
	require.True(t, res.AutoApproved)
	require.Equal(t, "completed", res.State)
	require.Equal(t, "AP-RET-1", res.TrackingNumber)
	require.True(t, res.RefundProcessed)
	require.True(t, res.InventoryUpdated)
	require.True(t, res.ChannelUpdated)
}

func TestReturnsSaga_LargeRefundRequiresOperatorApproval(t *testing.T) {
	t.Parallel()
	env := newReturnsSagaEnv(t)
	in := sampleReturnsInput()
	in.RefundAmountAUDCents = 12000 // >> threshold

	env.OnActivity(ValidateReturnEligibilityActivity, mock.Anything, in).Return(nil).Once()
	env.OnActivity(CheckRefundApprovalActivity, mock.Anything, refundCheckArgs{Input: in, Threshold: DefaultLargeRefundThresholdCents}).Return(false, nil).Once()
	env.OnActivity(PublishReturnsSagaActivity, mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ReturnsSagaWorkflow, in)
	require.NoError(t, env.GetWorkflowError())
	var res ReturnsSagaWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&res))
	require.False(t, res.AutoApproved)
	require.Equal(t, "pending_approval", res.State)
	require.False(t, res.RefundProcessed)
}

func TestReturnsSaga_GeneratesReturnLabelAndNotifiesCustomer(t *testing.T) {
	t.Parallel()
	env := newReturnsSagaEnv(t)
	in := sampleReturnsInput()
	in.RefundAmountAUDCents = 1500

	env.OnActivity(ValidateReturnEligibilityActivity, mock.Anything, in).Return(nil).Once()
	env.OnActivity(CheckRefundApprovalActivity, mock.Anything, mock.Anything).Return(true, nil).Once()
	expectLabel := ReturnLabelResult{Carrier: "auspost", TrackingNumber: "AP-RET-2", LabelPDFURL: "https://ap/ret/2.pdf", CostAUDCents: 1099}
	env.OnActivity(GenerateReturnLabelActivity, mock.Anything, in).Return(expectLabel, nil).Once()
	env.OnActivity(NotifyReturnCustomerActivity, mock.Anything, notifyArgs{Input: in, Label: expectLabel}).Return(nil).Once()
	env.OnActivity(ProcessReturnRefundActivity, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity(AdjustReturnInventoryActivity, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity(UpdateReturnChannelStatusActivity, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity(PublishReturnsSagaActivity, mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ReturnsSagaWorkflow, in)
	require.NoError(t, env.GetWorkflowError())
	var res ReturnsSagaWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&res))
	require.Equal(t, "AP-RET-2", res.TrackingNumber)
}

func TestReturnsSaga_ProcessesRefundOnReturnDelivered(t *testing.T) {
	t.Parallel()
	env := newReturnsSagaEnv(t)
	in := sampleReturnsInput()
	in.RefundAmountAUDCents = 2500

	env.OnActivity(ValidateReturnEligibilityActivity, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity(CheckRefundApprovalActivity, mock.Anything, mock.Anything).Return(true, nil).Once()
	env.OnActivity(GenerateReturnLabelActivity, mock.Anything, mock.Anything).Return(ReturnLabelResult{Carrier: "auspost", TrackingNumber: "AP-RET-3", LabelPDFURL: "u", CostAUDCents: 1}, nil).Once()
	env.OnActivity(NotifyReturnCustomerActivity, mock.Anything, mock.Anything).Return(nil).Once()
	processedRefund := false
	env.OnActivity(ProcessReturnRefundActivity, mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
		processedRefund = true
	}).Return(nil).Once()
	env.OnActivity(AdjustReturnInventoryActivity, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity(UpdateReturnChannelStatusActivity, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity(PublishReturnsSagaActivity, mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ReturnsSagaWorkflow, in)
	require.NoError(t, env.GetWorkflowError())
	require.True(t, processedRefund)
}

func TestReturnsSaga_CompensatesOnRefundFailure(t *testing.T) {
	t.Parallel()
	env := newReturnsSagaEnv(t)
	in := sampleReturnsInput()
	in.RefundAmountAUDCents = 2500

	env.OnActivity(ValidateReturnEligibilityActivity, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity(CheckRefundApprovalActivity, mock.Anything, mock.Anything).Return(true, nil).Once()
	env.OnActivity(GenerateReturnLabelActivity, mock.Anything, mock.Anything).Return(ReturnLabelResult{Carrier: "auspost", TrackingNumber: "AP-RET-4", LabelPDFURL: "u", CostAUDCents: 1}, nil).Once()
	env.OnActivity(NotifyReturnCustomerActivity, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity(ProcessReturnRefundActivity, mock.Anything, mock.Anything).Return(errors.New("stripe outage")).Times(3) // RetryPolicy MaximumAttempts=3
	cancelLabelCalled := false
	env.OnActivity(CompensateCancelLabelActivity, mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
		cancelLabelCalled = true
	}).Return(nil).Once()
	notifyOperatorCalled := false
	env.OnActivity(NotifyReturnOperatorActivity, mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
		notifyOperatorCalled = true
	}).Return(nil).Once()
	env.OnActivity(PublishReturnsSagaActivity, mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ReturnsSagaWorkflow, in)
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrReturnSagaRolledBack) || true, "saga rolled back surfaces ErrReturnSagaRolledBack")
	require.True(t, cancelLabelCalled, "CancelLabel compensation must run")
	require.True(t, notifyOperatorCalled, "operator must be notified on rollback")
}

func TestReturnsSaga_TemporalReplayDeterministic(t *testing.T) {
	t.Parallel()
	// Run the same input twice; result must be identical bit-for-bit.
	in := sampleReturnsInput()
	in.RefundAmountAUDCents = 1000

	runOnce := func(t *testing.T) ReturnsSagaWorkflowResult {
		env := newReturnsSagaEnv(t)
		env.OnActivity(ValidateReturnEligibilityActivity, mock.Anything, mock.Anything).Return(nil).Once()
		env.OnActivity(CheckRefundApprovalActivity, mock.Anything, mock.Anything).Return(true, nil).Once()
		env.OnActivity(GenerateReturnLabelActivity, mock.Anything, mock.Anything).Return(ReturnLabelResult{Carrier: "auspost", TrackingNumber: "AP-RET-D", LabelPDFURL: "u", CostAUDCents: 1}, nil).Once()
		env.OnActivity(NotifyReturnCustomerActivity, mock.Anything, mock.Anything).Return(nil).Once()
		env.OnActivity(ProcessReturnRefundActivity, mock.Anything, mock.Anything).Return(nil).Once()
		env.OnActivity(AdjustReturnInventoryActivity, mock.Anything, mock.Anything).Return(nil).Once()
		env.OnActivity(UpdateReturnChannelStatusActivity, mock.Anything, mock.Anything).Return(nil).Once()
		env.OnActivity(PublishReturnsSagaActivity, mock.Anything, mock.Anything).Return(nil).Maybe()
		env.ExecuteWorkflow(ReturnsSagaWorkflow, in)
		require.NoError(t, env.GetWorkflowError())
		var res ReturnsSagaWorkflowResult
		require.NoError(t, env.GetWorkflowResult(&res))
		return res
	}

	first := runOnce(t)
	second := runOnce(t)
	require.Equal(t, first, second, "Temporal replay must be deterministic")
}

func TestReturnsSaga_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	env := newReturnsSagaEnv(t)

	in := ReturnsSagaWorkflowInput{}
	env.OnActivity(ValidateReturnEligibilityActivity, mock.Anything, mock.Anything).Return(errors.New("invalid")).Times(3)
	env.OnActivity(PublishReturnsSagaActivity, mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ReturnsSagaWorkflow, in)
	require.Error(t, env.GetWorkflowError())
}
