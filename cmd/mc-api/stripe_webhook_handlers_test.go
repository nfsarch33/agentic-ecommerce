package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
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
