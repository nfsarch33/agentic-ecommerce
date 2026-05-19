package refund

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrIneligible      = errors.New("order is not eligible for refund")
	ErrExceedsOriginal = errors.New("refund amount exceeds original payment")
	ErrNotFound        = errors.New("refund not found")
	ErrInvalidTransition = errors.New("invalid refund state transition")
)

type State string

const (
	StateRequested State = "requested"
	StateApproved  State = "approved"
	StateProcessed State = "processed"
	StateCompleted State = "completed"
	StateDenied    State = "denied"
)

// Order is the minimal order data needed for refund eligibility checks.
type Order struct {
	ID         string
	PaidAmount int
	PaidAt     time.Time
}

// RefundItem identifies a specific item and amount to refund.
type RefundItem struct {
	SKU    string
	Amount int
}

// Refund is the aggregate root.
type Refund struct {
	ID          string
	OrderID     string
	Items       []RefundItem
	Total       int
	Reason      string
	State       State
	StoreCredit bool
	DenialReason string
	CreatedAt   time.Time
}

// RefundPolicy determines eligibility.
type RefundPolicy struct {
	window time.Duration
}

func NewDefaultPolicy(window time.Duration) *RefundPolicy { return &RefundPolicy{window: window} }

func (p *RefundPolicy) IsEligible(order Order, requestedAt time.Time) (bool, string) {
	if requestedAt.Sub(order.PaidAt) > p.window {
		return false, "refund window expired"
	}
	return true, ""
}

// RefundProcessor manages refund lifecycle.
type RefundProcessor struct {
	policy  *RefundPolicy
	mu      sync.RWMutex
	refunds map[string]Refund
}

func NewRefundProcessor(policy *RefundPolicy) *RefundProcessor {
	return &RefundProcessor{policy: policy, refunds: make(map[string]Refund)}
}

// RequestRefund creates a new refund request for an order.
func (p *RefundProcessor) RequestRefund(orderID string, order Order, items []RefundItem, reason string) (Refund, error) {
	return p.requestRefund(orderID, order, items, reason, false)
}

// RequestRefundWithCredit creates a refund request preferring store credit.
func (p *RefundProcessor) RequestRefundWithCredit(orderID string, order Order, items []RefundItem, reason string) (Refund, error) {
	return p.requestRefund(orderID, order, items, reason, true)
}

func (p *RefundProcessor) requestRefund(orderID string, order Order, items []RefundItem, reason string, storeCredit bool) (Refund, error) {
	eligible, msg := p.policy.IsEligible(order, time.Now())
	if !eligible {
		return Refund{}, errors.Join(ErrIneligible, errors.New(msg))
	}
	total := sumItems(items)
	if total > order.PaidAmount {
		return Refund{}, ErrExceedsOriginal
	}
	r := Refund{
		ID:          uuid.New().String(),
		OrderID:     orderID,
		Items:       items,
		Total:       total,
		Reason:      reason,
		State:       StateRequested,
		StoreCredit: storeCredit,
		CreatedAt:   time.Now().UTC(),
	}
	p.mu.Lock()
	p.refunds[r.ID] = r
	p.mu.Unlock()
	return r, nil
}

func (p *RefundProcessor) Approve(id string) (Refund, error) {
	return p.transition(id, StateApproved, StateRequested)
}

func (p *RefundProcessor) Process(id string) (Refund, error) {
	return p.transition(id, StateProcessed, StateApproved)
}

func (p *RefundProcessor) Complete(id string) (Refund, error) {
	return p.transition(id, StateCompleted, StateProcessed)
}

func (p *RefundProcessor) Deny(id, reason string) (Refund, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.refunds[id]
	if !ok {
		return Refund{}, ErrNotFound
	}
	if r.State != StateRequested {
		return Refund{}, ErrInvalidTransition
	}
	r.State = StateDenied
	r.DenialReason = reason
	p.refunds[id] = r
	return r, nil
}

func (p *RefundProcessor) transition(id string, to State, allowedFrom State) (Refund, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.refunds[id]
	if !ok {
		return Refund{}, ErrNotFound
	}
	if r.State != allowedFrom {
		return Refund{}, ErrInvalidTransition
	}
	r.State = to
	p.refunds[id] = r
	return r, nil
}

func sumItems(items []RefundItem) int {
	total := 0
	for _, it := range items {
		total += it.Amount
	}
	return total
}
