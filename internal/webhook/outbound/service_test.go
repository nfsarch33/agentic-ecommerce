package outbound

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

func TestServiceDeliversEventToMatchingRegistrationsAndRecordsResults(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-EC-Webhook-Signature") == "" {
			t.Error("missing signature")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	store := NewInMemoryStore()
	reg, err := store.CreateRegistration(context.Background(), CreateRegistrationInput{
		URL:        server.URL,
		EventTypes: []eventbus.EventType{eventbus.ProductCreated},
		Secret:     "whsec_test",
	})
	if err != nil {
		t.Fatalf("register hook: %v", err)
	}
	_, err = store.CreateRegistration(context.Background(), CreateRegistrationInput{
		URL:        server.URL + "/orders",
		EventTypes: []eventbus.EventType{eventbus.OrderPlaced},
		Secret:     "whsec_order",
	})
	if err != nil {
		t.Fatalf("register order hook: %v", err)
	}

	service := NewService(ServiceConfig{
		Store: store,
		Client: NewClient(ClientConfig{
			HTTPClient:  server.Client(),
			MaxAttempts: 1,
			Backoff:     func(int) time.Duration { return 0 },
		}),
	})
	results, err := service.DeliverEvent(context.Background(), eventbus.Event{
		ID:        "evt-product",
		Type:      eventbus.ProductCreated,
		TenantID:  "tenant-a",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("deliver event: %v", err)
	}

	if len(results) != 1 || results[0].WebhookID != reg.ID || !results[0].Success {
		t.Fatalf("results = %+v", results)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	deliveries, err := store.ListDeliveries(context.Background(), reg.ID)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].EventID != "evt-product" {
		t.Fatalf("deliveries = %+v", deliveries)
	}
}

func TestServiceRegistrationLifecycle(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{Store: NewInMemoryStore(), Client: NewClient(ClientConfig{MaxAttempts: 1})})

	reg, err := service.Register(context.Background(), CreateRegistrationInput{
		URL:        "https://hooks.example.test/product",
		EventTypes: []eventbus.EventType{eventbus.ProductCreated},
		Secret:     "whsec_test",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	all, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 || all[0].ID != reg.ID {
		t.Fatalf("registrations = %+v", all)
	}

	if err := service.Delete(context.Background(), reg.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	all, err = service.List(context.Background())
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("registrations after delete = %+v", all)
	}
	if err := service.Delete(context.Background(), reg.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing err = %v, want ErrNotFound", err)
	}
}

func TestServiceTestDeliversSubscribedEvent(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	store := NewInMemoryStore()
	service := NewService(ServiceConfig{
		Store: store,
		Client: NewClient(ClientConfig{
			HTTPClient:  server.Client(),
			MaxAttempts: 1,
			Backoff:     func(int) time.Duration { return 0 },
		}),
	})
	reg, err := service.Register(context.Background(), CreateRegistrationInput{
		URL:        server.URL,
		EventTypes: []eventbus.EventType{eventbus.ProductCreated},
		Secret:     "whsec_test",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	result, err := service.Test(context.Background(), reg.ID, eventbus.ProductCreated)
	if err != nil {
		t.Fatalf("test delivery: %v", err)
	}
	if !result.Success || result.WebhookID != reg.ID || result.EventType != eventbus.ProductCreated {
		t.Fatalf("result = %+v", result)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}

	_, err = service.Test(context.Background(), reg.ID, eventbus.OrderPlaced)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported test event err = %v, want ErrInvalidInput", err)
	}
}

func TestServiceSubscribesToEventBus(t *testing.T) {
	t.Parallel()

	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	bus := eventbus.NewInMemoryBus()
	store := NewInMemoryStore()
	_, err := store.CreateRegistration(context.Background(), CreateRegistrationInput{
		URL:        server.URL,
		EventTypes: []eventbus.EventType{eventbus.OrderPlaced},
		Secret:     "whsec_test",
	})
	if err != nil {
		t.Fatalf("register hook: %v", err)
	}
	service := NewService(ServiceConfig{
		Store: store,
		Client: NewClient(ClientConfig{
			HTTPClient:  server.Client(),
			MaxAttempts: 1,
			Backoff:     func(int) time.Duration { return 0 },
		}),
	})

	if err := service.Subscribe(context.Background(), bus, "webhook-bridge-test"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := bus.Publish(context.Background(), eventbus.Event{ID: "evt-order", Type: eventbus.OrderPlaced, Timestamp: time.Now().UTC()}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}
}
