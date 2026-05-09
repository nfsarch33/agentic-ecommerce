package social

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/dnaeon/go-vcr.v3/cassette"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
)

const tiktokCassetteBase = "https://cassette.tiktokshop.local"

// signedTokenManager wires a TokenManager + MemoryStore primed with
// a valid token so client request paths skip the OAuth bootstrap.
func signedTokenManager(t *testing.T, tenantID string) *TokenManager {
	t.Helper()
	store := NewMemoryTokenStore()
	tok := TikTokToken{
		TenantID:     tenantID,
		ShopID:       "shop-1",
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}
	if err := store.Put(context.Background(), tok); err != nil {
		t.Fatalf("Put token: %v", err)
	}
	mgr, err := NewTokenManager(TokenManagerConfig{Store: store, Exchanger: &fakeExchanger{}})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	return mgr
}

func newClientWithBaseURL(t *testing.T, base string, httpClient *http.Client, hook TikTokMetricsHook) *TikTokShopClient {
	t.Helper()
	mgr := signedTokenManager(t, "tenant-test")
	cfg := TikTokShopConfig{
		HTTPClient:   httpClient,
		TokenManager: mgr,
		BaseURL:      base,
		ClientID:     "test-client-id",
		ClientSecret: []byte(testTikTokSecret),
		TenantID:     "tenant-test",
		Now:          func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
		MetricsHook:  hook,
	}
	c, err := NewTikTokShopClient(slog.Default(), cfg)
	if err != nil {
		t.Fatalf("NewTikTokShopClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// TestTikTokShopClient_ListProductsReturnsPagedResults is the
// EC-3-1 RED acceptance test. Cassette-backed via dnaeon/go-vcr/v3
// mirroring the v3.1.0 China adapter pattern.
func TestTikTokShopClient_ListProductsReturnsPagedResults(t *testing.T) {
	t.Parallel()

	rec := newReplayTikTokCassette(t, "testdata/cassettes/tiktok_products_search")
	client := newClientWithBaseURL(t, tiktokCassetteBase, rec.GetDefaultClient(), nil)

	page, err := client.ListProducts(context.Background(), TikTokListProductsRequest{
		TenantID: "tenant-test",
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(page.Products) != 10 {
		t.Fatalf("len = %d, want 10", len(page.Products))
	}
	if page.NextPage != "page-cursor-2" {
		t.Fatalf("NextPage = %q", page.NextPage)
	}
	first := page.Products[0]
	if first.ID != "tt-cassette-product-01" {
		t.Fatalf("first.ID = %q", first.ID)
	}
	if first.PriceCents != 1999 || first.Stock != 50 || first.Currency != "AUD" {
		t.Fatalf("first = %+v", first)
	}
	if first.UpdatedAt.IsZero() {
		t.Fatalf("first.UpdatedAt is zero")
	}
}

func TestTikTokShopClient_CreateProductSignsAndReturnsID(t *testing.T) {
	t.Parallel()

	var capturedSig string
	var capturedTokenHeader string
	var capturedTimestamp string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/products" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		capturedSig = r.Header.Get("X-Tts-Sign")
		capturedTokenHeader = r.Header.Get("X-Tts-Access-Token")
		capturedTimestamp = r.Header.Get("X-Tts-Timestamp")
		_ = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"product_id": "tt-12345"},
		})
	}))
	t.Cleanup(srv.Close)
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), nil)

	id, err := client.CreateProduct(context.Background(), TikTokProductPayload{
		TenantID:   "tenant-test",
		ExternalID: "local-1",
		Title:      "Headphones",
		PriceCents: 4999,
		Currency:   "AUD",
		StockUnits: 25,
		CategoryID: "cat-audio",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if id != "tt-12345" {
		t.Fatalf("id = %q", id)
	}
	if capturedSig == "" {
		t.Fatalf("missing X-Tts-Sign")
	}
	if capturedTokenHeader != "test-access-token" {
		t.Fatalf("token header = %q", capturedTokenHeader)
	}
	if capturedTimestamp == "" {
		t.Fatalf("missing X-Tts-Timestamp")
	}
}

func TestTikTokShopClient_RateLimitMapsToSentinel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":429,"message":"rate limited"}`))
	}))
	t.Cleanup(srv.Close)
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), nil)
	_, err := client.ListProducts(context.Background(), TikTokListProductsRequest{TenantID: "tenant-test"})
	if !errors.Is(err, ErrTikTokRateLimited) {
		t.Fatalf("err = %v, want ErrTikTokRateLimited", err)
	}
}

func TestTikTokShopClient_AuthFailureMapsToSentinel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401}`))
	}))
	t.Cleanup(srv.Close)
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), nil)
	_, err := client.ListProducts(context.Background(), TikTokListProductsRequest{TenantID: "tenant-test"})
	if !errors.Is(err, ErrTikTokAuthFailed) {
		t.Fatalf("err = %v, want ErrTikTokAuthFailed", err)
	}
}

func TestTikTokShopClient_DeleteProduct404IsIdempotent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), nil)
	if err := client.DeleteProduct(context.Background(), "tt-99"); err != nil {
		t.Fatalf("DeleteProduct: %v", err)
	}
}

func TestTikTokShopClient_SyncInventoryRequiresSKU(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	t.Cleanup(srv.Close)
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), nil)
	err := client.SyncInventory(context.Background(), TikTokInventoryUpdate{TenantID: "tenant-test", Delta: -1})
	if !errors.Is(err, ErrTikTokUnconfigured) {
		t.Fatalf("err = %v, want ErrTikTokUnconfigured", err)
	}
}

func TestTikTokShopClient_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), nil)
	_ = client.Close(context.Background())
	_, err := client.ListProducts(context.Background(), TikTokListProductsRequest{TenantID: "tenant-test"})
	if !errors.Is(err, ErrTikTokClosed) {
		t.Fatalf("err = %v, want ErrTikTokClosed", err)
	}
}

func TestTikTokShopClient_ConfigValidation(t *testing.T) {
	t.Parallel()
	cases := map[string]TikTokShopConfig{
		"missing tokenmgr":  {ClientID: "id", ClientSecret: []byte(testTikTokSecret), TenantID: "t"},
		"missing client_id": {TokenManager: &TokenManager{}, ClientSecret: []byte(testTikTokSecret), TenantID: "t"},
		"short secret":      {TokenManager: &TokenManager{}, ClientID: "id", ClientSecret: []byte("short"), TenantID: "t"},
		"missing tenant":    {TokenManager: &TokenManager{}, ClientID: "id", ClientSecret: []byte(testTikTokSecret)},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewTikTokShopClient(nil, cfg)
			if err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestTikTokShopClient_OAuthExchange(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/token" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ax",
			"refresh_token": "rf",
			"expires_in":    7200,
			"scope":         "shop.products",
		})
	}))
	t.Cleanup(srv.Close)
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), nil)
	tok, err := client.Exchange(context.Background(), OAuthBootstrapRequest{
		TenantID:          "tenant-test",
		ShopID:            "shop-99",
		AuthorizationCode: "code",
		Verifier:          "ver",
	})
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.AccessToken != "ax" || tok.TenantID != "tenant-test" || tok.ShopID != "shop-99" {
		t.Fatalf("tok = %+v", tok)
	}
}

func TestTikTokShopClient_OAuthExchangeUnauthorised(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), nil)
	_, err := client.Exchange(context.Background(), OAuthBootstrapRequest{
		TenantID: "tenant-test", AuthorizationCode: "c", Verifier: "v",
	})
	if !errors.Is(err, ErrTikTokAuthFailed) {
		t.Fatalf("err = %v", err)
	}
}

type recorderHook struct {
	calls atomic.Int64
}

func (r *recorderHook) RecordAPICall(string, string, string, float64) { r.calls.Add(1) }
func (r *recorderHook) RecordListing(string, string)                  {}
func (r *recorderHook) RecordWebhook(string, string, string)          {}
func (r *recorderHook) RecordInventorySync(string, string, string)    {}
func (r *recorderHook) RecordSignatureFailure(string, string)         {}

func TestTikTokShopClient_MetricsHookFires(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"data":{"products":[]}}`))
	}))
	t.Cleanup(srv.Close)
	hook := &recorderHook{}
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), hook)
	if _, err := client.ListProducts(context.Background(), TikTokListProductsRequest{TenantID: "tenant-test"}); err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if got := hook.calls.Load(); got == 0 {
		t.Fatalf("expected metrics hook to fire, got %d", got)
	}
}

func TestTikTokCassettesContainNoSecrets(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"testdata/cassettes/tiktok_products_search.yaml",
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read cassette: %v", err)
			}
			lower := strings.ToLower(string(data))
			for _, marker := range []string{"client_secret", "x-tts-access-token", "authorization", "bearer "} {
				if strings.Contains(lower, marker) {
					t.Fatalf("cassette %s contains forbidden marker %q", path, marker)
				}
			}
		})
	}
}

func newReplayTikTokCassette(t *testing.T, cassetteName string) *recorder.Recorder {
	t.Helper()
	rec, err := recorder.NewWithOptions(&recorder.Options{
		CassetteName: cassetteName,
		Mode:         recorder.ModeReplayOnly,
	})
	if err != nil {
		t.Fatalf("new cassette recorder %s: %v", cassetteName, err)
	}
	rec.SetMatcher(matchTikTokReplay)
	t.Cleanup(func() {
		if err := rec.Stop(); err != nil {
			t.Errorf("stop cassette recorder %s: %v", cassetteName, err)
		}
	})
	return rec
}

func matchTikTokReplay(req *http.Request, recorded cassette.Request) bool {
	recordedURL, err := url.Parse(recorded.URL)
	if err != nil {
		return false
	}
	return req.Method == recorded.Method &&
		req.URL.Path == recordedURL.Path
}
