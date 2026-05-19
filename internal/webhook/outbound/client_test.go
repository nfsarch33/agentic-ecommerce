package outbound

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

func TestDeliveryClientRetriesServerFailuresAndSignsRequest(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if got := r.Header.Get("X-EC-Webhook-Signature"); got == "" {
			t.Error("missing signature header")
		}
		if got := r.Header.Get("X-EC-Webhook-ID"); got != "webhook-1" {
			t.Errorf("webhook id header = %q, want webhook-1", got)
		}
		var payload EventPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.ID != "evt-1" || payload.Type != eventbus.ProductCreated {
			t.Errorf("payload = %+v", payload)
		}
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		HTTPClient:  server.Client(),
		MaxAttempts: 3,
		Backoff:     func(int) time.Duration { return 0 },
		Now:         func() time.Time { return time.Unix(1_779_000_000, 0).UTC() },
		SSRFGuard:   NewPermissiveSSRFGuard(),
	})
	result := client.Deliver(context.Background(), DeliveryRequest{
		Registration: Registration{ID: "webhook-1", URL: server.URL, EventTypes: []eventbus.EventType{eventbus.ProductCreated}, Enabled: true},
		Secret:       "whsec_test",
		Event: eventbus.Event{
			ID:        "evt-1",
			Type:      eventbus.ProductCreated,
			TenantID:  "tenant-a",
			Timestamp: time.Unix(1_779_000_001, 0).UTC(),
			Source:    "unit-test",
			Payload:   map[string]any{"sku": "SKU-1"},
		},
	})

	if !result.Success || result.Status != http.StatusAccepted || result.Attempts != 3 {
		t.Fatalf("result = %+v", result)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestDeliveryClientSignsExactRequestBody(t *testing.T) {
	t.Parallel()

	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		mac := hmac.New(sha256.New, []byte("whsec_test"))
		_, _ = mac.Write([]byte(r.Header.Get("X-EC-Webhook-Timestamp") + "."))
		_, _ = mac.Write(body)
		wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got := r.Header.Get("X-EC-Webhook-Signature"); got != wantSignature {
			t.Errorf("signature = %q, want %q", got, wantSignature)
		}
		received <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		HTTPClient:  server.Client(),
		MaxAttempts: 1,
		Now:         func() time.Time { return time.Date(2026, 5, 8, 3, 4, 5, 0, time.FixedZone("AEST", 10*60*60)) },
		SSRFGuard:   NewPermissiveSSRFGuard(),
	})
	result := client.Deliver(context.Background(), DeliveryRequest{
		Registration: Registration{ID: "webhook-1", URL: server.URL, EventTypes: []eventbus.EventType{eventbus.ProductCreated}, Enabled: true},
		Secret:       "whsec_test",
		Event: eventbus.Event{
			ID:        "evt-1",
			Type:      eventbus.ProductCreated,
			TenantID:  "tenant-a",
			Timestamp: time.Unix(1_779_000_001, 0).UTC(),
			Payload:   map[string]any{"sku": "SKU-1"},
		},
	})
	if !result.Success {
		t.Fatalf("result = %+v, want success", result)
	}

	select {
	case body := <-received:
		if !json.Valid(body) {
			t.Fatalf("request body is not valid JSON: %s", string(body))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signed delivery")
	}
}

func TestDeliveryClientRetriesRateLimitWithBackoff(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	backoffAttempts := []int{}
	client := NewClient(ClientConfig{
		HTTPClient:  server.Client(),
		MaxAttempts: 3,
		Backoff: func(attempt int) time.Duration {
			backoffAttempts = append(backoffAttempts, attempt)
			return 0
		},
		SSRFGuard: NewPermissiveSSRFGuard(),
	})
	result := client.Deliver(context.Background(), DeliveryRequest{
		Registration: Registration{ID: "webhook-1", URL: server.URL, EventTypes: []eventbus.EventType{eventbus.ProductCreated}, Enabled: true},
		Secret:       "whsec_test",
		Event:        eventbus.Event{ID: "evt-1", Type: eventbus.ProductCreated, Timestamp: time.Now().UTC()},
	})

	if !result.Success || result.Status != http.StatusNoContent || result.Attempts != 3 {
		t.Fatalf("result = %+v", result)
	}
	if got, want := backoffAttempts, []int{1, 2}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("backoff attempts = %v, want %v", got, want)
	}
}

func TestDeliveryClientUsesPerAttemptTimeout(t *testing.T) {
	t.Parallel()

	client := NewClient(ClientConfig{
		HTTPClient:  timeoutAwareDoer{},
		MaxAttempts: 1,
		Timeout:     time.Millisecond,
		Backoff:     func(int) time.Duration { return 0 },
		SSRFGuard:   NewPermissiveSSRFGuard(),
	})
	result := client.Deliver(context.Background(), DeliveryRequest{
		Registration: Registration{ID: "webhook-timeout", URL: "https://hooks.example.test/timeout", EventTypes: []eventbus.EventType{eventbus.ProductCreated}, Enabled: true},
		Secret:       "whsec_test",
		Event:        eventbus.Event{ID: "evt-timeout", Type: eventbus.ProductCreated, Timestamp: time.Now().UTC()},
	})

	if result.Success || result.Attempts != 1 || result.Error != "request_failed" {
		t.Fatalf("result = %+v, want sanitized timeout failure", result)
	}
}

func TestDeliveryClientDoesNotRetryClientFailures(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		HTTPClient:  server.Client(),
		MaxAttempts: 3,
		Backoff:     func(int) time.Duration { return 0 },
		SSRFGuard:   NewPermissiveSSRFGuard(),
	})
	result := client.Deliver(context.Background(), DeliveryRequest{
		Registration: Registration{ID: "webhook-1", URL: server.URL, EventTypes: []eventbus.EventType{eventbus.ProductCreated}, Enabled: true},
		Secret:       "whsec_test",
		Event: eventbus.Event{
			ID:        "evt-1",
			Type:      eventbus.ProductCreated,
			Timestamp: time.Now().UTC(),
		},
	})

	if result.Success || result.Status != http.StatusBadRequest || result.Attempts != 1 {
		t.Fatalf("result = %+v", result)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

type timeoutAwareDoer struct{}

func (timeoutAwareDoer) Do(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, errors.New("upstream timed out while waiting for secret-token-value")
}
