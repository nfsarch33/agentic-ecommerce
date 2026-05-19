// Pair 1 QA v6.1.1 coverage push: direct unit tests for the
// PaymentSagaActivities receiver methods, the package-level
// rollbackCharge wrapper, the input validator, and the default-time
// hook. The pre-existing temporal-suite tests in payment_saga_test.go
// exercise the workflow body via mocked activity stubs and therefore
// never enter the receiver implementations — those code paths showed
// 0% coverage in the QA profile.
package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

// --- Selector / Orders / Publisher test doubles ------------------------------

type fixedProviderSelector struct {
	provider string
	err      error
}

func (s *fixedProviderSelector) SelectProvider(_ context.Context, _ PaymentSagaInput) (string, error) {
	return s.provider, s.err
}

type recordingOrderUpdater struct {
	called    bool
	orderID   string
	status    string
	returnErr error
}

func (r *recordingOrderUpdater) UpdateOrderStatus(_ context.Context, orderID, status string) error {
	r.called = true
	r.orderID = orderID
	r.status = status
	return r.returnErr
}

type recordingPaymentPublisher struct {
	events    []eventbus.Event
	returnErr error
}

func (r *recordingPaymentPublisher) Publish(_ context.Context, e eventbus.Event) error {
	r.events = append(r.events, e)
	return r.returnErr
}

func (r *recordingPaymentPublisher) Close() error { return nil }

type erroringGateway struct{ chargeErr, refundErr error }

func (g *erroringGateway) Charge(_ context.Context, _, _ string, _ port.Money, _ port.PaymentMethod) (port.PaymentResult, error) {
	if g.chargeErr != nil {
		return port.PaymentResult{}, g.chargeErr
	}
	return port.PaymentResult{
		PaymentID:   "pi_ok",
		ExternalRef: "ext_ok",
		Status:      port.PaymentStatusSucceeded,
		Provider:    "stripe",
	}, nil
}

func (g *erroringGateway) Refund(_ context.Context, _, _ string, _ port.Money) (port.RefundResult, error) {
	if g.refundErr != nil {
		return port.RefundResult{}, g.refundErr
	}
	return port.RefundResult{RefundID: "re_ok", Status: "succeeded"}, nil
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
}

func baseInput() PaymentSagaInput {
	return PaymentSagaInput{
		TenantID:    "tenant-x",
		OrderID:     "order-1",
		AmountCents: 1500,
		Currency:    "AUD",
		Method:      "card",
	}
}

// --- NewPaymentSagaActivities ------------------------------------------------

func TestNewPaymentSagaActivities_DefaultNowSet(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{})
	require.NotNil(t, acts)
	require.NotNil(t, acts.deps.Now)
	now := acts.deps.Now()
	assert.False(t, now.IsZero(), "default Now must produce a real time")
	assert.Equal(t, time.UTC, now.Location(), "default Now must be UTC")
}

func TestNewPaymentSagaActivities_PreservesProvidedNow(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{Now: fixedNow})
	require.NotNil(t, acts)
	assert.Equal(t, fixedNow(), acts.deps.Now())
}

// --- SelectProvider ----------------------------------------------------------

func TestActivities_SelectProvider_DefaultStripeWhenNilSelector(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{})
	sel, err := acts.SelectProvider(context.Background(), baseInput())
	require.NoError(t, err)
	assert.Equal(t, "stripe", sel.Provider)
}

func TestActivities_SelectProvider_UsesInjectedSelector(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Selector: &fixedProviderSelector{provider: "alipay"},
	})
	sel, err := acts.SelectProvider(context.Background(), baseInput())
	require.NoError(t, err)
	assert.Equal(t, "alipay", sel.Provider)
}

func TestActivities_SelectProvider_PropagatesSelectorError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("selector down")
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Selector: &fixedProviderSelector{err: wantErr},
	})
	sel, err := acts.SelectProvider(context.Background(), baseInput())
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, ProviderSelection{}, sel)
}

// --- Charge ------------------------------------------------------------------

func TestActivities_Charge_SuccessReturnsResult(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Providers: map[string]PaymentGatewayPort{"stripe": &erroringGateway{}},
	})
	res, err := acts.Charge(context.Background(), paymentChargeInput{
		Input: baseInput(), Provider: "stripe",
	})
	require.NoError(t, err)
	assert.Equal(t, "pi_ok", res.PaymentID)
	assert.Equal(t, "ext_ok", res.ExternalRef)
	assert.Equal(t, string(port.PaymentStatusSucceeded), res.Status)
	assert.Equal(t, "stripe", res.Provider)
}

func TestActivities_Charge_UnknownProviderReturnsTypedError(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Providers: map[string]PaymentGatewayPort{},
	})
	res, err := acts.Charge(context.Background(), paymentChargeInput{
		Input: baseInput(), Provider: "atlantis-pay",
	})
	require.ErrorIs(t, err, port.ErrPaymentProviderUnavailable)
	assert.Equal(t, ChargeResult{}, res)
}

func TestActivities_Charge_PropagatesGatewayError(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Providers: map[string]PaymentGatewayPort{
			"stripe": &erroringGateway{chargeErr: port.ErrPaymentDeclined},
		},
	})
	res, err := acts.Charge(context.Background(), paymentChargeInput{
		Input: baseInput(), Provider: "stripe",
	})
	require.ErrorIs(t, err, port.ErrPaymentDeclined)
	assert.Equal(t, ChargeResult{}, res)
}

// --- RefundPayment -----------------------------------------------------------

func TestActivities_RefundPayment_UnknownProvider(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Providers: map[string]PaymentGatewayPort{},
	})
	err := acts.RefundPayment(context.Background(), paymentRefundInput{
		Input: baseInput(), PaymentID: "pi_x", Provider: "atlantis-pay",
	})
	require.ErrorIs(t, err, port.ErrPaymentProviderUnavailable)
}

func TestActivities_RefundPayment_PropagatesGatewayError(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Providers: map[string]PaymentGatewayPort{
			"stripe": &erroringGateway{refundErr: errors.New("provider 502")},
		},
	})
	err := acts.RefundPayment(context.Background(), paymentRefundInput{
		Input: baseInput(), PaymentID: "pi_x", Provider: "stripe",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider 502")
}

// --- UpdateOrderStatus -------------------------------------------------------

func TestActivities_UpdateOrderStatus_NoOpWhenNilPort(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{})
	err := acts.UpdateOrderStatus(context.Background(), orderStatusUpdate{
		Input:  baseInput(),
		Status: "paid",
	})
	require.NoError(t, err)
}

func TestActivities_UpdateOrderStatus_DelegatesToPort(t *testing.T) {
	t.Parallel()
	updater := &recordingOrderUpdater{}
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{Orders: updater})
	err := acts.UpdateOrderStatus(context.Background(), orderStatusUpdate{
		Input:  baseInput(),
		Status: "paid",
	})
	require.NoError(t, err)
	assert.True(t, updater.called)
	assert.Equal(t, "order-1", updater.orderID)
	assert.Equal(t, "paid", updater.status)
}

// --- PublishPaymentEvent -----------------------------------------------------

func TestActivities_PublishPaymentEvent_NoOpWhenNilPublisher(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{Now: fixedNow})
	err := acts.PublishPaymentEvent(context.Background(), paymentPublishArgs{
		Input: baseInput(),
		Result: ChargeResult{
			PaymentID: "pi_1", Provider: "stripe", Status: "succeeded",
		},
		State: "completed",
	})
	require.NoError(t, err)
}

func TestActivities_PublishPaymentEvent_CompletedEmitsTypedEvent(t *testing.T) {
	t.Parallel()
	pub := &recordingPaymentPublisher{}
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Publisher: pub,
		Now:       fixedNow,
	})
	err := acts.PublishPaymentEvent(context.Background(), paymentPublishArgs{
		Input: baseInput(),
		Result: ChargeResult{
			PaymentID: "pi_1", Provider: "stripe", Status: "succeeded",
		},
		State: "completed",
	})
	require.NoError(t, err)
	require.Len(t, pub.events, 1)
	assert.Equal(t, eventbus.PaymentCompleted, pub.events[0].Type)
	assert.Equal(t, "tenant-x", pub.events[0].TenantID)
	assert.Equal(t, fixedNow(), pub.events[0].Timestamp)
}

func TestActivities_PublishPaymentEvent_FailedEmitsTypedEvent(t *testing.T) {
	t.Parallel()
	pub := &recordingPaymentPublisher{}
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Publisher: pub,
		Now:       fixedNow,
	})
	err := acts.PublishPaymentEvent(context.Background(), paymentPublishArgs{
		Input: baseInput(),
		Result: ChargeResult{
			PaymentID: "pi_1", Provider: "stripe", Status: "failed",
		},
		State:      "failed",
		FailReason: "card declined",
	})
	require.NoError(t, err)
	require.Len(t, pub.events, 1)
	assert.Equal(t, eventbus.PaymentFailed, pub.events[0].Type)
	payload := pub.events[0].Payload
	assert.Equal(t, "card declined", payload["fail_reason"])
}

func TestActivities_PublishPaymentEvent_PayloadValidationError(t *testing.T) {
	t.Parallel()
	pub := &recordingPaymentPublisher{}
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Publisher: pub,
		Now:       fixedNow,
	})
	bad := baseInput()
	bad.TenantID = ""
	err := acts.PublishPaymentEvent(context.Background(), paymentPublishArgs{
		Input: bad,
		Result: ChargeResult{
			PaymentID: "pi_1", Provider: "stripe", Status: "succeeded",
		},
		State: "completed",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, eventbus.ErrPaymentSagaPayloadInvalid)
	assert.Empty(t, pub.events)
}

func TestActivities_PublishPaymentEvent_PublisherErrorBubbles(t *testing.T) {
	t.Parallel()
	pubErr := errors.New("bus closed")
	pub := &recordingPaymentPublisher{returnErr: pubErr}
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{
		Publisher: pub,
		Now:       fixedNow,
	})
	err := acts.PublishPaymentEvent(context.Background(), paymentPublishArgs{
		Input: baseInput(),
		Result: ChargeResult{
			PaymentID: "pi_1", Provider: "stripe", Status: "succeeded",
		},
		State: "completed",
	})
	require.ErrorIs(t, err, pubErr)
}

// --- NotifyOperator ----------------------------------------------------------

func TestActivities_NotifyOperator_IsNoOpStub(t *testing.T) {
	t.Parallel()
	acts := NewPaymentSagaActivities(PaymentSagaActivityDeps{})
	err := acts.NotifyOperator(context.Background(), paymentOperatorAlert{
		Input:  baseInput(),
		Reason: "manual review",
	})
	require.NoError(t, err)
}

// --- validatePaymentInput exhaustive branch coverage -------------------------

func TestValidatePaymentInput_RejectsEmptyTenantID(t *testing.T) {
	t.Parallel()
	in := baseInput()
	in.TenantID = "  "
	err := validatePaymentInput(in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id required")
}

func TestValidatePaymentInput_RejectsEmptyOrderID(t *testing.T) {
	t.Parallel()
	in := baseInput()
	in.OrderID = ""
	err := validatePaymentInput(in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "order_id required")
}

func TestValidatePaymentInput_RejectsNonPositiveAmount(t *testing.T) {
	t.Parallel()
	in := baseInput()
	in.AmountCents = 0
	err := validatePaymentInput(in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount must be positive")

	in.AmountCents = -1
	err = validatePaymentInput(in)
	require.Error(t, err)
}

func TestValidatePaymentInput_RejectsEmptyCurrency(t *testing.T) {
	t.Parallel()
	in := baseInput()
	in.Currency = " "
	err := validatePaymentInput(in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "currency required")
}

func TestValidatePaymentInput_AcceptsValidInput(t *testing.T) {
	t.Parallel()
	require.NoError(t, validatePaymentInput(baseInput()))
}

// --- backoffDuration coverage of the saturating loop ------------------------

func TestBackoffDuration_AttemptZeroReturnsOneSecond(t *testing.T) {
	t.Parallel()
	assert.Equal(t, time.Second, backoffDuration(0))
}

func TestBackoffDuration_DoublesPerAttempt(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 2*time.Second, backoffDuration(1))
	assert.Equal(t, 4*time.Second, backoffDuration(2))
	assert.Equal(t, 16*time.Second, backoffDuration(4))
}

func TestBackoffDuration_CappedAtThirtySeconds(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 30*time.Second, backoffDuration(6))
	assert.Equal(t, 30*time.Second, backoffDuration(20))
}

// --- RollbackCharge package-level wrapper -----------------------------------

func TestRollbackCharge_InvokesRefundActivity(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var captured paymentRefundInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, in paymentRefundInput) error {
			captured = in
			return nil
		},
		activity.RegisterOptions{Name: RefundPaymentActivity},
	)

	wf := func(ctx temporalworkflow.Context) error {
		ctx = temporalworkflow.WithActivityOptions(ctx, temporalworkflow.ActivityOptions{
			StartToCloseTimeout: time.Minute,
		})
		return RollbackCharge(ctx, baseInput(), "pi_rollback", "stripe")
	}
	env.RegisterWorkflow(wf)
	env.ExecuteWorkflow(wf)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	assert.Equal(t, "pi_rollback", captured.PaymentID)
	assert.Equal(t, "stripe", captured.Provider)
	assert.Equal(t, "tenant-x", captured.Input.TenantID)
}
