// Package payment error consolidation. v5.3.0 extracts shared error
// types so all 4 payment adapters (Stripe, Alipay, WeChat, PayPal)
// map provider-specific errors to a common set. Callers switch on
// these via errors.Is instead of adapter-specific types.
//
// The canonical sentinel errors live in internal/port/payment.go:
//   - port.ErrPaymentDeclined
//   - port.ErrInsufficientFunds
//   - port.ErrPaymentProviderUnavailable
//   - port.ErrInvalidWebhookSignature
//   - port.ErrPaymentNotFound
//
// This file adds adapter-layer shared errors that wrap port sentinels
// with provider context, plus ErrRefundFailed which was previously
// missing from the shared set.
package payment

import (
	"errors"
	"fmt"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// Shared payment adapter errors. All 4 adapters map their
// provider-specific responses to these types.
var (
	ErrRefundFailed = errors.New("payment: refund failed")
)

// ClassifyHTTPError maps an HTTP status code + response body from
// any payment provider to the appropriate shared error sentinel.
// Used by all 4 adapters to reduce duplicated error classification.
func ClassifyHTTPError(provider string, statusCode int, body []byte) error {
	if statusCode >= 500 {
		return fmt.Errorf("%w: %s status=%d", port.ErrPaymentProviderUnavailable, provider, statusCode)
	}
	return fmt.Errorf("%w: %s status=%d body=%s", port.ErrPaymentDeclined, provider, statusCode, string(body))
}

// WrapDeclined wraps port.ErrPaymentDeclined with provider context.
func WrapDeclined(provider, detail string) error {
	return fmt.Errorf("%w: %s: %s", port.ErrPaymentDeclined, provider, detail)
}

// WrapUnavailable wraps port.ErrPaymentProviderUnavailable.
func WrapUnavailable(provider string, cause error) error {
	return fmt.Errorf("%w: %s: %v", port.ErrPaymentProviderUnavailable, provider, cause)
}

// WrapInvalidSignature wraps port.ErrInvalidWebhookSignature.
func WrapInvalidSignature(provider string, cause error) error {
	return fmt.Errorf("%w: %s: %v", port.ErrInvalidWebhookSignature, provider, cause)
}

// WrapRefundFailed wraps ErrRefundFailed with provider context.
func WrapRefundFailed(provider string, cause error) error {
	return fmt.Errorf("%w: %s: %v", ErrRefundFailed, provider, cause)
}

// IsDeclined checks if the error chain contains ErrPaymentDeclined.
func IsDeclined(err error) bool {
	return errors.Is(err, port.ErrPaymentDeclined)
}

// IsInsufficientFunds checks if the error chain contains ErrInsufficientFunds.
func IsInsufficientFunds(err error) bool {
	return errors.Is(err, port.ErrInsufficientFunds)
}

// IsProviderUnavailable checks if the error chain contains ErrPaymentProviderUnavailable.
func IsProviderUnavailable(err error) bool {
	return errors.Is(err, port.ErrPaymentProviderUnavailable)
}

// IsInvalidSignature checks if the error chain contains ErrInvalidWebhookSignature.
func IsInvalidSignature(err error) bool {
	return errors.Is(err, port.ErrInvalidWebhookSignature)
}

// IsRefundFailed checks if the error chain contains ErrRefundFailed.
func IsRefundFailed(err error) bool {
	return errors.Is(err, ErrRefundFailed)
}
