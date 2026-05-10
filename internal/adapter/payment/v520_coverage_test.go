package payment

import (
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/stretchr/testify/assert"
)

func TestResolvePayPalBaseURL_Sandbox(t *testing.T) {
	url := resolvePayPalBaseURL(true)
	assert.Equal(t, "https://api-m.sandbox.paypal.com", url)
}

func TestResolvePayPalBaseURL_Production(t *testing.T) {
	url := resolvePayPalBaseURL(false)
	assert.Equal(t, "https://api-m.paypal.com", url)
}

func TestMapPayPalStatus(t *testing.T) {
	tests := []struct {
		input string
		want  port.PaymentStatus
	}{
		{"COMPLETED", port.PaymentStatusSucceeded},
		{"VOIDED", port.PaymentStatusFailed},
		{"PENDING", port.PaymentStatusPending},
		{"CREATED", port.PaymentStatusPending},
		{"", port.PaymentStatusPending},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := mapPayPalStatus(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyWeChatError_5xx(t *testing.T) {
	err := classifyWeChatError(502, []byte("bad gateway"))
	assert.ErrorIs(t, err, port.ErrPaymentProviderUnavailable)
}

func TestClassifyWeChatError_4xx(t *testing.T) {
	err := classifyWeChatError(400, []byte("bad request"))
	assert.ErrorIs(t, err, port.ErrPaymentDeclined)
}

func TestParseWeChatOrderStatus(t *testing.T) {
	tests := []struct {
		name string
		body string
		want port.PaymentStatus
	}{
		{"success", `{"trade_state":"SUCCESS"}`, port.PaymentStatusSucceeded},
		{"closed", `{"trade_state":"CLOSED"}`, port.PaymentStatusFailed},
		{"payerror", `{"trade_state":"PAYERROR"}`, port.PaymentStatusFailed},
		{"revoked", `{"trade_state":"REVOKED"}`, port.PaymentStatusFailed},
		{"notpay", `{"trade_state":"NOTPAY"}`, port.PaymentStatusPending},
		{"unknown", `{"trade_state":"WEIRD"}`, port.PaymentStatusPending},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, err := parseWeChatOrderStatus([]byte(tc.body))
			assert.NoError(t, err)
			assert.Equal(t, tc.want, status)
		})
	}
}

func TestParseWeChatOrderStatus_DecodeError(t *testing.T) {
	_, err := parseWeChatOrderStatus([]byte("not json"))
	assert.Error(t, err)
}

func TestMapStripeStatus(t *testing.T) {
	tests := []struct {
		input string
		want  port.PaymentStatus
	}{
		{"succeeded", port.PaymentStatusSucceeded},
		{"canceled", port.PaymentStatusFailed},
		{"requires_payment_method", port.PaymentStatusPending},
		{"processing", port.PaymentStatusPending},
		{"unknown", port.PaymentStatusPending},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := mapStripeStatus(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMapPaymentMethod(t *testing.T) {
	tests := []struct {
		input port.PaymentMethod
		want  string
	}{
		{port.PaymentMethodAlipay, "alipay"},
		{port.PaymentMethodWeChat, "wechat_pay"},
		{port.PaymentMethodCard, "card"},
		{port.PaymentMethodPayPal, "card"},
	}
	for _, tc := range tests {
		t.Run(string(tc.input), func(t *testing.T) {
			got := mapPaymentMethod(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyStripeError(t *testing.T) {
	tests := []struct {
		code int
		want error
	}{
		{500, port.ErrPaymentProviderUnavailable},
		{402, port.ErrPaymentDeclined},
		{400, port.ErrPaymentDeclined},
	}
	for _, tc := range tests {
		err := classifyStripeError(tc.code, nil)
		assert.ErrorIs(t, err, tc.want)
	}
}
