package order

import "errors"

var (
	ErrInvalidStatus           = errors.New("invalid order status")
	ErrInvalidStatusTransition = errors.New("invalid order status transition")
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusPaid      Status = "paid"
	StatusFulfilled Status = "fulfilled"
	StatusShipped   Status = "shipped"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

func ParseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusPending, StatusPaid, StatusFulfilled, StatusShipped, StatusCompleted, StatusFailed, StatusCancelled:
		return Status(s), nil
	default:
		return "", ErrInvalidStatus
	}
}

func (s Status) String() string { return string(s) }

func canTransition(from, to Status) bool {
	if from == to {
		return true
	}
	switch from {
	case StatusPending:
		return to == StatusPaid || to == StatusCancelled || to == StatusFailed
	case StatusPaid:
		return to == StatusFulfilled || to == StatusCancelled || to == StatusFailed
	case StatusFulfilled:
		return to == StatusShipped || to == StatusFailed
	case StatusShipped:
		return to == StatusCompleted
	default:
		return false
	}
}
