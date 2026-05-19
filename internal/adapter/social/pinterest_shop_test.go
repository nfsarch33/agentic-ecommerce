package social

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

func newTestPinServer(t *testing.T, statusCode int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
}

func newTestPinAdapter(t *testing.T, serverURL string) *PinterestAdapter {
	t.Helper()
	secret := "pinterest-secret-at-least-32-bytes!!"
	a, err := NewPinterestAdapter(nil, "tenant-v460", PinterestConfig{
		AppID:       "pin-app-id",
		AppSecret:   secret,
		AccessToken: "pin-access-token",
		BaseURL:     serverURL,
		Now:         func() time.Time { return time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewPinterestAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	return a
}

func TestPinterest_CatalogFeed(t *testing.T) {
	t.Parallel()
	srv := newTestPinServer(t, http.StatusOK, map[string]string{"id": "pin-1"})
	defer srv.Close()
	a := newTestPinAdapter(t, srv.URL)

	err := a.Publish(context.Background(), eventbus.ProductEnrichedPayload{
		Version:      1,
		TenantID:     "tenant-v460",
		ProductID:    "sku-1",
		EnglishTitle: "Pinterest Product",
		PriceCents:   3500,
		Currency:     "AUD",
		StockUnits:   5,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func TestPinterest_ProductPin(t *testing.T) {
	t.Parallel()
	srv := newTestPinServer(t, http.StatusOK, nil)
	defer srv.Close()
	a := newTestPinAdapter(t, srv.URL)

	err := a.CreateListing(context.Background(), channelport.ListingRequest{
		TenantID:      "tenant-v460",
		ProductID:     "sku-2",
		Channel:       PinterestChannelName,
		Title:         "Pin Product",
		PriceAUDCents: 2999,
	})
	if err != nil {
		t.Fatalf("CreateListing: %v", err)
	}
}

func TestPinterest_OrderTracking(t *testing.T) {
	t.Parallel()
	srv := newTestPinServer(t, http.StatusOK, nil)
	defer srv.Close()
	a := newTestPinAdapter(t, srv.URL)

	err := a.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID:        "tenant-v460",
		ExternalOrderID: "pin-order-1",
		Status:          "shipped",
		TrackingNumber:  "PTK-001",
	})
	if err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}
}

func TestPinterest_WebhookVerify(t *testing.T) {
	t.Parallel()
	secret := "pinterest-secret-at-least-32-bytes!!"
	a := newTestPinAdapter(t, "http://unused")
	a.cfg.AppSecret = secret

	payload := []byte(`{"event":"order.created"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil))

	if err := a.VerifyWebhook(sig, payload); err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
}

func TestPinterest_WebhookRejectBadSig(t *testing.T) {
	t.Parallel()
	a := newTestPinAdapter(t, "http://unused")

	payload := []byte(`{"event":"order.created"}`)
	if err := a.VerifyWebhook("badbadbadbad", payload); !errors.Is(err, ErrPinterestSignatureBad) {
		t.Fatalf("expected ErrPinterestSignatureBad, got: %v", err)
	}
}

func TestPinterest_StubToFullMigration(t *testing.T) {
	t.Parallel()
	srv := newTestPinServer(t, http.StatusOK, nil)
	defer srv.Close()
	a := newTestPinAdapter(t, srv.URL)

	if a.Name() != PinterestChannelName {
		t.Fatalf("Name=%q want=%q", a.Name(), PinterestChannelName)
	}
	err := a.CreateListing(context.Background(), channelport.ListingRequest{
		TenantID:      "tenant-v460",
		ProductID:     "sku-1",
		Channel:       PinterestChannelName,
		Title:         "Pin Product",
		PriceAUDCents: 2999,
	})
	if err != nil {
		t.Fatalf("CreateListing should succeed: %v", err)
	}
	if errors.Is(err, ErrChannelNotImplemented) {
		t.Fatal("production adapter must not return ErrChannelNotImplemented")
	}
}
