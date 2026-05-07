package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/webhook/outbound"
)

func TestWebhookRegistrationAPI(t *testing.T) {
	t.Parallel()

	srv := testWebhookServer(t)
	body := `{"url":"https://hooks.example.test/product","event_types":["product.created","order.placed"],"secret":"super-secret-webhook-key","secret_ref":"local:test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "super-secret-webhook-key") {
		t.Fatalf("create response leaked secret: %s", rec.Body.String())
	}
	var created struct {
		ID         string   `json:"id"`
		URL        string   `json:"url"`
		EventTypes []string `json:"event_types"`
		SecretRef  string   `json:"secret_ref"`
		SecretHash string   `json:"secret_hash"`
		Enabled    bool     `json:"enabled"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == "" || created.URL != "https://hooks.example.test/product" || !created.Enabled {
		t.Fatalf("created = %+v", created)
	}
	if created.SecretRef != "local:test" || created.SecretHash == "" {
		t.Fatalf("secret metadata = %+v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Webhooks []struct {
			ID string `json:"id"`
		} `json:"webhooks"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Webhooks) != 1 || listed.Webhooks[0].ID != created.ID {
		t.Fatalf("listed = %+v", listed)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/"+created.ID, nil)
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebhookRegistrationAPIValidatesInput(t *testing.T) {
	t.Parallel()

	srv := testWebhookServer(t)
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid URL", body: `{"url":"ftp://example.test/hook","event_types":["product.created"],"secret":"secret"}`},
		{name: "missing event types", body: `{"url":"https://hooks.example.test","secret":"secret"}`},
		{name: "missing secret", body: `{"url":"https://hooks.example.test","event_types":["product.created"]}`},
		{name: "unknown event", body: `{"url":"https://hooks.example.test","event_types":["unknown.event"],"secret":"secret"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()
			srv.mux().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWebhookTestEndpointDeliversSignedEvent(t *testing.T) {
	t.Parallel()

	received := make(chan http.Header, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	srv := testWebhookServer(t)
	createBody := `{"url":"` + receiver.URL + `","event_types":["product.created"],"secret":"whsec_test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewBufferString(createBody))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/"+created.ID+"/test", bytes.NewBufferString(`{"event_type":"product.created"}`))
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("test status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}

	select {
	case headers := <-received:
		if headers.Get("X-EC-Webhook-Signature") == "" || headers.Get("X-EC-Webhook-ID") != created.ID {
			t.Fatalf("headers = %+v", headers)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for test webhook delivery")
	}
}

func TestWebhookAPIRecordsAuditWithoutSecret(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	srv := testWebhookServer(t)
	srv.log = slog.New(slog.NewJSONHandler(&logs, nil))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewBufferString(`{"url":"https://hooks.example.test","event_types":["product.created"],"secret":"super-secret-webhook-key"}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(logs.String(), "super-secret-webhook-key") {
		t.Fatalf("logs leaked secret: %s", logs.String())
	}
}

func testWebhookServer(t *testing.T) *server {
	t.Helper()
	store := outbound.NewInMemoryStore()
	return &server{
		cfg:            serverConfig{allowedOrigin: "", apiToken: ""},
		repo:           inmemory.NewProductRepository(),
		orderRepo:      inmemory.NewOrderRepository(),
		cartRepo:       inmemory.NewCartRepository(),
		eventBus:       eventbus.NewInMemoryBus(),
		webhookService: outbound.NewService(outbound.ServiceConfig{Store: store, Client: outbound.NewClient(outbound.ClientConfig{MaxAttempts: 1})}),
		log:            slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
}
