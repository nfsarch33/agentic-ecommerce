package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validCommercialOrderRequest = `{
  "customer_email":"shopper@example.com",
  "idempotency_key":"checkout-dup-1",
  "delivery_option":"standard",
  "items":[
    {
      "product_id":"c1000000-0000-0000-0000-000000000001",
      "sku":"BAND-001",
      "title":"Resistance Band",
      "quantity":1,
      "unit_price":{"amount":2495,"currency":"AUD"}
    }
  ],
  "shipping_address":{
    "name":"Jane Shopper",
    "line1":"1 Market Street",
    "city":"Sydney",
    "region":"NSW",
    "postal_code":"2000",
    "country":"AU"
  }
}`

func TestCreateOrderWithSameIdempotencyKeyReturnsSameOrder(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	first := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(validCommercialOrderRequest))
	first.Header.Set("Content-Type", "application/json")
	firstRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, want 201; body=%s", firstRec.Code, firstRec.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(validCommercialOrderRequest))
	second.Header.Set("Content-Type", "application/json")
	secondRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusCreated {
		t.Fatalf("second create status = %d, want 201; body=%s", secondRec.Code, secondRec.Body.String())
	}

	var firstResp orderResponse
	if err := json.NewDecoder(firstRec.Body).Decode(&firstResp); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	var secondResp orderResponse
	if err := json.NewDecoder(secondRec.Body).Decode(&secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}

	if firstResp.ID != secondResp.ID {
		t.Fatalf("duplicate checkout created different orders: first=%s second=%s", firstResp.ID, secondResp.ID)
	}
	if !firstResp.CreatedAt.Equal(secondResp.CreatedAt) {
		t.Fatalf("duplicate checkout changed created_at: first=%s second=%s", firstResp.CreatedAt, secondResp.CreatedAt)
	}
}

func TestCreateOrderRejectsMissingDeliveryOption(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	body := strings.Replace(validCommercialOrderRequest, "\"delivery_option\":\"standard\",\n", "", 1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing delivery option status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrderRejectsUnsupportedDeliveryOption(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	body := strings.Replace(validCommercialOrderRequest, "\"delivery_option\":\"standard\"", "\"delivery_option\":\"teleport\"", 1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported delivery option status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrderRejectsIdempotencyReplayPayloadMismatch(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	first := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(validCommercialOrderRequest))
	first.Header.Set("Content-Type", "application/json")
	firstRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, want 201; body=%s", firstRec.Code, firstRec.Body.String())
	}

	mismatch := strings.Replace(validCommercialOrderRequest, "\"delivery_option\":\"standard\"", "\"delivery_option\":\"express\"", 1)
	second := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(mismatch))
	second.Header.Set("Content-Type", "application/json")
	secondRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(secondRec, second)

	if secondRec.Code != http.StatusConflict {
		t.Fatalf("payload mismatch replay status = %d, want 409; body=%s", secondRec.Code, secondRec.Body.String())
	}
}
