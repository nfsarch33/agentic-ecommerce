package port

import (
	"context"
	"errors"
	"net/http"
)

// PaymentMethod identifies the payment instrument used by the buyer.
type PaymentMethod string

const (
	PaymentMethodCard   PaymentMethod = "card"
	PaymentMethodAlipay PaymentMethod = "alipay"
	PaymentMethodWeChat PaymentMethod = "wechat"
	PaymentMethodPayPal PaymentMethod = "paypal"
)

// PaymentStatus represents the lifecycle state of a payment.
type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "pending"
	PaymentStatusSucceeded PaymentStatus = "succeeded"
	PaymentStatusFailed    PaymentStatus = "failed"
	PaymentStatusRefunded  PaymentStatus = "refunded"
)

// Money represents a monetary amount in the smallest currency unit
// (e.g. cents) with its ISO 4217 currency code.
type Money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// PaymentResult is the outcome of a successful charge.
type PaymentResult struct {
	PaymentID   string        `json:"payment_id"`
	ExternalRef string        `json:"external_ref"`
	Status      PaymentStatus `json:"status"`
	Amount      Money         `json:"amount"`
	Provider    string        `json:"provider"`
}

// RefundResult is the outcome of a successful refund.
type RefundResult struct {
	RefundID    string `json:"refund_id"`
	PaymentID   string `json:"payment_id"`
	ExternalRef string `json:"external_ref"`
	Amount      Money  `json:"amount"`
	Status      string `json:"status"`
}

// WebhookEventType classifies inbound payment webhook events.
type WebhookEventType string

const (
	WebhookEventChargeSucceeded WebhookEventType = "charge.succeeded"
	WebhookEventChargeFailed    WebhookEventType = "charge.failed"
	WebhookEventRefundCompleted WebhookEventType = "refund.completed"
)

// WebhookEvent is the parsed result of a verified inbound webhook.
type WebhookEvent struct {
	EventID   string           `json:"event_id"`
	Type      WebhookEventType `json:"type"`
	PaymentID string           `json:"payment_id"`
	Provider  string           `json:"provider"`
	RawJSON   []byte           `json:"raw_json,omitempty"`
}

// Multi-provider payment gateway port. Each provider adapter
// (Stripe, Alipay, WeChat Pay) implements this interface so the
// payment saga can route via tenant config without coupling to any
// single provider SDK.
type MultiPaymentGateway interface {
	Charge(ctx context.Context, tenantID, orderID string, amount Money, method PaymentMethod) (PaymentResult, error)
	Refund(ctx context.Context, tenantID, paymentID string, amount Money) (RefundResult, error)
	VerifyWebhook(ctx context.Context, headers http.Header, body []byte) (WebhookEvent, error)
	GetStatus(ctx context.Context, tenantID, paymentID string) (PaymentStatus, error)
}

// Payment-domain typed sentinel errors.
var (
	ErrPaymentDeclined            = errors.New("payment declined")
	ErrInsufficientFunds          = errors.New("insufficient funds")
	ErrPaymentProviderUnavailable = errors.New("payment provider unavailable")
	ErrInvalidWebhookSignature    = errors.New("invalid webhook signature")
	ErrPaymentNotFound            = errors.New("payment not found")
)
