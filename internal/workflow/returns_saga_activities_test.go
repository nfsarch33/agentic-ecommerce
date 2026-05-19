// File scope: v3.8.1 carry-forward closure -- direct unit tests
// for the v3.8.0 EC-7-5 ReturnsSagaActivities adapter.
//
// The v3.8.0 sprint shipped the workflow body + the activity stubs
// but the activity wrappers (ValidateReturnEligibility,
// GenerateReturnLabel, etc.) were exercised only indirectly via
// the workflow tests. v3.8.1 closes the coverage gap with focused
// per-activity tests + the typed sentinel + dep-not-wired
// branches.
package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/stretchr/testify/require"
)

type stubEligibility struct{ err error }

func (s *stubEligibility) CheckEligibility(_ context.Context, _ ReturnsSagaWorkflowInput) error {
	return s.err
}

type stubLabelGen struct {
	res        ReturnLabelResult
	err        error
	cancelErr  error
	cancelCall int
}

func (s *stubLabelGen) GenerateReturnLabel(_ context.Context, _ ReturnsSagaWorkflowInput) (ReturnLabelResult, error) {
	if s.err != nil {
		return ReturnLabelResult{}, s.err
	}
	return s.res, nil
}

func (s *stubLabelGen) CancelLabel(_ context.Context, _ ReturnsSagaWorkflowInput) error {
	s.cancelCall++
	return s.cancelErr
}

type stubMessaging struct{ err error }

func (s *stubMessaging) NotifyReturnLabel(_ context.Context, _ notifyArgs) error { return s.err }

type stubRefunds struct{ err error }

func (s *stubRefunds) ProcessRefund(_ context.Context, _ ReturnsSagaWorkflowInput) error {
	return s.err
}

type stubInventory struct {
	adjustErr  error
	reverseErr error
}

func (s *stubInventory) AdjustInventory(_ context.Context, _ ReturnsSagaWorkflowInput) error {
	return s.adjustErr
}

func (s *stubInventory) ReverseInventory(_ context.Context, _ ReturnsSagaWorkflowInput) error {
	return s.reverseErr
}

type stubChannelStatus struct{ err error }

func (s *stubChannelStatus) UpdateReturnedStatus(_ context.Context, _ ReturnsSagaWorkflowInput) error {
	return s.err
}

type stubOperator struct{ err error }

func (s *stubOperator) NotifyOperator(_ context.Context, _ operatorAlertArgs) error { return s.err }

type capPublisher struct {
	events []eventbus.Event
	err    error
}

func (p *capPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.events = append(p.events, evt)
	return p.err
}

func (p *capPublisher) Close() error { return nil }

func validInput() ReturnsSagaWorkflowInput {
	return ReturnsSagaWorkflowInput{
		TenantID:             "tenant-A",
		RMAID:                "rma-1",
		OrderID:              "ord-1",
		Channel:              "tiktok",
		BuyerEmail:           "x@example.test",
		Reason:               "wrong",
		RefundAmountAUDCents: 1500,
	}
}

func TestNewReturnsSagaActivities_DefaultsNow(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	require.NotNil(t, a)
	require.NotNil(t, a.deps.Now, "Now defaults to time.Now")
	require.WithinDuration(t, time.Now(), a.deps.Now(), 5*time.Second)
}

func TestActivity_ValidateReturnEligibility_PassesValidInput(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{Eligibility: &stubEligibility{}})
	require.NoError(t, a.ValidateReturnEligibility(context.Background(), validInput()))
}

func TestActivity_ValidateReturnEligibility_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	err := a.ValidateReturnEligibility(context.Background(), ReturnsSagaWorkflowInput{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrReturnNotEligible))
}

func TestActivity_ValidateReturnEligibility_NoChecker(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	require.NoError(t, a.ValidateReturnEligibility(context.Background(), validInput()))
}

func TestActivity_ValidateReturnEligibility_PropagatesCheckError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("policy reject")
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{Eligibility: &stubEligibility{err: wantErr}})
	err := a.ValidateReturnEligibility(context.Background(), validInput())
	require.ErrorIs(t, err, wantErr)
}

func TestActivity_CheckRefundApproval_AutoApprovesBelowThreshold(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	ok, err := a.CheckRefundApproval(context.Background(), refundCheckArgs{
		Input:     ReturnsSagaWorkflowInput{RefundAmountAUDCents: 1000},
		Threshold: 5000,
	})
	require.NoError(t, err)
	require.True(t, ok)
}

func TestActivity_CheckRefundApproval_DeniesAboveThreshold(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	ok, err := a.CheckRefundApproval(context.Background(), refundCheckArgs{
		Input:     ReturnsSagaWorkflowInput{RefundAmountAUDCents: 12000},
		Threshold: 5000,
	})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestActivity_GenerateReturnLabel_DepNotWired(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	_, err := a.GenerateReturnLabel(context.Background(), validInput())
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrReturnSagaRolledBack))
}

func TestActivity_GenerateReturnLabel_DelegatesToDep(t *testing.T) {
	t.Parallel()
	gen := &stubLabelGen{res: ReturnLabelResult{Carrier: "auspost", TrackingNumber: "AP-1"}}
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{LabelGen: gen})
	res, err := a.GenerateReturnLabel(context.Background(), validInput())
	require.NoError(t, err)
	require.Equal(t, "AP-1", res.TrackingNumber)
}

func TestActivity_NotifyReturnCustomer_NoOpWhenDepUnwired(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	require.NoError(t, a.NotifyReturnCustomer(context.Background(), notifyArgs{Input: validInput()}))
}

func TestActivity_NotifyReturnCustomer_DelegatesToMessaging(t *testing.T) {
	t.Parallel()
	msg := &stubMessaging{err: errors.New("messaging boom")}
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{Messaging: msg})
	err := a.NotifyReturnCustomer(context.Background(), notifyArgs{Input: validInput()})
	require.Error(t, err)
}

func TestActivity_ProcessReturnRefund_NoOpWhenDepUnwired(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	require.NoError(t, a.ProcessReturnRefund(context.Background(), validInput()))
}

func TestActivity_ProcessReturnRefund_DelegatesError(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{Refunds: &stubRefunds{err: errors.New("stripe")}})
	err := a.ProcessReturnRefund(context.Background(), validInput())
	require.Error(t, err)
}

func TestActivity_AdjustReturnInventory_NoOpAndDelegate(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	require.NoError(t, a.AdjustReturnInventory(context.Background(), validInput()))
	a2 := NewReturnsSagaActivities(ReturnsSagaActivityDeps{Inventory: &stubInventory{adjustErr: errors.New("oos")}})
	require.Error(t, a2.AdjustReturnInventory(context.Background(), validInput()))
}

func TestActivity_UpdateReturnChannelStatus_NoOpAndDelegate(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	require.NoError(t, a.UpdateReturnChannelStatus(context.Background(), validInput()))
	a2 := NewReturnsSagaActivities(ReturnsSagaActivityDeps{ChannelStatus: &stubChannelStatus{err: errors.New("tiktok api")}})
	require.Error(t, a2.UpdateReturnChannelStatus(context.Background(), validInput()))
}

func TestActivity_CompensateReverseInventory_NoOpAndDelegate(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	require.NoError(t, a.CompensateReverseInventory(context.Background(), validInput()))
	a2 := NewReturnsSagaActivities(ReturnsSagaActivityDeps{Inventory: &stubInventory{reverseErr: errors.New("rev fail")}})
	require.Error(t, a2.CompensateReverseInventory(context.Background(), validInput()))
}

func TestActivity_CompensateCancelLabel_NoOpAndDelegate(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	require.NoError(t, a.CompensateCancelLabel(context.Background(), validInput()))
	gen := &stubLabelGen{cancelErr: errors.New("cancel fail")}
	a2 := NewReturnsSagaActivities(ReturnsSagaActivityDeps{LabelGen: gen})
	require.Error(t, a2.CompensateCancelLabel(context.Background(), validInput()))
	require.Equal(t, 1, gen.cancelCall)
}

func TestActivity_NotifyReturnOperator_NoOpAndDelegate(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	require.NoError(t, a.NotifyReturnOperator(context.Background(), operatorAlertArgs{Input: validInput()}))
	a2 := NewReturnsSagaActivities(ReturnsSagaActivityDeps{OperatorAlert: &stubOperator{err: errors.New("alert fail")}})
	require.Error(t, a2.NotifyReturnOperator(context.Background(), operatorAlertArgs{Input: validInput()}))
}

func TestActivity_PublishReturnsSagaState_AllStateBranches(t *testing.T) {
	t.Parallel()
	pub := &capPublisher{}
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{Publisher: pub})
	cases := []string{"pending_approval", "rolled_back", "completed", "requested"}
	for _, state := range cases {
		err := a.PublishReturnsSagaState(context.Background(), eventbus.ReturnsSagaPayload{
			Version: eventbus.ReturnsSagaPayloadVersion, TenantID: "t", RMAID: "r", OrderID: "o", State: state,
		})
		require.NoError(t, err, "state=%s", state)
	}
	require.Len(t, pub.events, 4, "one event per state branch")
}

func TestActivity_PublishReturnsSagaState_NoOpWhenPublisherUnwired(t *testing.T) {
	t.Parallel()
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{})
	require.NoError(t, a.PublishReturnsSagaState(context.Background(), eventbus.ReturnsSagaPayload{
		Version: eventbus.ReturnsSagaPayloadVersion, TenantID: "t", RMAID: "r", OrderID: "o", State: "completed",
	}))
}

func TestActivity_PublishReturnsSagaState_PropagatesPublisherError(t *testing.T) {
	t.Parallel()
	pub := &capPublisher{err: errors.New("publisher down")}
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{Publisher: pub})
	err := a.PublishReturnsSagaState(context.Background(), eventbus.ReturnsSagaPayload{
		Version: eventbus.ReturnsSagaPayloadVersion, TenantID: "t", RMAID: "r", OrderID: "o", State: "completed",
	})
	require.Error(t, err)
}

func TestActivity_PublishReturnsSagaState_RejectsInvalidPayload(t *testing.T) {
	t.Parallel()
	pub := &capPublisher{}
	a := NewReturnsSagaActivities(ReturnsSagaActivityDeps{Publisher: pub})
	// Empty payload fails Validate inside the eventbus constructor.
	err := a.PublishReturnsSagaState(context.Background(), eventbus.ReturnsSagaPayload{})
	require.Error(t, err)
}

func TestValidateReturnsInput_BoundaryCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   ReturnsSagaWorkflowInput
	}{
		{name: "missing-tenant", in: ReturnsSagaWorkflowInput{RMAID: "r", OrderID: "o"}},
		{name: "missing-rma", in: ReturnsSagaWorkflowInput{TenantID: "t", OrderID: "o"}},
		{name: "missing-order", in: ReturnsSagaWorkflowInput{TenantID: "t", RMAID: "r"}},
		{name: "negative-refund", in: ReturnsSagaWorkflowInput{TenantID: "t", RMAID: "r", OrderID: "o", RefundAmountAUDCents: -1}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			err := validateReturnsInput(c.in)
			require.Error(t, err, c.name)
			require.True(t, errors.Is(err, ErrReturnNotEligible), c.name)
		})
	}
	require.NoError(t, validateReturnsInput(validInput()))
}
