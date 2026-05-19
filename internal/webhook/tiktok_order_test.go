package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/social"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

const testTikTokWebhookSecret = "tiktok-webhook-test-secret-bytes-fixture" // gitleaks:allow

type capturingMetrics struct {
	mu       sync.Mutex
	webhooks []string
	sigFails []string
	calls    atomic.Int64
}

func (m *capturingMetrics) RecordWebhook(_, _, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.webhooks = append(m.webhooks, status)
	m.calls.Add(1)
}

func (m *capturingMetrics) RecordSignatureFailure(_, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sigFails = append(m.sigFails, reason)
}

func (m *capturingMetrics) outcomes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.webhooks))
	copy(out, m.webhooks)
	return out
}

var fixedTestTime = time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

func newWebhookHarness(t *testing.T) (*TikTokOrderHandler, *eventbus.InMemoryBus, *social.TikTokWebhookVerifier, *capturingMetrics) {
	t.Helper()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	verifier, err := social.NewTikTokWebhookVerifier(social.TikTokWebhookConfig{
		Secret: []byte(testTikTokWebhookSecret),
		Now:    func() time.Time { return fixedTestTime },
	})
	if err != nil {
		t.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	metrics := &capturingMetrics{}
	handler, err := NewTikTokOrderHandler(nil, TikTokOrderHandlerConfig{
		Verifier:    verifier,
		Publisher:   bus,
		Idempotency: NewMemoryIdempotencyStore(),
		TenantID:    "tenant-1",
		Metrics:     metrics,
		Now:         func() time.Time { return fixedTestTime },
	})
	if err != nil {
		t.Fatalf("NewTikTokOrderHandler: %v", err)
	}
	t.Cleanup(func() { _ = handler.Close(context.Background()) })
	return handler, bus, verifier, metrics
}

func orderBody(t *testing.T, orderID string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"tenant_id":   "tenant-1",
		"order_id":    orderID,
		"shop_id":     "shop-1",
		"buyer_email": "buyer@example.com",
		"total_cents": 4999,
		"currency":    "AUD",
		"items": []map[string]any{
			{"sku": "SKU-1", "quantity": 1, "unit_cents": 4999, "product_id": "prod-1"},
		},
		"status":      "placed",
		"occurred_at": "2026-05-09T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return body
}

func signedRequest(t *testing.T, body []byte, verifier *social.TikTokWebhookVerifier, ts time.Time) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tiktok/orders", bytes.NewReader(body))
	req.Header.Set("X-Tts-Signature", verifier.SignWebhook(ts.Unix(), body))
	return req
}

// TestTikTokWebhook_VerifiesHMACAndEmitsOrderEvent is the EC-3-3
// RED acceptance test. Drives the handler with a signed body and
// asserts a single OrderReceivedEvent on the bus.
func TestTikTokWebhook_VerifiesHMACAndEmitsOrderEvent(t *testing.T) {
	t.Parallel()

	handler, bus, verifier, metrics := newWebhookHarness(t)
	body := orderBody(t, "order-1")
	req := signedRequest(t, body, verifier, time.Date(2026, 5, 9, 11, 59, 30, 0, time.UTC))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	delivered := bus.Delivered()
	count := 0
	for _, e := range delivered {
		if e.Type == eventbus.OrderReceived {
			count++
			if e.TenantID != "tenant-1" {
				t.Fatalf("tenant = %s", e.TenantID)
			}
			if got, _ := e.Payload["order_id"].(string); got != "order-1" {
				t.Fatalf("order_id = %v", e.Payload["order_id"])
			}
		}
	}
	if count != 1 {
		t.Fatalf("OrderReceived count = %d", count)
	}
	if got := metrics.outcomes(); len(got) != 1 || got[0] != "ok" {
		t.Fatalf("outcomes = %v", got)
	}
}

func TestTikTokWebhook_TamperedHMACRejected(t *testing.T) {
	t.Parallel()

	handler, bus, verifier, metrics := newWebhookHarness(t)
	body := orderBody(t, "order-tampered")
	req := signedRequest(t, body, verifier, time.Date(2026, 5, 9, 11, 59, 30, 0, time.UTC))
	// flip the last hex char of the signature
	header := req.Header.Get("X-Tts-Signature")
	parts := strings.SplitN(header, "s=", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected header: %s", header)
	}
	last := parts[1][len(parts[1])-1]
	flipped := byte('0')
	if last == '0' {
		flipped = '1'
	}
	tampered := parts[0] + "s=" + parts[1][:len(parts[1])-1] + string(flipped)
	req.Header.Set("X-Tts-Signature", tampered)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	delivered := bus.Delivered()
	for _, e := range delivered {
		if e.Type == eventbus.OrderReceived {
			t.Fatalf("unexpected OrderReceived event on tamper")
		}
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if len(metrics.sigFails) == 0 {
		t.Fatalf("expected signature failure metric")
	}
}

func TestTikTokWebhook_DuplicateIsNoOp(t *testing.T) {
	t.Parallel()

	handler, bus, verifier, metrics := newWebhookHarness(t)
	body := orderBody(t, "order-dup")
	for i := 0; i < 3; i++ {
		req := signedRequest(t, body, verifier, time.Date(2026, 5, 9, 11, 59, 30, 0, time.UTC))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d", i, rec.Code)
		}
	}
	count := 0
	for _, e := range bus.Delivered() {
		if e.Type == eventbus.OrderReceived {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("OrderReceived count = %d, want 1 (idempotent)", count)
	}
	got := metrics.outcomes()
	if len(got) < 3 {
		t.Fatalf("expected 3 outcomes, got %v", got)
	}
	if got[0] != "ok" {
		t.Fatalf("first = %s", got[0])
	}
	if got[1] != "duplicate" || got[2] != "duplicate" {
		t.Fatalf("subsequent = %v", got[1:])
	}
}

func TestTikTokWebhook_MissingSignature(t *testing.T) {
	t.Parallel()
	handler, _, _, _ := newWebhookHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tiktok/orders", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTikTokWebhook_ExpiredSignature(t *testing.T) {
	t.Parallel()
	handler, _, verifier, _ := newWebhookHarness(t)
	body := orderBody(t, "order-old")
	req := signedRequest(t, body, verifier, time.Date(2026, 5, 9, 11, 0, 0, 0, time.UTC)) // 1 hour old
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (poison pill)", rec.Code)
	}
}

func TestTikTokWebhook_NonPostRejected(t *testing.T) {
	t.Parallel()
	handler, _, _, _ := newWebhookHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/webhooks/tiktok/orders", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTikTokWebhook_DecodeFailure(t *testing.T) {
	t.Parallel()
	handler, _, verifier, _ := newWebhookHarness(t)
	body := []byte("not-json")
	req := signedRequest(t, body, verifier, fixedTestTime)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTikTokWebhook_PayloadValidation(t *testing.T) {
	t.Parallel()
	handler, _, verifier, _ := newWebhookHarness(t)
	cases := []map[string]any{
		{"order_id": "", "items": []map[string]any{{"sku": "S", "quantity": 1, "unit_cents": 1}}},
		{"order_id": "x", "items": []map[string]any{}},
	}
	for i, c := range cases {
		body, _ := json.Marshal(c)
		req := signedRequest(t, body, verifier, fixedTestTime)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("case %d status = %d", i, rec.Code)
		}
	}
}

func TestTikTokWebhook_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	handler, _, _, _ := newWebhookHarness(t)
	_ = handler.Close(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tiktok/orders", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestNewTikTokOrderHandler_ConfigValidation(t *testing.T) {
	t.Parallel()
	verifier, _ := social.NewTikTokWebhookVerifier(social.TikTokWebhookConfig{Secret: []byte(testTikTokWebhookSecret)})
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	store := NewMemoryIdempotencyStore()
	cases := map[string]TikTokOrderHandlerConfig{
		"missing verifier":    {Publisher: bus, Idempotency: store, TenantID: "t"},
		"missing publisher":   {Verifier: verifier, Idempotency: store, TenantID: "t"},
		"missing idempotency": {Verifier: verifier, Publisher: bus, TenantID: "t"},
		"missing tenant":      {Verifier: verifier, Publisher: bus, Idempotency: store},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewTikTokOrderHandler(nil, cfg)
			if !errors.Is(err, ErrWebhookUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestMemoryIdempotencyStore_RejectsEmptyInput(t *testing.T) {
	t.Parallel()
	store := NewMemoryIdempotencyStore()
	if _, err := store.Reserve(context.Background(), "", "k"); !errors.Is(err, ErrWebhookPayloadInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := store.Reserve(context.Background(), "t", ""); !errors.Is(err, ErrWebhookPayloadInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestMemoryIdempotencyStore_DistinctTenants(t *testing.T) {
	t.Parallel()
	store := NewMemoryIdempotencyStore()
	for _, tenant := range []string{"t1", "t2"} {
		ok, err := store.Reserve(context.Background(), tenant, "shared-key")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !ok {
			t.Fatalf("tenant %s should have first-write", tenant)
		}
	}
}

func TestSignatureFailureReason_TableDriven(t *testing.T) {
	t.Parallel()
	cases := map[error]string{
		social.ErrTikTokMissingSignature:   "missing",
		social.ErrTikTokSignatureMismatch:  "mismatch",
		social.ErrTikTokSignatureMalformed: "malformed",
		social.ErrTikTokEventTooOld:        "expired",
		errors.New("other"):                "other",
	}
	for err, want := range cases {
		got := signatureFailureReason(err)
		if got != want {
			t.Errorf("signatureFailureReason(%v) = %s, want %s", err, got, want)
		}
	}
}

func TestStatusForVerifyError_TableDriven(t *testing.T) {
	t.Parallel()
	cases := map[error]int{
		social.ErrTikTokMissingSignature:   http.StatusUnauthorized,
		social.ErrTikTokSignatureMismatch:  http.StatusUnauthorized,
		social.ErrTikTokSignatureMalformed: http.StatusUnauthorized,
		social.ErrTikTokEventTooOld:        http.StatusBadRequest,
		errors.New("other"):                http.StatusInternalServerError,
	}
	for err, want := range cases {
		got := statusForVerifyError(err)
		if got != want {
			t.Errorf("statusForVerifyError(%v) = %d, want %d", err, got, want)
		}
	}
}

func TestReadBody_LimitReader(t *testing.T) {
	t.Parallel()
	huge := bytes.Repeat([]byte("a"), MaxTikTokOrderBodyBytes+1024)
	req := httptest.NewRequest(http.MethodPost, "/x", io.NopCloser(bytes.NewReader(huge)))
	body, err := readBody(req)
	if err != nil {
		t.Fatalf("readBody: %v", err)
	}
	if len(body) != MaxTikTokOrderBodyBytes {
		t.Fatalf("len = %d, want %d", len(body), MaxTikTokOrderBodyBytes)
	}
}
