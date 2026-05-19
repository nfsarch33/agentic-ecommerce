package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	enginesync "github.com/nfsarch33/helixon-ec/internal/sync"
)

const testSecret = "wc-webhook-secret"

func TestVerifySignatureAcceptsWooCommerceBase64HMAC(t *testing.T) {
	t.Parallel()

	body := []byte(`{"id":123,"status":"processing"}`)
	signature := signBase64(body, testSecret)

	if !VerifySignature(body, signature, testSecret) {
		t.Fatal("expected valid WooCommerce signature")
	}
	if VerifySignature(body, signature, "wrong-secret") {
		t.Fatal("expected wrong secret to fail")
	}
}

func TestHandlerAcceptsOrderWebhookAndRecordsEvent(t *testing.T) {
	t.Parallel()

	recorder := &eventRecorder{}
	handler := NewHandler(Config{Secret: testSecret, Resource: ResourceOrders, Recorder: recorder})
	body := []byte(`{"id":123,"status":"processing","total":"59.99","billing":{"email":"buyer@example.com"},"line_items":[{"id":1,"name":"Band","product_id":7,"quantity":2,"total":"59.99"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/woocommerce/orders", bytes.NewReader(body))
	req.Header.Set("X-WC-Webhook-Signature", signBase64(body, testSecret))
	req.Header.Set("X-WC-Webhook-Topic", "order.updated")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	if len(recorder.events) != 1 || recorder.events[0].Type != enginesync.EventInventoryReconciled {
		t.Fatalf("events = %+v", recorder.events)
	}
}

func TestHandlerRejectsInvalidProductPayload(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{Secret: testSecret, Resource: ResourceProducts, Recorder: &eventRecorder{}})
	body := []byte(`{"id":0,"sku":"","name":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/woocommerce/products", bytes.NewReader(body))
	req.Header.Set("X-WC-Webhook-Signature", signBase64(body, testSecret))
	req.Header.Set("X-WC-Webhook-Topic", "product.updated")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", res.Code, res.Body.String())
	}
}

func TestHandlerAcceptsProductWebhookAndRecordsEvent(t *testing.T) {
	t.Parallel()

	recorder := &eventRecorder{}
	handler := NewHandler(Config{Secret: testSecret, Resource: ResourceProducts, Recorder: recorder})
	body := []byte(`{"id":7,"sku":"SKU-7","name":"Webhook product"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/woocommerce/products", bytes.NewReader(body))
	req.Header.Set("X-WC-Webhook-Signature", signBase64(body, testSecret))
	req.Header.Set("X-WC-Webhook-Topic", "product.updated")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	if len(recorder.events) != 1 || recorder.events[0].Type != enginesync.EventProductImported {
		t.Fatalf("events = %+v", recorder.events)
	}
	if recorder.events[0].Metadata["sku"] != "SKU-7" {
		t.Fatalf("metadata = %+v", recorder.events[0].Metadata)
	}
}

func TestHandlerRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{Secret: testSecret, Resource: ResourceProducts, Recorder: &eventRecorder{}})
	body := []byte(`{"id":7,"sku":"SKU-1","name":"Band"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/woocommerce/products", bytes.NewReader(body))
	req.Header.Set("X-WC-Webhook-Signature", signBase64(body, "wrong"))
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.Code)
	}
}

func TestHandlerRejectsMethodEmptyBodyLargePayloadAndBadJSON(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{Secret: testSecret, Resource: ResourceOrders, Recorder: &eventRecorder{}})

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/hook", nil))
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d, want 405", res.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(nil))
	req.Header.Set("X-WC-Webhook-Signature", signBase64(nil, testSecret))
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("empty status = %d, want 400", res.Code)
	}

	large := bytes.Repeat([]byte("a"), maxPayloadSize+1)
	req = httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(large))
	req.Header.Set("X-WC-Webhook-Signature", signBase64(large, testSecret))
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large status = %d, want 413", res.Code)
	}

	badJSON := []byte(`{"id":`)
	req = httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(badJSON))
	req.Header.Set("X-WC-Webhook-Signature", signBase64(badJSON, testSecret))
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d, want 400", res.Code)
	}
}

type eventRecorder struct {
	events []enginesync.Event
}

func (r *eventRecorder) RecordEvent(event enginesync.Event) {
	r.events = append(r.events, event)
}

func signBase64(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func decodeBody(t *testing.T, body *bytes.Buffer) map[string]string {
	t.Helper()
	var out map[string]string
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return out
}
