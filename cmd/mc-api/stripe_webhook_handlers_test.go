package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// stripeWebhookTestSecret matches the fallback dev secret in main.go's
// buildStripeWebhookVerifier so tests run against the in-process
// verifier without env overrides. gitleaks:allow
const stripeWebhookTestSecret = "dev-only-stripe-webhook-secret-32b"

func sign(t *testing.T, secret []byte, payload []byte, ts int64) string {
	t.Helper()
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "t=" + strconv.FormatInt(ts, 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

const subscriptionCreatedPayload = `{
  "id": "evt_test_1",
  "type": "customer.subscription.created",
  "created": 1778240000,
  "data": {
    "object": {
      "id": "sub_test_1",
      "status": "active",
      "customer": "cus_test_1",
      "current_period_start": 1778240000,
      "current_period_end": 1780832000,
      "cancel_at_period_end": false,
      "metadata": {"tenant_id": "tenant-a", "plan_id": "starter"}
    }
  }
}`

func TestStripeWebhookMissingSignature(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewBufferString(subscriptionCreatedPayload)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing signature, got %d", rec.Code)
	}
}

func TestStripeWebhookInvalidSignature(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewBufferString(subscriptionCreatedPayload))
	req.Header.Set("Stripe-Signature", "t=1700000000,v1=deadbeef")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid signature, got %d", rec.Code)
	}
}

func TestStripeWebhookReplayProtection(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	old := time.Now().Add(-10 * time.Minute).Unix()
	header := sign(t, []byte(stripeWebhookTestSecret), []byte(subscriptionCreatedPayload), old)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewBufferString(subscriptionCreatedPayload))
	req.Header.Set("Stripe-Signature", header)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for replay, got %d", rec.Code)
	}
}

func TestStripeWebhookValidSignatureSubscriptionCreated(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	now := time.Now().Unix()
	header := sign(t, []byte(stripeWebhookTestSecret), []byte(subscriptionCreatedPayload), now)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewBufferString(subscriptionCreatedPayload))
	req.Header.Set("Stripe-Signature", header)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := srv.billingRepo.GetSubscriptionByStripeID(t.Context(), "tenant-a", "sub_test_1")
	if err != nil {
		t.Fatalf("post-webhook GetByStripeID: %v", err)
	}
	if got.PlanID != "starter" {
		t.Fatalf("plan_id = %s", got.PlanID)
	}
}

func TestStripeWebhookIdempotent(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	now := time.Now().Unix()
	header := sign(t, []byte(stripeWebhookTestSecret), []byte(subscriptionCreatedPayload), now)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewBufferString(subscriptionCreatedPayload))
		req.Header.Set("Stripe-Signature", header)
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d code = %d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	list, err := srv.billingRepo.ListSubscriptions(t.Context(), "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("expected 1 row after idempotent replay, got %d", list.Total)
	}
}

func TestStripeWebhookMethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/webhooks/stripe", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestStripeWebhookBodyTooLarge(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	huge := make([]byte, stripeWebhookMaxBody+10)
	now := time.Now().Unix()
	header := sign(t, []byte(stripeWebhookTestSecret), huge, now)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(huge))
	req.Header.Set("Stripe-Signature", header)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}

const invoicePaymentFailedPayload = `{
  "id": "evt_test_inv_fail_1",
  "type": "invoice.payment_failed",
  "created": 1778240000,
  "data": {
    "object": {
      "id": "in_test_fail_1",
      "subscription": "sub_test_fail_1",
      "customer": "cus_test_1",
      "amount_due": 2900,
      "amount_paid": 0,
      "currency": "usd",
      "status": "open",
      "period_start": 1778240000,
      "period_end": 1780832000,
      "metadata": {"tenant_id": "tenant-b", "plan_id": "pro"}
    }
  }
}`

const invoicePaymentSucceededPayload = `{
  "id": "evt_test_inv_ok_1",
  "type": "invoice.payment_succeeded",
  "created": 1778240000,
  "data": {
    "object": {
      "id": "in_test_ok_1",
      "subscription": "sub_test_ok_1",
      "customer": "cus_test_1",
      "amount_due": 2900,
      "amount_paid": 2900,
      "currency": "usd",
      "status": "paid",
      "period_start": 1778240000,
      "period_end": 1780832000,
      "metadata": {"tenant_id": "tenant-b", "plan_id": "pro"}
    }
  }
}`

// TestStripeWebhookInvoicePaymentFailed verifies that invoice.payment_failed
// events are accepted (200) and idempotent on replay.
func TestStripeWebhookInvoicePaymentFailed(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	now := time.Now().Unix()
	header := sign(t, []byte(stripeWebhookTestSecret), []byte(invoicePaymentFailedPayload), now)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewBufferString(invoicePaymentFailedPayload))
		req.Header.Set("Stripe-Signature", header)
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200 for invoice.payment_failed, got %d body=%s", i, rec.Code, rec.Body.String())
		}
	}

	// Invoice recorded exactly once.
	list, err := srv.billingRepo.ListInvoices(t.Context(), "tenant-b", 1, 10)
	if err != nil {
		t.Fatalf("ListInvoices: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("expected 1 invoice after idempotent replay, got %d", list.Total)
	}
}

// TestStripeWebhookInvoicePaymentSucceeded verifies that
// invoice.payment_succeeded events are accepted and idempotent.
func TestStripeWebhookInvoicePaymentSucceeded(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	now := time.Now().Unix()
	header := sign(t, []byte(stripeWebhookTestSecret), []byte(invoicePaymentSucceededPayload), now)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewBufferString(invoicePaymentSucceededPayload))
		req.Header.Set("Stripe-Signature", header)
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200 for invoice.payment_succeeded, got %d body=%s", i, rec.Code, rec.Body.String())
		}
	}

	list, err := srv.billingRepo.ListInvoices(t.Context(), "tenant-b", 1, 10)
	if err != nil {
		t.Fatalf("ListInvoices: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("expected 1 invoice after idempotent replay, got %d", list.Total)
	}
}

// TestStripeWebhookConcurrentDuplicates fires the same event from N
// goroutines simultaneously. Exactly one should be processed; the rest
// must see the dedup guard. The final state must have exactly one
// subscription row -- no duplicate inserts under concurrency.
func TestStripeWebhookConcurrentDuplicates(t *testing.T) {
	const concurrency = 10
	srv := newTestServer(t)
	now := time.Now().Unix()
	header := sign(t, []byte(stripeWebhookTestSecret), []byte(subscriptionCreatedPayload), now)

	var wg sync.WaitGroup
	codes := make([]int, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewBufferString(subscriptionCreatedPayload))
			req.Header.Set("Stripe-Signature", header)
			rec := httptest.NewRecorder()
			srv.mux().ServeHTTP(rec, req)
			codes[idx] = rec.Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("goroutine %d: got %d, want 200", i, code)
		}
	}

	list, err := srv.billingRepo.ListSubscriptions(t.Context(), "tenant-a", 1, 20)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("concurrent dedup: expected 1 subscription, got %d", list.Total)
	}
}
