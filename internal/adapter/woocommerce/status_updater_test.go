// File scope: v3.9.0 carry-forward closure -- WooCommerce
// StatusUpdater RED tests.
package woocommerce

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/agent/fulfilment"
)

func TestWooCommerceStatusUpdater_PostsExpectedShape(t *testing.T) {
	t.Parallel()
	var seenPath, seenMethod, seenBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenMethod = r.Method
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		seenBody = string(buf)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":42,"status":"completed"}`))
	}))
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	updater, err := NewStatusUpdater(nil, client, "tenant-1")
	if err != nil {
		t.Fatalf("NewStatusUpdater: %v", err)
	}
	if updater.ChannelName() != WooCommerceChannelName {
		t.Fatalf("expected woocommerce channel, got %s", updater.ChannelName())
	}
	err = updater.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID:        "tenant-1",
		ExternalOrderID: "42",
		Status:          "delivered",
		TrackingNumber:  "AP-1",
		DeliveryDate:    time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}
	if seenMethod != http.MethodPut {
		t.Fatalf("expected PUT, got %s", seenMethod)
	}
	if !strings.HasSuffix(seenPath, "/orders/42") {
		t.Fatalf("expected /orders/42, got %s", seenPath)
	}
	if !strings.Contains(seenBody, `"status":"completed"`) {
		t.Fatalf("expected status completed (mapped from delivered) in body: %s", seenBody)
	}
}

func TestWooCommerceStatusUpdater_StatusMapping(t *testing.T) {
	cases := map[string]string{
		"delivered":  "completed",
		"shipped":    "processing",
		"in_transit": "processing",
		"exception":  "on-hold",
		"unknown":    "unknown",
	}
	for input, want := range cases {
		got := mapShipmentStatusToWoo(input)
		if got != want {
			t.Errorf("status %q: expected %q got %q", input, want, got)
		}
	}
}

func TestWooCommerceStatusUpdater_TenantRequired(t *testing.T) {
	t.Parallel()
	client := NewClient(Config{BaseURL: "https://example.test"}, http.DefaultClient)
	if _, err := NewStatusUpdater(nil, client, ""); !errors.Is(err, ErrWooCommerceStatusUpdateFailed) {
		t.Fatalf("expected ErrWooCommerceStatusUpdateFailed, got %v", err)
	}
}

func TestWooCommerceStatusUpdater_MissingExternalOrderID(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	updater, _ := NewStatusUpdater(nil, client, "tenant-1")
	err := updater.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID: "tenant-1",
		Status:   "shipped",
	})
	if !errors.Is(err, ErrWooCommerceStatusUpdateFailed) {
		t.Fatalf("expected ErrWooCommerceStatusUpdateFailed, got %v", err)
	}
}

func TestWooCommerceStatusUpdater_CloseIsNoOp(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	updater, _ := NewStatusUpdater(nil, client, "tenant-1")
	if err := updater.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestWooCommerceStatusUpdater_StringOrFallback(t *testing.T) {
	t.Parallel()
	if got := stringOrFallback("a", "b"); got != "a" {
		t.Errorf("expected a, got %s", got)
	}
	if got := stringOrFallback("", "b"); got != "b" {
		t.Errorf("expected b, got %s", got)
	}
	if got := stringOrFallback("   ", "b"); got != "b" {
		t.Errorf("expected b for whitespace, got %s", got)
	}
}

func TestWooCommerceStatusUpdater_BadStatusReturnsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"db down"}`))
	}))
	defer srv.Close()
	client := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	updater, _ := NewStatusUpdater(nil, client, "tenant-1")
	err := updater.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID:        "tenant-1",
		ExternalOrderID: "42",
		Status:          "shipped",
	})
	if !errors.Is(err, ErrWooCommerceStatusUpdateFailed) {
		t.Fatalf("expected ErrWooCommerceStatusUpdateFailed, got %v", err)
	}
}
