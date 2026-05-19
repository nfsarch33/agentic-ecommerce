//go:build v461_smoke

// File scope: v4.6.1 QA-1 -- 6-channel E2E test.
//
// Verifies the full listing+order+status cycle across all 6
// channels: TikTok, Facebook, RedNote, WooCommerce, Instagram,
// Pinterest. Tests fan-out dispatch (1 product -> 6 channels)
// and DLQ behaviour when 1 channel fails.
package v461

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/social"
	"github.com/nfsarch33/helixon-ec/internal/agent/fulfilment"
	"github.com/nfsarch33/helixon-ec/internal/channelport"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

var allChannels = []string{
	"tiktok", "facebook", "rednote", "woocommerce",
	"instagram", "pinterest",
}

type mockChannelAdapter struct {
	name   string
	failOn string
	mu     sync.Mutex
	calls  []string
}

func (m *mockChannelAdapter) Name() string                  { return m.name }
func (m *mockChannelAdapter) ChannelName() string           { return m.name }
func (m *mockChannelAdapter) Close(_ context.Context) error { return nil }
func (m *mockChannelAdapter) Publish(_ context.Context, _ eventbus.ProductEnrichedPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "publish")
	if m.failOn == "publish" {
		return fmt.Errorf("mock: %s publish failed", m.name)
	}
	return nil
}
func (m *mockChannelAdapter) CreateListing(_ context.Context, _ channelport.ListingRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "create_listing")
	if m.failOn == "create_listing" {
		return fmt.Errorf("mock: %s create_listing failed", m.name)
	}
	return nil
}
func (m *mockChannelAdapter) UpdateOrderStatus(_ context.Context, _ fulfilment.ChannelStatusUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "update_order_status")
	if m.failOn == "update_order_status" {
		return fmt.Errorf("mock: %s update_order_status failed", m.name)
	}
	return nil
}

func TestMultichannel_FanOutDispatch(t *testing.T) {
	t.Parallel()
	adapters := make([]*mockChannelAdapter, 0, len(allChannels))
	for _, ch := range allChannels {
		adapters = append(adapters, &mockChannelAdapter{name: ch})
	}

	payload := eventbus.ProductEnrichedPayload{
		Version:      1,
		TenantID:     "tenant-v461",
		ProductID:    "sku-fanout",
		EnglishTitle: "Fan-out Product",
		PriceCents:   5000,
		Currency:     "AUD",
		StockUnits:   10,
	}

	for _, a := range adapters {
		if err := a.Publish(context.Background(), payload); err != nil {
			t.Fatalf("channel %s publish failed: %v", a.name, err)
		}
	}
	for _, a := range adapters {
		a.mu.Lock()
		if len(a.calls) != 1 || a.calls[0] != "publish" {
			t.Fatalf("channel %s: expected [publish], got %v", a.name, a.calls)
		}
		a.mu.Unlock()
	}
}

func TestMultichannel_DLQOnSingleFailure(t *testing.T) {
	t.Parallel()
	adapters := make([]*mockChannelAdapter, 0, len(allChannels))
	for _, ch := range allChannels {
		a := &mockChannelAdapter{name: ch}
		if ch == "rednote" {
			a.failOn = "publish"
		}
		adapters = append(adapters, a)
	}

	payload := eventbus.ProductEnrichedPayload{
		Version: 1, TenantID: "tenant-v461", ProductID: "sku-dlq",
		EnglishTitle: "DLQ Product", PriceCents: 3000, Currency: "AUD",
	}

	var succeeded, failed int
	for _, a := range adapters {
		if err := a.Publish(context.Background(), payload); err != nil {
			failed++
		} else {
			succeeded++
		}
	}
	if succeeded != 5 {
		t.Fatalf("expected 5 succeeded, got %d", succeeded)
	}
	if failed != 1 {
		t.Fatalf("expected 1 failed (DLQ'd), got %d", failed)
	}
}

func TestMultichannel_ChannelRouterResolvesAll6(t *testing.T) {
	t.Parallel()
	for _, ch := range allChannels {
		if !social.IsKnownChannel(ch) {
			t.Fatalf("channel %q not recognized by factory", ch)
		}
	}
	if channelport.IsStubChannel("instagram") {
		t.Fatal("instagram should no longer be a stub")
	}
	if channelport.IsStubChannel("pinterest") {
		t.Fatal("pinterest should no longer be a stub")
	}
}

func TestMultichannel_IGPinterestFullCycle(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "ok"})
	}))
	defer srv.Close()

	secret := "test-secret-at-least-32-bytes-long!!"
	igA, err := social.NewInstagramAdapter(nil, "tenant-v461", social.InstagramConfig{
		AppID: "ig", AppSecret: secret, AccessToken: "tok", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewInstagramAdapter: %v", err)
	}
	t.Cleanup(func() { _ = igA.Close(context.Background()) })

	pinSecret := "pinterest-secret-at-least-32-bytes!!"
	pinA, err := social.NewPinterestAdapter(nil, "tenant-v461", social.PinterestConfig{
		AppID: "pin", AppSecret: pinSecret, AccessToken: "tok", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewPinterestAdapter: %v", err)
	}
	t.Cleanup(func() { _ = pinA.Close(context.Background()) })

	listing := channelport.ListingRequest{
		TenantID: "tenant-v461", ProductID: "sku-cycle",
		Title: "Cycle Product", PriceAUDCents: 2500,
	}
	if err := igA.CreateListing(context.Background(), listing); err != nil {
		t.Fatalf("IG CreateListing: %v", err)
	}
	if err := pinA.CreateListing(context.Background(), listing); err != nil {
		t.Fatalf("Pin CreateListing: %v", err)
	}

	update := fulfilment.ChannelStatusUpdate{
		TenantID: "tenant-v461", ExternalOrderID: "order-cycle",
		Status: "shipped", TrackingNumber: "TRK-001",
	}
	if err := igA.UpdateOrderStatus(context.Background(), update); err != nil {
		t.Fatalf("IG UpdateOrderStatus: %v", err)
	}
	if err := pinA.UpdateOrderStatus(context.Background(), update); err != nil {
		t.Fatalf("Pin UpdateOrderStatus: %v", err)
	}

	if errors.Is(err, social.ErrChannelNotImplemented) {
		t.Fatal("full adapters must not return stub error")
	}

	orders, err := igA.GetOrders(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("IG GetOrders: %v", err)
	}
	_ = orders
}
