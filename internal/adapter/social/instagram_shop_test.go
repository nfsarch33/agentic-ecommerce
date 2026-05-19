package social

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/agent/fulfilment"
	"github.com/nfsarch33/helixon-ec/internal/channelport"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

func newTestIGServer(t *testing.T, statusCode int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
}

func newTestIGAdapter(t *testing.T, serverURL string) *InstagramAdapter {
	t.Helper()
	secret := "test-secret-at-least-32-bytes-long!!"
	a, err := NewInstagramAdapter(nil, "tenant-v460", InstagramConfig{
		AppID:       "test-app-id",
		AppSecret:   secret,
		AccessToken: "test-access-token",
		BaseURL:     serverURL,
		Now:         func() time.Time { return time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewInstagramAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	return a
}

func TestInstagram_CatalogSync(t *testing.T) {
	t.Parallel()
	srv := newTestIGServer(t, http.StatusOK, map[string]string{"id": "prod-1"})
	defer srv.Close()
	a := newTestIGAdapter(t, srv.URL)

	err := a.Publish(context.Background(), eventbus.ProductEnrichedPayload{
		Version:      1,
		TenantID:     "tenant-v460",
		ProductID:    "sku-1",
		EnglishTitle: "Test IG Product",
		PriceCents:   4990,
		Currency:     "AUD",
		StockUnits:   10,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func TestInstagram_OrderFetch(t *testing.T) {
	t.Parallel()
	orders := map[string]any{
		"data": []map[string]any{
			{"id": "order-1", "order_status": "completed", "total_amount_cents": 5000},
		},
	}
	srv := newTestIGServer(t, http.StatusOK, orders)
	defer srv.Close()
	a := newTestIGAdapter(t, srv.URL)

	got, err := a.GetOrders(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("GetOrders: %v", err)
	}
	if len(got) != 1 || got[0].OrderID != "order-1" {
		t.Fatalf("unexpected orders: %+v", got)
	}
}

func TestInstagram_StatusUpdate(t *testing.T) {
	t.Parallel()
	srv := newTestIGServer(t, http.StatusOK, nil)
	defer srv.Close()
	a := newTestIGAdapter(t, srv.URL)

	err := a.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID:        "tenant-v460",
		ExternalOrderID: "ig-order-1",
		Status:          "shipped",
		TrackingNumber:  "ETK-001",
	})
	if err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}
}

func TestInstagram_WebhookVerify(t *testing.T) {
	t.Parallel()
	secret := "test-secret-at-least-32-bytes-long!!"
	a, _ := NewInstagramAdapter(nil, "tenant-v460", InstagramConfig{
		AppID:       "test-app-id",
		AppSecret:   secret,
		AccessToken: "test-access-token",
	})
	t.Cleanup(func() { _ = a.Close(context.Background()) })

	payload := []byte(`{"object":"page","entry":[]}`)
	sig, err := SignFacebookWebhook([]byte(secret), payload)
	if err != nil {
		t.Fatalf("SignFacebookWebhook: %v", err)
	}
	if err := a.VerifyWebhook(sig, payload); err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
}

func TestInstagram_WebhookRejectBadSignature(t *testing.T) {
	t.Parallel()
	secret := "test-secret-at-least-32-bytes-long!!"
	a, _ := NewInstagramAdapter(nil, "tenant-v460", InstagramConfig{
		AppID:       "test-app-id",
		AppSecret:   secret,
		AccessToken: "test-access-token",
	})
	t.Cleanup(func() { _ = a.Close(context.Background()) })

	payload := []byte(`{"object":"page","entry":[]}`)
	if err := a.VerifyWebhook("sha256=badbadbadbad", payload); err == nil {
		t.Fatal("expected error for bad signature")
	}
}

func TestInstagram_StubToFullMigration(t *testing.T) {
	t.Parallel()
	srv := newTestIGServer(t, http.StatusOK, nil)
	defer srv.Close()
	a := newTestIGAdapter(t, srv.URL)

	if a.Name() != InstagramChannelName {
		t.Fatalf("Name=%q want=%q", a.Name(), InstagramChannelName)
	}
	err := a.CreateListing(context.Background(), channelport.ListingRequest{
		TenantID:      "tenant-v460",
		ProductID:     "sku-1",
		Channel:       InstagramChannelName,
		Title:         "IG Product",
		PriceAUDCents: 4990,
	})
	if err != nil {
		t.Fatalf("CreateListing should succeed (not ErrChannelNotImplemented): %v", err)
	}
	if errors.Is(err, ErrChannelNotImplemented) {
		t.Fatal("production adapter must not return ErrChannelNotImplemented")
	}
}
