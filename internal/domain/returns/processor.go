package returns

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrNotFound         = errors.New("request not found")
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrExpiredWindow    = errors.New("return window expired")
	ErrAlreadyApproved  = errors.New("already approved")
)

type ReturnStatus string

const (
	StatusPending  ReturnStatus = "pending"
	StatusApproved ReturnStatus = "approved"
	StatusRejected ReturnStatus = "rejected"
)

type ReturnItem struct {
	SKU      string
	Quantity int
}

type ReturnRequest struct {
	ID           string
	OrderID      string
	Items        []ReturnItem
	Reason       string
	Status       ReturnStatus
	CreatedAt    time.Time
	RejectedNote string
}

type ExchangeRequest struct {
	ReturnRequest
	ReplacementItems []ReturnItem
}

type RefundInitiator interface {
	InitiateRefund(requestID, orderID string) error
}

type ReturnProcessor struct {
	mu       sync.RWMutex
	requests map[string]*ReturnRequest
	refunder RefundInitiator
	window   time.Duration
}

func NewReturnProcessor(refunder RefundInitiator, window time.Duration) *ReturnProcessor {
	return &ReturnProcessor{
		requests: make(map[string]*ReturnRequest),
		refunder: refunder,
		window:   window,
	}
}

func (rp *ReturnProcessor) CreateReturn(r ReturnRequest) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if time.Since(r.CreatedAt) > rp.window {
		return ErrExpiredWindow
	}
	r.Status = StatusPending
	rp.requests[r.ID] = &r
	return nil
}

func (rp *ReturnProcessor) CreateExchange(e ExchangeRequest) error {
	return rp.CreateReturn(e.ReturnRequest)
}

func (rp *ReturnProcessor) Approve(requestID string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	r, ok := rp.requests[requestID]
	if !ok {
		return ErrNotFound
	}
	if r.Status == StatusApproved {
		return nil // idempotent
	}
	if r.Status != StatusPending {
		return ErrInvalidTransition
	}
	r.Status = StatusApproved
	if rp.refunder != nil {
		return rp.refunder.InitiateRefund(requestID, r.OrderID)
	}
	return nil
}

func (rp *ReturnProcessor) Reject(requestID, reason string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	r, ok := rp.requests[requestID]
	if !ok {
		return ErrNotFound
	}
	if r.Status != StatusPending {
		return ErrInvalidTransition
	}
	r.Status = StatusRejected
	r.RejectedNote = reason
	return nil
}

func (rp *ReturnProcessor) Get(requestID string) (ReturnRequest, error) {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	r, ok := rp.requests[requestID]
	if !ok {
		return ReturnRequest{}, ErrNotFound
	}
	return *r, nil
}
