package eventbus_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaymentSagaPayloadValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload eventbus.PaymentSagaPayload
		wantErr bool
	}{
		{
			name: "valid",
			payload: eventbus.PaymentSagaPayload{
				Version: 1, TenantID: "t1", OrderID: "o1",
				Provider: "stripe", Status: "completed",
			},
		},
		{name: "missing_version", payload: eventbus.PaymentSagaPayload{TenantID: "t1", OrderID: "o1", Provider: "stripe", Status: "ok"}, wantErr: true},
		{name: "missing_tenant", payload: eventbus.PaymentSagaPayload{Version: 1, OrderID: "o1", Provider: "stripe", Status: "ok"}, wantErr: true},
		{name: "missing_order", payload: eventbus.PaymentSagaPayload{Version: 1, TenantID: "t1", Provider: "stripe", Status: "ok"}, wantErr: true},
		{name: "missing_provider", payload: eventbus.PaymentSagaPayload{Version: 1, TenantID: "t1", OrderID: "o1", Status: "ok"}, wantErr: true},
		{name: "missing_status", payload: eventbus.PaymentSagaPayload{Version: 1, TenantID: "t1", OrderID: "o1", Provider: "stripe"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.payload.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, eventbus.ErrPaymentSagaPayloadInvalid)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewPaymentCompletedEvent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	evt, err := eventbus.NewPaymentCompletedEvent("test", now, eventbus.PaymentSagaPayload{
		Version: 1, TenantID: "t1", OrderID: "o1",
		PaymentID: "p1", Provider: "stripe",
		AmountCents: 2500, Currency: "AUD", Status: "completed",
	})
	require.NoError(t, err)
	assert.Equal(t, eventbus.PaymentCompleted, evt.Type)
	assert.Equal(t, "t1", evt.TenantID)
}

func TestNewPaymentFailedEvent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	evt, err := eventbus.NewPaymentFailedEvent("test", now, eventbus.PaymentSagaPayload{
		Version: 1, TenantID: "t1", OrderID: "o1",
		Provider: "alipay", Status: "failed", FailReason: "declined",
	})
	require.NoError(t, err)
	assert.Equal(t, eventbus.PaymentFailed, evt.Type)
}
