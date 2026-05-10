package payment_test

import (
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/payment"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/stretchr/testify/assert"
)

func TestClassifyHTTPError_AllProviders(t *testing.T) {
	t.Parallel()
	providers := []string{"stripe", "alipay", "wechat", "paypal"}

	for _, provider := range providers {
		t.Run(provider+"_5xx_is_unavailable", func(t *testing.T) {
			t.Parallel()
			err := payment.ClassifyHTTPError(provider, 500, []byte("internal"))
			assert.True(t, errors.Is(err, port.ErrPaymentProviderUnavailable))
		})
		t.Run(provider+"_4xx_is_declined", func(t *testing.T) {
			t.Parallel()
			err := payment.ClassifyHTTPError(provider, 400, []byte("bad"))
			assert.True(t, errors.Is(err, port.ErrPaymentDeclined))
		})
	}
}

func TestSharedErrorsIs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		errFn  func() error
		target error
	}{
		{"WrapDeclined", func() error { return payment.WrapDeclined("stripe", "card_declined") }, port.ErrPaymentDeclined},
		{"WrapUnavailable", func() error { return payment.WrapUnavailable("alipay", errors.New("timeout")) }, port.ErrPaymentProviderUnavailable},
		{"WrapInvalidSignature", func() error { return payment.WrapInvalidSignature("wechat", errors.New("bad")) }, port.ErrInvalidWebhookSignature},
		{"WrapRefundFailed", func() error { return payment.WrapRefundFailed("paypal", errors.New("declined")) }, payment.ErrRefundFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.errFn()
			assert.True(t, errors.Is(err, tc.target), "expected errors.Is(%v, %v) to be true", err, tc.target)
		})
	}
}

func TestErrorPredicates(t *testing.T) {
	t.Parallel()
	assert.True(t, payment.IsDeclined(payment.WrapDeclined("stripe", "x")))
	assert.True(t, payment.IsProviderUnavailable(payment.WrapUnavailable("alipay", nil)))
	assert.True(t, payment.IsInvalidSignature(payment.WrapInvalidSignature("wechat", nil)))
	assert.True(t, payment.IsRefundFailed(payment.WrapRefundFailed("paypal", nil)))
	assert.False(t, payment.IsDeclined(payment.WrapUnavailable("stripe", nil)))
}
