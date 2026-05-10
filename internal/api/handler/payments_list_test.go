package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inMemoryPaymentsRepo struct {
	rows []PaymentRow
}

func (r *inMemoryPaymentsRepo) List(_ context.Context, filter PaymentsFilter) ([]PaymentRow, int, error) {
	var out []PaymentRow
	for _, row := range r.rows {
		if row.TenantID != filter.TenantID {
			continue
		}
		if filter.Status != "" && row.Status != filter.Status {
			continue
		}
		if filter.Provider != "" && row.Provider != filter.Provider {
			continue
		}
		out = append(out, row)
	}
	total := len(out)
	if filter.Offset < len(out) {
		out = out[filter.Offset:]
	} else {
		out = nil
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, total, nil
}

func testPaymentsHandler(t *testing.T) *PaymentsHandler {
	t.Helper()
	h, err := NewPaymentsHandler(nil, PaymentsHandlerConfig{
		Repository: &inMemoryPaymentsRepo{
			rows: []PaymentRow{
				{PaymentID: "p1", TenantID: "t1", OrderID: "o1", Provider: "stripe", Status: "succeeded", AmountCents: 5000, Currency: "AUD", CreatedAt: time.Date(2026, 5, 10, 1, 0, 0, 0, time.UTC)},
				{PaymentID: "p2", TenantID: "t1", OrderID: "o2", Provider: "paypal", Status: "pending", AmountCents: 3000, Currency: "AUD", CreatedAt: time.Date(2026, 5, 10, 2, 0, 0, 0, time.UTC)},
				{PaymentID: "p3", TenantID: "t1", OrderID: "o3", Provider: "alipay", Status: "succeeded", AmountCents: 8000, Currency: "CNY", CreatedAt: time.Date(2026, 5, 10, 3, 0, 0, 0, time.UTC)},
				{PaymentID: "p4", TenantID: "t2", OrderID: "o4", Provider: "wechat", Status: "succeeded", AmountCents: 1000, Currency: "CNY", CreatedAt: time.Date(2026, 5, 10, 4, 0, 0, 0, time.UTC)},
			},
		},
	})
	require.NoError(t, err)
	return h
}

func TestPaymentsList_AllForTenant(t *testing.T) {
	t.Parallel()
	h := testPaymentsHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments?tenant_id=t1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Payments []PaymentRow `json:"payments"`
		Total    int          `json:"total"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, 3, body.Total)
	assert.Len(t, body.Payments, 3)
}

func TestPaymentsList_FilterByProvider(t *testing.T) {
	t.Parallel()
	h := testPaymentsHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments?tenant_id=t1&provider=stripe", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Payments []PaymentRow `json:"payments"`
		Total    int          `json:"total"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, 1, body.Total)
	assert.Equal(t, "stripe", body.Payments[0].Provider)
}

func TestPaymentsList_FilterByStatus(t *testing.T) {
	t.Parallel()
	h := testPaymentsHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments?tenant_id=t1&status=pending", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Payments []PaymentRow `json:"payments"`
		Total    int          `json:"total"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, 1, body.Total)
	assert.Equal(t, "pending", body.Payments[0].Status)
}

func TestPaymentsList_TenantIsolation(t *testing.T) {
	t.Parallel()
	h := testPaymentsHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments?tenant_id=t2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Payments []PaymentRow `json:"payments"`
		Total    int          `json:"total"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, 1, body.Total)
	assert.Equal(t, "wechat", body.Payments[0].Provider)
}

func TestPaymentsList_MissingTenant(t *testing.T) {
	t.Parallel()
	h := testPaymentsHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPaymentsList_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := testPaymentsHandler(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/payments?tenant_id=t1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestPaymentsList_TenantHeader(t *testing.T) {
	t.Parallel()
	h := testPaymentsHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, 3, body.Total)
}
