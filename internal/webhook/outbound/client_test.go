package outbound

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
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
