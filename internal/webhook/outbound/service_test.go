package outbound

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
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
			SSRFGuard:   NewPermissiveSSRFGuard(),
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

func TestServiceSkipsDisabledRegistrations(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	store := NewInMemoryStore()
	disabled := false
	reg, err := store.CreateRegistration(context.Background(), CreateRegistrationInput{
		URL:        server.URL,
		EventTypes: []eventbus.EventType{eventbus.ProductCreated},
		Secret:     "whsec_test",
		Enabled:    &disabled,
	})
	if err != nil {
		t.Fatalf("register disabled hook: %v", err)
	}
	service := NewService(ServiceConfig{
		Store: store,
		Client: NewClient(ClientConfig{
			HTTPClient:  server.Client(),
			MaxAttempts: 1,
			Backoff:     func(int) time.Duration { return 0 },
			SSRFGuard:   NewPermissiveSSRFGuard(),
		}),
	})

	results, err := service.DeliverEvent(context.Background(), eventbus.Event{ID: "evt-disabled", Type: eventbus.ProductCreated, Timestamp: time.Now().UTC()})
	if err != nil {
		t.Fatalf("deliver disabled event: %v", err)
	}
	if len(results) != 0 || requests != 0 {
		t.Fatalf("disabled registration delivered: results=%+v requests=%d", results, requests)
	}
	deliveries, err := store.ListDeliveries(context.Background(), reg.ID)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) != 0 {
		t.Fatalf("disabled registration recorded deliveries: %+v", deliveries)
	}
}

func TestServiceRecordsFailedDeliveriesWithoutLeakingSecrets(t *testing.T) {
	t.Parallel()

	var logs strings.Builder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	store := NewInMemoryStore()
	reg, err := store.CreateRegistration(context.Background(), CreateRegistrationInput{
		URL:        server.URL,
		EventTypes: []eventbus.EventType{eventbus.ProductCreated},
		Secret:     "super-secret-webhook-key",
	})
	if err != nil {
		t.Fatalf("register hook: %v", err)
	}
	service := NewService(ServiceConfig{
		Store: store,
		Client: NewClient(ClientConfig{
			HTTPClient:  server.Client(),
			MaxAttempts: 2,
			Backoff:     func(int) time.Duration { return 0 },
			SSRFGuard:   NewPermissiveSSRFGuard(),
		}),
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})

	results, err := service.DeliverEvent(context.Background(), eventbus.Event{ID: "evt-fail", Type: eventbus.ProductCreated, Timestamp: time.Now().UTC()})
	if err != nil {
		t.Fatalf("deliver failed event: %v", err)
	}
	if len(results) != 1 || results[0].Success || results[0].Attempts != 2 || results[0].Error != "http_500" {
		t.Fatalf("results = %+v", results)
	}
	deliveries, err := store.ListDeliveries(context.Background(), reg.ID)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].EventID != "evt-fail" {
		t.Fatalf("deliveries = %+v", deliveries)
	}
	if strings.Contains(logs.String(), "super-secret-webhook-key") || strings.Contains(results[0].Error, "super-secret-webhook-key") {
		t.Fatalf("delivery failure leaked secret: logs=%q result=%+v", logs.String(), results[0])
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

func TestServiceTenantRegistrationLifecycle(t *testing.T) {
	t.Parallel()

	received := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("X-EC-Webhook-ID")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	service := NewService(ServiceConfig{
		Store: NewInMemoryStore(),
		Client: NewClient(ClientConfig{
			HTTPClient:  server.Client(),
			MaxAttempts: 1,
			Backoff:     func(int) time.Duration { return 0 },
			SSRFGuard:   NewPermissiveSSRFGuard(),
		}),
	})
	tenantA := mustRegisterTenantWebhook(t, service, "tenant-a", server.URL)
	mustRegisterTenantWebhook(t, service, "tenant-b", server.URL)

	assertTenantWebhookList(t, service, tenantA)
	assertTenantWebhookDelivery(t, service, received, tenantA)
	assertTenantWebhookTestAndDelete(t, service, received, tenantA)
}

func assertTenantWebhookDelivery(t *testing.T, service *Service, received <-chan string, tenantA Registration) {
	t.Helper()
	results, err := service.DeliverEvent(context.Background(), eventbus.Event{
		ID:        "evt-a",
		Type:      eventbus.ProductCreated,
		TenantID:  "tenant-a",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("deliver tenant A event: %v", err)
	}
	if len(results) != 1 || results[0].WebhookID != tenantA.ID {
		t.Fatalf("tenant A delivery results = %+v", results)
	}
	if got := <-received; got != tenantA.ID {
		t.Fatalf("delivered webhook id = %q, want %q", got, tenantA.ID)
	}
}

func assertTenantWebhookTestAndDelete(t *testing.T, service *Service, received <-chan string, tenantA Registration) {
	t.Helper()
	if _, err := service.TestForTenant(context.Background(), tenantA.ID, "tenant-b", eventbus.ProductCreated); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant test err = %v, want ErrNotFound", err)
	}
	if _, err := service.TestForTenant(context.Background(), tenantA.ID, "tenant-a", eventbus.ProductCreated); err != nil {
		t.Fatalf("tenant A test delivery: %v", err)
	}
	<-received

	if err := service.DeleteForTenant(context.Background(), tenantA.ID, "tenant-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant delete err = %v, want ErrNotFound", err)
	}
	if err := service.DeleteForTenant(context.Background(), tenantA.ID, "tenant-a"); err != nil {
		t.Fatalf("delete tenant A: %v", err)
	}
}

func mustRegisterTenantWebhook(t *testing.T, service *Service, tenantID, url string) Registration {
	t.Helper()
	reg, err := service.Register(context.Background(), CreateRegistrationInput{
		TenantID:   tenantID,
		URL:        url,
		EventTypes: []eventbus.EventType{eventbus.ProductCreated},
		Secret:     "whsec_" + tenantID,
	})
	if err != nil {
		t.Fatalf("register %s: %v", tenantID, err)
	}
	return reg
}

func assertTenantWebhookList(t *testing.T, service *Service, want Registration) {
	t.Helper()
	registrations, err := service.ListForTenant(context.Background(), want.TenantID)
	if err != nil {
		t.Fatalf("list %s: %v", want.TenantID, err)
	}
	if len(registrations) != 1 || registrations[0].ID != want.ID {
		t.Fatalf("%s registrations = %+v", want.TenantID, registrations)
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
			SSRFGuard:   NewPermissiveSSRFGuard(),
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
	reg, err := store.CreateRegistration(context.Background(), CreateRegistrationInput{
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
			SSRFGuard:   NewPermissiveSSRFGuard(),
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

	deliveries, err := store.ListDeliveries(context.Background(), reg.ID)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].EventID != "evt-order" {
		t.Fatalf("deliveries = %+v, want event bridge record", deliveries)
	}
}

func TestServiceEventBridgeRetriesAtLeastOnceAndPersistsResult(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	bus := eventbus.NewInMemoryBus()
	store := NewInMemoryStore()
	reg, err := store.CreateRegistration(context.Background(), CreateRegistrationInput{
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
			MaxAttempts: 2,
			Backoff:     func(int) time.Duration { return 0 },
			SSRFGuard:   NewPermissiveSSRFGuard(),
		}),
	})

	if err := service.Subscribe(context.Background(), bus, "webhook-bridge-test"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := bus.Publish(context.Background(), eventbus.Event{ID: "evt-order", Type: eventbus.OrderPlaced, Timestamp: time.Now().UTC()}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deliveries, err := store.ListDeliveries(context.Background(), reg.ID)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if requests != 2 || len(deliveries) != 1 || !deliveries[0].Success || deliveries[0].Attempts != 2 {
		t.Fatalf("requests=%d deliveries=%+v, want one persisted delivery after retry", requests, deliveries)
	}
}

func TestServiceEventBridgeDedupesSameConsumerGroup(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	bus := eventbus.NewInMemoryBus()
	store := NewInMemoryStore()
	reg, err := store.CreateRegistration(context.Background(), CreateRegistrationInput{
		URL:        server.URL,
		EventTypes: []eventbus.EventType{eventbus.ProductUpdated},
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
			SSRFGuard:   NewPermissiveSSRFGuard(),
		}),
	})

	if err := service.Subscribe(context.Background(), bus, "webhook-bridge-test"); err != nil {
		t.Fatalf("subscribe first: %v", err)
	}
	if err := service.Subscribe(context.Background(), bus, "webhook-bridge-test"); err != nil {
		t.Fatalf("subscribe second: %v", err)
	}
	if err := bus.Publish(context.Background(), eventbus.Event{ID: "evt-product", Type: eventbus.ProductUpdated, Timestamp: time.Now().UTC()}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deliveries, err := store.ListDeliveries(context.Background(), reg.ID)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if requests != 1 || len(deliveries) != 1 {
		t.Fatalf("requests=%d deliveries=%+v, want same-group dedupe", requests, deliveries)
	}
}
