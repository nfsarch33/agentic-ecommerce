package billing_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/billing"
)

func TestRefundFullCycleWithVCR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("want POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/refunds" {
			t.Fatalf("want /v1/refunds, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer sk_test_key_for_vcr" {
			t.Fatalf("unexpected auth: %s", auth)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("payment_intent") != "pi_test_123" {
			t.Fatalf("unexpected payment_intent: %s", r.Form.Get("payment_intent"))
		}
		if r.Form.Get("amount") != "2500" {
			t.Fatalf("unexpected amount: %s", r.Form.Get("amount"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "re_test_abc",
			"payment_intent": "pi_test_123",
			"amount":         2500,
			"status":         "succeeded",
			"currency":       "aud",
		})
	}))
	defer srv.Close()

	refunder, err := billing.NewStripeRefunder(billing.StripeRefunderConfig{
		APIURL: srv.URL,
		APIKey: "sk_test_key_for_vcr",
	})
	if err != nil {
		t.Fatalf("new refunder: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := refunder.Refund(ctx, billing.RefundRequest{
		TenantID:        "tenant-test",
		PaymentIntentID: "pi_test_123",
		AmountCents:     2500,
		Reason:          "requested_by_customer",
		IdempotencyKey:  "idem-key-1",
	})
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if result.RefundID != "re_test_abc" {
		t.Fatalf("refund_id = %q, want re_test_abc", result.RefundID)
	}
	if result.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if result.AmountCents != 2500 {
		t.Fatalf("amount = %d, want 2500", result.AmountCents)
	}
}

func TestRefundValidationErrors(t *testing.T) {
	refunder, err := billing.NewStripeRefunder(billing.StripeRefunderConfig{
		APIURL: "http://unused",
		APIKey: "sk_test_unused",
	})
	if err != nil {
		t.Fatalf("new refunder: %v", err)
	}
	ctx := context.Background()

	tests := []struct {
		name string
		req  billing.RefundRequest
	}{
		{"missing tenant", billing.RefundRequest{PaymentIntentID: "pi_1", AmountCents: 100}},
		{"missing payment_intent", billing.RefundRequest{TenantID: "t1", AmountCents: 100}},
		{"zero amount", billing.RefundRequest{TenantID: "t1", PaymentIntentID: "pi_1", AmountCents: 0}},
		{"negative amount", billing.RefundRequest{TenantID: "t1", PaymentIntentID: "pi_1", AmountCents: -1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := refunder.Refund(ctx, tc.req)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !errors.Is(err, billing.ErrRefundFailed) {
				t.Fatalf("error = %v, want ErrRefundFailed", err)
			}
		})
	}
}

func TestRefundStripeAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid payment intent"}}`))
	}))
	defer srv.Close()

	refunder, _ := billing.NewStripeRefunder(billing.StripeRefunderConfig{
		APIURL: srv.URL,
		APIKey: "sk_test_err",
	})
	ctx := context.Background()
	_, err := refunder.Refund(ctx, billing.RefundRequest{
		TenantID:        "tenant-err",
		PaymentIntentID: "pi_bad",
		AmountCents:     100,
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !errors.Is(err, billing.ErrRefundFailed) {
		t.Fatalf("error = %v, want ErrRefundFailed", err)
	}
}

func TestRefundWebhookConfirmation(t *testing.T) {
	secret := []byte("whsec_test_secret_32bytes_long!!")
	verifier, err := billing.NewWebhookVerifier(billing.WebhookConfig{
		Secret: secret,
		Now:    func() time.Time { return time.Unix(1700000100, 0) },
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	payload := []byte(`{"type":"charge.refunded","data":{"object":{"id":"re_test","status":"succeeded"}}}`)
	timestamp := "1700000100"
	sig := billing.ExportComputeStripeSignature(secret, 1700000100, payload)
	header := "t=" + timestamp + ",v1=" + sig

	if err := verifier.Verify(header, payload); err != nil {
		t.Fatalf("webhook verify failed: %v", err)
	}
}
