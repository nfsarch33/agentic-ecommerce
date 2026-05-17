package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateOrderAppliesShippingAmountToTotals(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	body := `{
		"customer_email":"shopper@example.com",
		"delivery_option":"standard",
		"items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":2,"unit_price":{"amount":2495,"currency":"AUD"}}],
		"shipping_address":{"name":"Jane Shopper","line1":"1 Market Street","city":"Sydney","region":"NSW","postal_code":"2000","country":"AU"},
		"shipping":{"amount":500,"currency":"AUD"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp orderResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Totals.Shipping.Amount != 500 || resp.Totals.Total.Amount != 5490 {
		t.Fatalf("totals = %+v, want shipping 500 and total 5490", resp.Totals)
	}
}

func TestOrderHandlersRejectMalformedRequests(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "create invalid json", method: http.MethodPost, path: "/api/v1/orders", body: "{bad", want: http.StatusBadRequest},
		{name: "create invalid item id", method: http.MethodPost, path: "/api/v1/orders", body: `{"customer_email":"shopper@example.com","delivery_option":"standard","items":[{"product_id":"bad","sku":"BAND-001","title":"Resistance Band","quantity":1,"unit_price":{"amount":2495,"currency":"AUD"}}],"shipping_address":{"name":"Jane Shopper","line1":"1 Market Street","city":"Sydney","postal_code":"2000","country":"AU"}}`, want: http.StatusUnprocessableEntity},
		{name: "get invalid id", method: http.MethodGet, path: "/api/v1/orders/not-a-uuid", want: http.StatusBadRequest},
		{name: "get missing order", method: http.MethodGet, path: "/api/v1/orders/00000000-0000-0000-0000-000000000000", want: http.StatusNotFound},
		{name: "patch invalid json", method: http.MethodPatch, path: "/api/v1/orders/00000000-0000-0000-0000-000000000000/status", body: "{bad", want: http.StatusBadRequest},
		{name: "patch invalid status", method: http.MethodPatch, path: "/api/v1/orders/00000000-0000-0000-0000-000000000000/status", body: `{"status":"teleported"}`, want: http.StatusUnprocessableEntity},
		{name: "orders method not allowed", method: http.MethodGet, path: "/api/v1/orders", want: http.StatusMethodNotAllowed},
		{name: "cart missing session", method: http.MethodGet, path: "/api/v1/cart/", want: http.StatusBadRequest},
		{name: "cart invalid json", method: http.MethodPut, path: "/api/v1/cart/session-123", body: "{bad", want: http.StatusBadRequest},
		{name: "cart invalid item id", method: http.MethodPut, path: "/api/v1/cart/session-123", body: `{"items":[{"product_id":"bad","sku":"BAND-001","title":"Resistance Band","quantity":1,"unit_price":{"amount":2495,"currency":"AUD"}}]}`, want: http.StatusUnprocessableEntity},
		{name: "cart method not allowed", method: http.MethodPost, path: "/api/v1/cart/session-123", want: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			srv.mux().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestPatchOrderStatusReturnsNotFoundForValidMissingOrder(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/orders/00000000-0000-0000-0000-000000000000/status", bytes.NewBufferString(`{"status":"paid"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}
