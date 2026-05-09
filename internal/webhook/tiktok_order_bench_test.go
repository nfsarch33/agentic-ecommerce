package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/social"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

// BenchmarkTikTokOrderHandler_ServeHTTP measures the per-request
// cost of the EC-3-3 webhook path: signature verify + JSON decode +
// idempotency reserve + bus publish.
func BenchmarkTikTokOrderHandler_ServeHTTP(b *testing.B) {
	bus := eventbus.NewInMemoryBus()
	defer bus.Close()
	verifier, err := social.NewTikTokWebhookVerifier(social.TikTokWebhookConfig{
		Secret: []byte("tiktok-webhook-bench-secret-bytes-fixture"),
		Now:    func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		b.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	handler, err := NewTikTokOrderHandler(nil, TikTokOrderHandlerConfig{
		Verifier:    verifier,
		Publisher:   bus,
		Idempotency: NewMemoryIdempotencyStore(),
		TenantID:    "tenant-bench",
		Metrics:     &capturingMetrics{},
		Now:         func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		b.Fatalf("NewTikTokOrderHandler: %v", err)
	}
	defer handler.Close(context.Background())
	body, _ := json.Marshal(map[string]any{
		"tenant_id":   "tenant-bench",
		"order_id":    "bench-order",
		"shop_id":     "shop-bench",
		"buyer_email": "b@example.com",
		"total_cents": 4999,
		"currency":    "AUD",
		"items": []map[string]any{
			{"sku": "SKU-1", "quantity": 1, "unit_cents": 4999, "product_id": "prod-1"},
		},
		"status":      "placed",
		"occurred_at": "2026-05-09T12:00:00Z",
	})
	header := verifier.SignWebhook(time.Date(2026, 5, 9, 11, 59, 30, 0, time.UTC).Unix(), body)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/tiktok/orders", bytes.NewReader(body))
		req.Header.Set("X-Tts-Signature", header)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		// The first call writes; subsequent calls are duplicate; both
		// exercise the verify path.
	}
}
