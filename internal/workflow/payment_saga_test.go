package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func registerPaymentSagaActivities(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(_ context.Context, _ PaymentSagaInput) (ProviderSelection, error) {
		return ProviderSelection{Provider: "stripe"}, nil
	}, activity.RegisterOptions{Name: SelectPaymentProviderActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ paymentChargeInput) (ChargeResult, error) {
		return ChargeResult{}, nil
	}, activity.RegisterOptions{Name: ChargePaymentActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ paymentRefundInput) error {
		return nil
	}, activity.RegisterOptions{Name: RefundPaymentActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ orderStatusUpdate) error {
		return nil
	}, activity.RegisterOptions{Name: UpdateOrderStatusActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ paymentPublishArgs) error {
		return nil
	}, activity.RegisterOptions{Name: PublishPaymentEventActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, _ paymentOperatorAlert) error {
		return nil
	}, activity.RegisterOptions{Name: NotifyPaymentOperatorActivity})
}

func newPaymentEnv(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerPaymentSagaActivities(env)
	return env
}

func basePaymentInput() PaymentSagaInput {
	return PaymentSagaInput{
		TenantID: "tenant-a", OrderID: "order-1",
		AmountCents: 2500, Currency: "AUD", Method: "card",
		MaxRetries: 3,
	}
}

func TestPaymentSaga_HappyPathStripe(t *testing.T) {
	t.Parallel()
	env := newPaymentEnv(t)
	input := basePaymentInput()

	env.OnActivity(SelectPaymentProviderActivity, mock.Anything, mock.Anything).
		Return(ProviderSelection{Provider: "stripe"}, nil)
	env.OnActivity(ChargePaymentActivity, mock.Anything, mock.Anything).
		Return(ChargeResult{PaymentID: "pi_123", ExternalRef: "pi_123", Status: "succeeded", Provider: "stripe"}, nil)
	env.OnActivity(UpdateOrderStatusActivity, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(PublishPaymentEventActivity, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(PaymentSagaWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result PaymentSagaResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, "completed", result.Status)
	assert.Equal(t, "stripe", result.Provider)
	assert.Equal(t, "pi_123", result.PaymentID)
}

func TestPaymentSaga_HappyPathAlipay(t *testing.T) {
	t.Parallel()
	env := newPaymentEnv(t)
	input := basePaymentInput()
	input.Method = "alipay"

	env.OnActivity(SelectPaymentProviderActivity, mock.Anything, mock.Anything).
		Return(ProviderSelection{Provider: "alipay"}, nil)
	env.OnActivity(ChargePaymentActivity, mock.Anything, mock.Anything).
		Return(ChargeResult{PaymentID: "alipay_123", Status: "pending", Provider: "alipay"}, nil)
	env.OnActivity(UpdateOrderStatusActivity, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(PublishPaymentEventActivity, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(PaymentSagaWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result PaymentSagaResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, "completed", result.Status)
	assert.Equal(t, "alipay", result.Provider)
}

func TestPaymentSaga_DeclineWithRetry(t *testing.T) {
	t.Parallel()
	env := newPaymentEnv(t)
	input := basePaymentInput()
	input.MaxRetries = 3

	env.OnActivity(SelectPaymentProviderActivity, mock.Anything, mock.Anything).
		Return(ProviderSelection{Provider: "stripe"}, nil)
	callCount := 0
	env.OnActivity(ChargePaymentActivity, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ paymentChargeInput) (ChargeResult, error) {
			callCount++
			if callCount < 3 {
				return ChargeResult{}, port.ErrPaymentDeclined
			}
			return ChargeResult{PaymentID: "pi_retry", Status: "succeeded", Provider: "stripe"}, nil
		})
	env.OnActivity(UpdateOrderStatusActivity, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(PublishPaymentEventActivity, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(PaymentSagaWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result PaymentSagaResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, "completed", result.Status)
}

func TestPaymentSaga_PersistentFailure(t *testing.T) {
	t.Parallel()
	env := newPaymentEnv(t)
	input := basePaymentInput()
	input.MaxRetries = 2

	env.OnActivity(SelectPaymentProviderActivity, mock.Anything, mock.Anything).
		Return(ProviderSelection{Provider: "stripe"}, nil)
	env.OnActivity(ChargePaymentActivity, mock.Anything, mock.Anything).
		Return(ChargeResult{}, port.ErrPaymentDeclined)
	env.OnActivity(NotifyPaymentOperatorActivity, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(PublishPaymentEventActivity, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(PaymentSagaWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	wfErr := env.GetWorkflowError()
	require.Error(t, wfErr)
	assert.Contains(t, wfErr.Error(), "retry exhausted")
}

func TestPaymentSaga_RefundOnReturns(t *testing.T) {
	t.Parallel()
	deps := PaymentSagaActivityDeps{
		Providers: map[string]PaymentGatewayPort{
			"stripe": &fakeGateway{},
		},
		Publisher: &fakePaymentPublisher{},
		Now:       func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	}
	activities := NewPaymentSagaActivities(deps)
	err := activities.RefundPayment(context.Background(), paymentRefundInput{
		Input:     PaymentSagaInput{TenantID: "t1", OrderID: "o1", AmountCents: 1000, Currency: "AUD"},
		PaymentID: "pi_123", Provider: "stripe",
	})
	require.NoError(t, err)
}

func TestPaymentSaga_SagaRollback(t *testing.T) {
	t.Parallel()
	env := newPaymentEnv(t)
	input := basePaymentInput()

	env.OnActivity(SelectPaymentProviderActivity, mock.Anything, mock.Anything).
		Return(ProviderSelection{Provider: "stripe"}, nil)
	env.OnActivity(ChargePaymentActivity, mock.Anything, mock.Anything).
		Return(ChargeResult{PaymentID: "pi_saga", Status: "succeeded", Provider: "stripe"}, nil)
	env.OnActivity(UpdateOrderStatusActivity, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(PublishPaymentEventActivity, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(PaymentSagaWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())

	var result PaymentSagaResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, "completed", result.Status)
}

type fakeGateway struct{}

func (f *fakeGateway) Charge(_ context.Context, _, _ string, _ port.Money, _ port.PaymentMethod) (port.PaymentResult, error) {
	return port.PaymentResult{PaymentID: "pi_fake", Status: port.PaymentStatusSucceeded, Provider: "stripe"}, nil
}

func (f *fakeGateway) Refund(_ context.Context, _, _ string, _ port.Money) (port.RefundResult, error) {
	return port.RefundResult{RefundID: "re_fake", Status: "succeeded"}, nil
}

type fakePaymentPublisher struct{}

func (f *fakePaymentPublisher) Publish(_ context.Context, _ eventbus.Event) error { return nil }
func (f *fakePaymentPublisher) Close() error                                      { return nil }
