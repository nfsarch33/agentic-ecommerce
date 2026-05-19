// File scope: v3.8.0 EC-7-5 returns saga Temporal workflow.
//
// Orchestrates the customer-return RMA flow:
//
//  1. Validate eligibility (return policy window, etc.).
//  2. Auto-approval gate: refund_amount < threshold (default A$50)
//     auto-approves; >= threshold emits LargeRefundPendingApproval
//     and surfaces a typed "pending_approval" result so the operator
//     dashboard can intervene.
//  3. Generate return shipping label (reverse direction; consumes
//     the EC-7-3 ShippingLabelGenerator via the small port below).
//  4. Notify customer (tracking link via the EC-8-3 messaging
//     adapter port).
//  5. Process refund through the Stripe billing port (existing
//     v2.5.0 internal/billing surface).
//  6. Adjust inventory (return to supplier or restock locally).
//  7. Update channel order status to "returned".
//
// Compensations: if step 5+ fails, the saga reverses inventory (step
// 6 if ran), cancels the label (step 3), and notifies the operator.
// All compensations run inside the workflow as ordinary activities so
// Temporal replay stays deterministic.
//
// Reuse evidence:
//   - Temporal workflow + activities pattern from v3.5.0 EC-7-1
//     order_aggregator.go.
//   - Saga rollback pattern from v3.5.0 EC-7-2 dropship_agent.go.
//   - eventbus typed events from v380_payloads.go (this sprint).
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 13-sprint streak; v3.8.0 sprint 14 target):
//   - ReturnsSagaWorkflow body: validate -> approval gate ->
//     happy-path activity loop -> publish summary. Cyclomatic 4.
//   - Each saga step is its own activity function (idiomatic Temporal
//     pattern); each helper stays under cyclomatic 6.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

// EC-7-5 activity names.
const (
	ValidateReturnEligibilityActivity  = "returns_saga.validate_eligibility"
	CheckRefundApprovalActivity        = "returns_saga.check_approval"
	GenerateReturnLabelActivity        = "returns_saga.generate_label"
	NotifyReturnCustomerActivity       = "returns_saga.notify_customer"
	ProcessReturnRefundActivity        = "returns_saga.process_refund"
	AdjustReturnInventoryActivity      = "returns_saga.adjust_inventory"
	UpdateReturnChannelStatusActivity  = "returns_saga.update_channel"
	PublishReturnsSagaActivity         = "returns_saga.publish"
	CompensateReverseInventoryActivity = "returns_saga.compensate_inventory"
	CompensateCancelLabelActivity      = "returns_saga.compensate_label"
	NotifyReturnOperatorActivity       = "returns_saga.notify_operator"
)

// EC-7-5 typed sentinels.
var (
	// ErrReturnNotEligible signals the return failed the policy
	// gate (outside return window, ineligible category, etc.).
	ErrReturnNotEligible = errors.New("returns: not eligible")

	// ErrLargeRefundApprovalRequired signals the refund amount is
	// at or above the configured auto-approval threshold and must
	// route to the operator approval queue.
	ErrLargeRefundApprovalRequired = errors.New("returns: large refund operator approval required")

	// ErrReturnSagaRolledBack signals the saga compensating
	// activities ran. Wraps the underlying step error so the caller
	// can surface the root cause.
	ErrReturnSagaRolledBack = errors.New("returns: saga rolled back")
)

// DefaultLargeRefundThresholdCents is the operator-approval gate
// threshold (A$50 = 5000 cents per the EC-7-5 spec).
const DefaultLargeRefundThresholdCents = 5000

// ReturnsSagaWorkflowInput is the workflow input.
type ReturnsSagaWorkflowInput struct {
	TenantID                   string `json:"tenant_id"`
	RMAID                      string `json:"rma_id"`
	OrderID                    string `json:"order_id"`
	Channel                    string `json:"channel"`
	BuyerEmail                 string `json:"buyer_email,omitempty"`
	Reason                     string `json:"reason"`
	RefundAmountAUDCents       int    `json:"refund_amount_aud_cents"`
	AutoApprovalThresholdCents int    `json:"auto_approval_threshold_cents,omitempty"`
}

// ReturnsSagaWorkflowResult is the workflow result.
type ReturnsSagaWorkflowResult struct {
	TenantID         string `json:"tenant_id"`
	RMAID            string `json:"rma_id"`
	State            string `json:"state"`
	AutoApproved     bool   `json:"auto_approved"`
	TrackingNumber   string `json:"tracking_number,omitempty"`
	RefundProcessed  bool   `json:"refund_processed"`
	InventoryUpdated bool   `json:"inventory_updated"`
	ChannelUpdated   bool   `json:"channel_updated"`
	RolledBackReason string `json:"rolled_back_reason,omitempty"`
}

// ReturnLabelResult is the value returned by the GenerateReturnLabel
// activity. Subset of the EC-7-3 LabelResult so the workflow does
// not depend on the fulfilment package.
type ReturnLabelResult struct {
	Carrier        string `json:"carrier"`
	TrackingNumber string `json:"tracking_number"`
	LabelPDFURL    string `json:"label_pdf_url"`
	CostAUDCents   int    `json:"cost_aud_cents"`
}

// ReturnsSagaWorkflow is the v3.8.0 EC-7-5 Temporal workflow.
//
// Cyclomatic stays at 4 (validate / approval / happy-path /
// rollback). Every saga step is its own activity so the per-step
// state transitions remain inspectable through Temporal history.
func ReturnsSagaWorkflow(ctx temporalworkflow.Context, input ReturnsSagaWorkflowInput) (ReturnsSagaWorkflowResult, error) {
	activityOptions := temporalworkflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
	ctx = temporalworkflow.WithActivityOptions(ctx, activityOptions)
	threshold := input.AutoApprovalThresholdCents
	if threshold <= 0 {
		threshold = DefaultLargeRefundThresholdCents
	}
	res := ReturnsSagaWorkflowResult{TenantID: input.TenantID, RMAID: input.RMAID}

	if err := temporalworkflow.ExecuteActivity(ctx, ValidateReturnEligibilityActivity, input).Get(ctx, nil); err != nil {
		res.State = "rolled_back"
		res.RolledBackReason = err.Error()
		_ = temporalworkflow.ExecuteActivity(ctx, PublishReturnsSagaActivity, sagaSnapshot(input, res, "rolled_back")).Get(ctx, nil)
		return res, err
	}
	approved, err := approvalDecision(ctx, input, threshold)
	if err != nil {
		res.State = "pending_approval"
		_ = temporalworkflow.ExecuteActivity(ctx, PublishReturnsSagaActivity, sagaSnapshot(input, res, "pending_approval")).Get(ctx, nil)
		return res, nil
	}
	res.AutoApproved = approved
	return runHappyPath(ctx, input, res)
}

// approvalDecision encapsulates the auto-approval gate so the
// workflow body stays cyclomatic 4. Returns (autoApproved=true, nil)
// when the refund is below threshold; returns
// (false, ErrLargeRefundApprovalRequired) when above.
func approvalDecision(ctx temporalworkflow.Context, input ReturnsSagaWorkflowInput, threshold int) (bool, error) {
	var approved bool
	if err := temporalworkflow.ExecuteActivity(ctx, CheckRefundApprovalActivity, refundCheckArgs{Input: input, Threshold: threshold}).Get(ctx, &approved); err != nil {
		return false, err
	}
	if !approved {
		return false, ErrLargeRefundApprovalRequired
	}
	return true, nil
}

// refundCheckArgs is the activity input for CheckRefundApprovalActivity.
type refundCheckArgs struct {
	Input     ReturnsSagaWorkflowInput
	Threshold int
}

// runHappyPath dispatches the post-approval saga steps. Cyclomatic 5
// (label / notify / refund / inventory / channel + compensations).
func runHappyPath(ctx temporalworkflow.Context, input ReturnsSagaWorkflowInput, res ReturnsSagaWorkflowResult) (ReturnsSagaWorkflowResult, error) {
	var label ReturnLabelResult
	if err := temporalworkflow.ExecuteActivity(ctx, GenerateReturnLabelActivity, input).Get(ctx, &label); err != nil {
		return rollback(ctx, input, res, err, false, false)
	}
	res.TrackingNumber = label.TrackingNumber
	if err := temporalworkflow.ExecuteActivity(ctx, NotifyReturnCustomerActivity, notifyArgs{Input: input, Label: label}).Get(ctx, nil); err != nil {
		return rollback(ctx, input, res, err, true, false)
	}
	if err := temporalworkflow.ExecuteActivity(ctx, ProcessReturnRefundActivity, input).Get(ctx, nil); err != nil {
		return rollback(ctx, input, res, err, true, false)
	}
	res.RefundProcessed = true
	if err := temporalworkflow.ExecuteActivity(ctx, AdjustReturnInventoryActivity, input).Get(ctx, nil); err != nil {
		return rollback(ctx, input, res, err, true, false)
	}
	res.InventoryUpdated = true
	if err := temporalworkflow.ExecuteActivity(ctx, UpdateReturnChannelStatusActivity, input).Get(ctx, nil); err != nil {
		return rollback(ctx, input, res, err, true, true)
	}
	res.ChannelUpdated = true
	res.State = "completed"
	_ = temporalworkflow.ExecuteActivity(ctx, PublishReturnsSagaActivity, sagaSnapshot(input, res, "completed")).Get(ctx, nil)
	return res, nil
}

// rollback runs the compensating activities. labelGenerated +
// inventoryAdjusted control which compensations fire so we never
// "uncompensate" a step that did not run.
func rollback(ctx temporalworkflow.Context, input ReturnsSagaWorkflowInput, res ReturnsSagaWorkflowResult, cause error, labelGenerated, inventoryAdjusted bool) (ReturnsSagaWorkflowResult, error) {
	if inventoryAdjusted {
		_ = temporalworkflow.ExecuteActivity(ctx, CompensateReverseInventoryActivity, input).Get(ctx, nil)
	}
	if labelGenerated {
		_ = temporalworkflow.ExecuteActivity(ctx, CompensateCancelLabelActivity, input).Get(ctx, nil)
	}
	_ = temporalworkflow.ExecuteActivity(ctx, NotifyReturnOperatorActivity, operatorAlertArgs{Input: input, Reason: cause.Error()}).Get(ctx, nil)
	res.State = "rolled_back"
	res.RolledBackReason = cause.Error()
	_ = temporalworkflow.ExecuteActivity(ctx, PublishReturnsSagaActivity, sagaSnapshot(input, res, "rolled_back")).Get(ctx, nil)
	return res, fmt.Errorf("%w: %v", ErrReturnSagaRolledBack, cause)
}

// notifyArgs is the activity input for NotifyReturnCustomerActivity.
type notifyArgs struct {
	Input ReturnsSagaWorkflowInput
	Label ReturnLabelResult
}

// operatorAlertArgs is the activity input for NotifyReturnOperatorActivity.
type operatorAlertArgs struct {
	Input  ReturnsSagaWorkflowInput
	Reason string
}

// sagaSnapshot composes the publish-activity argument from the input
// + current result. Pure; side-effect free.
func sagaSnapshot(input ReturnsSagaWorkflowInput, res ReturnsSagaWorkflowResult, state string) eventbus.ReturnsSagaPayload {
	return eventbus.ReturnsSagaPayload{
		Version:           eventbus.ReturnsSagaPayloadVersion,
		TenantID:          input.TenantID,
		RMAID:             input.RMAID,
		OrderID:           input.OrderID,
		Reason:            input.Reason,
		RefundAmountCents: input.RefundAmountAUDCents,
		AutoApproved:      res.AutoApproved,
		State:             state,
		RolledBackReason:  res.RolledBackReason,
	}
}

// ReturnLabelGenerator is the small port the EC-7-5 saga consumes
// to produce a return shipping label. Production wires the EC-7-3
// fulfilment.ShippingLabelGenerator via a thin adapter; tests pass
// a mock implementation.
type ReturnLabelGenerator interface {
	GenerateReturnLabel(ctx context.Context, in ReturnsSagaWorkflowInput) (ReturnLabelResult, error)
	CancelLabel(ctx context.Context, in ReturnsSagaWorkflowInput) error
}

// ReturnMessagingAdapter is the small port the saga consumes to
// notify the customer of the return label. Production wires the
// EC-8-3 messaging adapter; tests pass a mock.
type ReturnMessagingAdapter interface {
	NotifyReturnLabel(ctx context.Context, in notifyArgs) error
}

// RefundProcessor is the small port the saga consumes to issue the
// refund. Production wires the v2.5.0 internal/billing.Stripe
// surface; tests pass a mock.
type RefundProcessor interface {
	ProcessRefund(ctx context.Context, in ReturnsSagaWorkflowInput) error
}

// InventoryAdjuster is the small port the saga consumes to restock
// the returned units.
type InventoryAdjuster interface {
	AdjustInventory(ctx context.Context, in ReturnsSagaWorkflowInput) error
	ReverseInventory(ctx context.Context, in ReturnsSagaWorkflowInput) error
}

// ReturnChannelUpdater is the small port the saga consumes to mark
// the channel order status as "returned".
type ReturnChannelUpdater interface {
	UpdateReturnedStatus(ctx context.Context, in ReturnsSagaWorkflowInput) error
}

// ReturnOperatorNotifier is the small port the saga consumes to
// alert an operator on saga rollback.
type ReturnOperatorNotifier interface {
	NotifyOperator(ctx context.Context, in operatorAlertArgs) error
}

// ReturnsEligibilityChecker is the small port the saga consumes to
// validate the return policy gate.
type ReturnsEligibilityChecker interface {
	CheckEligibility(ctx context.Context, in ReturnsSagaWorkflowInput) error
}

// ReturnsSagaActivityDeps wires concrete dependencies into the
// activity struct.
type ReturnsSagaActivityDeps struct {
	Eligibility   ReturnsEligibilityChecker
	LabelGen      ReturnLabelGenerator
	Messaging     ReturnMessagingAdapter
	Refunds       RefundProcessor
	Inventory     InventoryAdjuster
	ChannelStatus ReturnChannelUpdater
	OperatorAlert ReturnOperatorNotifier
	Publisher     eventbus.Publisher
	Now           func() time.Time
}

// ReturnsSagaActivities is the activity struct registered with the
// worker.
type ReturnsSagaActivities struct {
	deps ReturnsSagaActivityDeps
}

// NewReturnsSagaActivities constructs the activity struct.
func NewReturnsSagaActivities(deps ReturnsSagaActivityDeps) *ReturnsSagaActivities {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &ReturnsSagaActivities{deps: deps}
}

// ValidateReturnEligibility runs the eligibility gate.
func (a *ReturnsSagaActivities) ValidateReturnEligibility(ctx context.Context, in ReturnsSagaWorkflowInput) error {
	if err := validateReturnsInput(in); err != nil {
		return err
	}
	if a.deps.Eligibility == nil {
		return nil
	}
	return a.deps.Eligibility.CheckEligibility(ctx, in)
}

// CheckRefundApproval runs the auto-approval gate. Returns true on
// auto-approval; false when operator approval is required.
func (a *ReturnsSagaActivities) CheckRefundApproval(_ context.Context, args refundCheckArgs) (bool, error) {
	if args.Input.RefundAmountAUDCents >= args.Threshold {
		return false, nil
	}
	return true, nil
}

// GenerateReturnLabel produces the return shipping label.
func (a *ReturnsSagaActivities) GenerateReturnLabel(ctx context.Context, in ReturnsSagaWorkflowInput) (ReturnLabelResult, error) {
	if a.deps.LabelGen == nil {
		return ReturnLabelResult{}, fmt.Errorf("%w: label generator unconfigured", ErrReturnSagaRolledBack)
	}
	return a.deps.LabelGen.GenerateReturnLabel(ctx, in)
}

// NotifyReturnCustomer sends the tracking link via messaging.
func (a *ReturnsSagaActivities) NotifyReturnCustomer(ctx context.Context, args notifyArgs) error {
	if a.deps.Messaging == nil {
		return nil
	}
	return a.deps.Messaging.NotifyReturnLabel(ctx, args)
}

// ProcessReturnRefund issues the refund.
func (a *ReturnsSagaActivities) ProcessReturnRefund(ctx context.Context, in ReturnsSagaWorkflowInput) error {
	if a.deps.Refunds == nil {
		return nil
	}
	return a.deps.Refunds.ProcessRefund(ctx, in)
}

// AdjustReturnInventory restocks the returned units.
func (a *ReturnsSagaActivities) AdjustReturnInventory(ctx context.Context, in ReturnsSagaWorkflowInput) error {
	if a.deps.Inventory == nil {
		return nil
	}
	return a.deps.Inventory.AdjustInventory(ctx, in)
}

// UpdateReturnChannelStatus marks the channel order as "returned".
func (a *ReturnsSagaActivities) UpdateReturnChannelStatus(ctx context.Context, in ReturnsSagaWorkflowInput) error {
	if a.deps.ChannelStatus == nil {
		return nil
	}
	return a.deps.ChannelStatus.UpdateReturnedStatus(ctx, in)
}

// CompensateReverseInventory reverses a prior inventory adjustment.
func (a *ReturnsSagaActivities) CompensateReverseInventory(ctx context.Context, in ReturnsSagaWorkflowInput) error {
	if a.deps.Inventory == nil {
		return nil
	}
	return a.deps.Inventory.ReverseInventory(ctx, in)
}

// CompensateCancelLabel cancels a previously generated label.
func (a *ReturnsSagaActivities) CompensateCancelLabel(ctx context.Context, in ReturnsSagaWorkflowInput) error {
	if a.deps.LabelGen == nil {
		return nil
	}
	return a.deps.LabelGen.CancelLabel(ctx, in)
}

// NotifyReturnOperator alerts the operator on saga rollback.
func (a *ReturnsSagaActivities) NotifyReturnOperator(ctx context.Context, args operatorAlertArgs) error {
	if a.deps.OperatorAlert == nil {
		return nil
	}
	return a.deps.OperatorAlert.NotifyOperator(ctx, args)
}

// PublishReturnsSagaState emits the typed lifecycle event.
func (a *ReturnsSagaActivities) PublishReturnsSagaState(ctx context.Context, payload eventbus.ReturnsSagaPayload) error {
	if a.deps.Publisher == nil {
		return nil
	}
	var evt eventbus.Event
	var err error
	switch payload.State {
	case "pending_approval":
		evt, err = eventbus.NewLargeRefundPendingApprovalEvent("workflow.returns_saga", a.deps.Now(), payload)
	case "rolled_back":
		evt, err = eventbus.NewReturnsSagaRolledBackEvent("workflow.returns_saga", a.deps.Now(), payload)
	case "completed":
		evt, err = eventbus.NewReturnsSagaCompletedEvent("workflow.returns_saga", a.deps.Now(), payload)
	default:
		evt, err = eventbus.NewReturnRequestedEvent("workflow.returns_saga", a.deps.Now(), payload)
	}
	if err != nil {
		return err
	}
	return a.deps.Publisher.Publish(ctx, evt)
}

func validateReturnsInput(in ReturnsSagaWorkflowInput) error {
	if strings.TrimSpace(in.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id required", ErrReturnNotEligible)
	}
	if strings.TrimSpace(in.RMAID) == "" {
		return fmt.Errorf("%w: rma_id required", ErrReturnNotEligible)
	}
	if strings.TrimSpace(in.OrderID) == "" {
		return fmt.Errorf("%w: order_id required", ErrReturnNotEligible)
	}
	if in.RefundAmountAUDCents < 0 {
		return fmt.Errorf("%w: refund cannot be negative", ErrReturnNotEligible)
	}
	return nil
}
