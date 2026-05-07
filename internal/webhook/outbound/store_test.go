package outbound

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

func TestInMemoryStoreCreatesRegistrationWithoutExposingSecret(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()

	got, err := store.CreateRegistration(context.Background(), CreateRegistrationInput{
		URL:        "https://hooks.example.test/product",
		EventTypes: []eventbus.EventType{eventbus.ProductCreated, eventbus.OrderPlaced},
		Secret:     "super-secret-webhook-key",
		SecretRef:  "local:test",
	})
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}

	if got.ID == "" {
		t.Fatal("expected generated registration ID")
	}
	if got.URL != "https://hooks.example.test/product" || !got.Enabled {
		t.Fatalf("registration = %+v", got)
	}
	if got.SecretRef != "local:test" || got.SecretHash == "" {
		t.Fatalf("secret metadata = ref %q hash %q", got.SecretRef, got.SecretHash)
	}
	if got.SecretHash == "super-secret-webhook-key" {
		t.Fatal("secret hash exposed raw secret")
	}

	secret, err := store.SecretForRegistration(context.Background(), got.ID)
	if err != nil {
		t.Fatalf("secret lookup: %v", err)
	}
	if secret != "super-secret-webhook-key" {
		t.Fatalf("secret lookup = %q, want raw secret for signer only", secret)
	}
}

func TestInMemoryStoreRejectsInvalidRegistrations(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	tests := []struct {
		name  string
		input CreateRegistrationInput
	}{
		{name: "invalid url", input: CreateRegistrationInput{URL: "ftp://example.test/hook", EventTypes: []eventbus.EventType{eventbus.ProductCreated}, Secret: "secret"}},
		{name: "missing event types", input: CreateRegistrationInput{URL: "https://hooks.example.test", Secret: "secret"}},
		{name: "unknown event type", input: CreateRegistrationInput{URL: "https://hooks.example.test", EventTypes: []eventbus.EventType{"unknown.event"}, Secret: "secret"}},
		{name: "duplicate event type", input: CreateRegistrationInput{URL: "https://hooks.example.test", EventTypes: []eventbus.EventType{eventbus.ProductCreated, eventbus.ProductCreated}, Secret: "secret"}},
		{name: "missing secret", input: CreateRegistrationInput{URL: "https://hooks.example.test", EventTypes: []eventbus.EventType{eventbus.ProductCreated}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.CreateRegistration(context.Background(), tt.input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestInMemoryStoreReturnsNotFoundForMissingRegistration(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	if _, err := store.GetRegistration(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing err = %v, want ErrNotFound", err)
	}
	if _, err := store.SecretForRegistration(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("secret missing err = %v, want ErrNotFound", err)
	}
	if err := store.DeleteRegistration(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing err = %v, want ErrNotFound", err)
	}
}

func TestInMemoryStoreListsOnlyEnabledMatchingRegistrations(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	productHook, err := store.CreateRegistration(context.Background(), CreateRegistrationInput{
		URL:        "https://hooks.example.test/product",
		EventTypes: []eventbus.EventType{eventbus.ProductCreated},
		Secret:     "secret-a",
	})
	if err != nil {
		t.Fatalf("create product hook: %v", err)
	}
	disabledHook, err := store.CreateRegistration(context.Background(), CreateRegistrationInput{
		URL:        "https://hooks.example.test/disabled",
		EventTypes: []eventbus.EventType{eventbus.ProductCreated},
		Secret:     "secret-b",
		Enabled:    boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create disabled hook: %v", err)
	}
	_, err = store.CreateRegistration(context.Background(), CreateRegistrationInput{
		URL:        "https://hooks.example.test/order",
		EventTypes: []eventbus.EventType{eventbus.OrderPlaced},
		Secret:     "secret-c",
	})
	if err != nil {
		t.Fatalf("create order hook: %v", err)
	}

	matches, err := store.EnabledRegistrationsForEvent(context.Background(), eventbus.ProductCreated)
	if err != nil {
		t.Fatalf("list matching registrations: %v", err)
	}
	if len(matches) != 1 || matches[0].ID != productHook.ID {
		t.Fatalf("matches = %+v, want only %s", matches, productHook.ID)
	}

	if err := store.DeleteRegistration(context.Background(), disabledHook.ID); err != nil {
		t.Fatalf("delete disabled hook: %v", err)
	}
	all, err := store.ListRegistrations(context.Background())
	if err != nil {
		t.Fatalf("list registrations: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("registrations after delete = %d, want 2", len(all))
	}
}

func TestInMemoryStoreRecordsDeliveryResults(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	result := DeliveryResult{
		WebhookID: "webhook-1",
		EventID:   "evt-1",
		EventType: eventbus.ProductCreated,
		Success:   true,
		Status:    204,
		Attempts:  2,
	}

	created, err := store.RecordDelivery(context.Background(), result)
	if err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatalf("recorded delivery missing ID/time: %+v", created)
	}

	deliveries, err := store.ListDeliveries(context.Background(), "webhook-1")
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].Attempts != 2 {
		t.Fatalf("deliveries = %+v", deliveries)
	}
}

func boolPtr(v bool) *bool {
	return &v
}
