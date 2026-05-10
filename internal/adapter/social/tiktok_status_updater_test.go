// File scope: v3.9.0 carry-forward closure -- TikTokStatusUpdater
// RED tests verifying the v3.8.0 EC-7-4 ChannelStatusUpdater
// contract and HTTP wire shape.
package social

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/fulfilment"
)

func TestTikTokStatusUpdater_PostsExpectedShape(t *testing.T) {
	t.Parallel()
	var (
		mu       sync.Mutex
		seenPath string
		seenBody string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/api/oauth/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "tok-123",
				"refresh_token": "ref-123",
				"expires_in":    3600,
			})
			return
		}
		seenPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		seenBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
	}))
	defer srv.Close()

	store := NewMemoryTokenStore()
	if err := store.Put(context.Background(), TikTokToken{
		TenantID:    "tenant-1",
		ShopID:      "shop-1",
		AccessToken: "tok-123",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("token put: %v", err)
	}
	tm, err := NewTokenManager(TokenManagerConfig{Store: store, Exchanger: stubTokenExchanger{}})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	client, err := NewTikTokShopClient(nil, TikTokShopConfig{
		HTTPClient:   srv.Client(),
		TokenManager: tm,
		BaseURL:      srv.URL,
		ClientID:     "client-1",
		ClientSecret: []byte("0123456789abcdef0123456789abcdef"),
		TenantID:     "tenant-1",
		Now:          func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("NewTikTokShopClient: %v", err)
	}
	defer client.Close(context.Background())

	updater, err := NewTikTokStatusUpdater(nil, client, "tenant-1")
	if err != nil {
		t.Fatalf("NewTikTokStatusUpdater: %v", err)
	}
	if updater.ChannelName() != TikTokChannelName {
		t.Fatalf("expected channel=%s, got %s", TikTokChannelName, updater.ChannelName())
	}

	err = updater.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID:        "tenant-1",
		ExternalOrderID: "ord-123",
		Status:          "shipped",
		TrackingNumber:  "AP-XYZ",
		DeliveryDate:    time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seenPath != "/api/orders/ord-123/shipments" {
		t.Fatalf("unexpected path %q", seenPath)
	}
	if !contains(seenBody, "\"tracking_number\":\"AP-XYZ\"") {
		t.Fatalf("expected tracking_number in body: %s", seenBody)
	}
	if !contains(seenBody, "\"status\":\"shipped\"") {
		t.Fatalf("expected status in body: %s", seenBody)
	}
}

func TestTikTokStatusUpdater_MissingExternalOrderID(t *testing.T) {
	t.Parallel()
	updater := buildTikTokStatusHarness(t)
	err := updater.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID: "tenant-1",
		Status:   "shipped",
	})
	if !errors.Is(err, ErrTikTokUnconfigured) {
		t.Fatalf("expected ErrTikTokUnconfigured, got %v", err)
	}
}

func TestTikTokStatusUpdater_CloseDelegates(t *testing.T) {
	t.Parallel()
	updater := buildTikTokStatusHarness(t)
	if err := updater.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestTikTokStatusUpdater_NilClientRejected(t *testing.T) {
	t.Parallel()
	if _, err := NewTikTokStatusUpdater(nil, nil, "tenant-1"); !errors.Is(err, ErrTikTokUnconfigured) {
		t.Fatalf("expected ErrTikTokUnconfigured, got %v", err)
	}
}

func TestTikTokStatusUpdater_TenantRequired(t *testing.T) {
	t.Parallel()
	store := NewMemoryTokenStore()
	tm, err := NewTokenManager(TokenManagerConfig{Store: store, Exchanger: stubTokenExchanger{}})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	client, err := NewTikTokShopClient(nil, TikTokShopConfig{
		TokenManager: tm,
		ClientID:     "x",
		ClientSecret: []byte("0123456789abcdef0123456789abcdef"),
		TenantID:     "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewTikTokShopClient: %v", err)
	}
	defer client.Close(context.Background())
	if _, err := NewTikTokStatusUpdater(nil, client, ""); !errors.Is(err, ErrTikTokUnconfigured) {
		t.Fatalf("expected ErrTikTokUnconfigured, got %v", err)
	}
}

func buildTikTokStatusHarness(t *testing.T) *TikTokStatusUpdater {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	t.Cleanup(srv.Close)
	store := NewMemoryTokenStore()
	if err := store.Put(context.Background(), TikTokToken{
		TenantID: "tenant-1", AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("token put: %v", err)
	}
	tm, err := NewTokenManager(TokenManagerConfig{Store: store, Exchanger: stubTokenExchanger{}})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	client, err := NewTikTokShopClient(nil, TikTokShopConfig{
		HTTPClient:   srv.Client(),
		TokenManager: tm,
		BaseURL:      srv.URL,
		ClientID:     "client-1",
		ClientSecret: []byte("0123456789abcdef0123456789abcdef"),
		TenantID:     "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewTikTokShopClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	updater, err := NewTikTokStatusUpdater(nil, client, "tenant-1")
	if err != nil {
		t.Fatalf("NewTikTokStatusUpdater: %v", err)
	}
	return updater
}

// stubTokenExchanger is a no-op TokenExchanger used purely to
// satisfy NewTokenManager's contract; the seeded token never
// expires in the tests below so Refresh is never invoked.
type stubTokenExchanger struct{}

func (stubTokenExchanger) Exchange(_ context.Context, req OAuthBootstrapRequest) (TikTokToken, error) {
	return TikTokToken{TenantID: req.TenantID, AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (stubTokenExchanger) Refresh(_ context.Context, _, tenantID string) (TikTokToken, error) {
	return TikTokToken{TenantID: tenantID, AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
