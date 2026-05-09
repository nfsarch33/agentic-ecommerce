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

const facebookCassetteBase = "https://cassette.facebook.local"

// signedFacebookTokenManager wires a FacebookTokenManager + memory
// store primed with a fresh token so client request paths skip the
// OAuth bootstrap.
func signedFacebookTokenManager(t *testing.T, tenantID string) *FacebookTokenManager {
	t.Helper()
	store := NewFacebookMemoryTokenStore()
	tok := FacebookToken{
		TenantID:    tenantID,
		PageID:      "page-1",
		AccessToken: "test-page-token",
		ExpiresAt:   time.Now().Add(2 * time.Hour),
	}
	if err := store.Put(context.Background(), tok); err != nil {
		t.Fatalf("Put token: %v", err)
	}
	mgr, err := NewFacebookTokenManager(FacebookTokenManagerConfig{Store: store, Exchanger: &fakeFacebookExchanger{}})
	if err != nil {
		t.Fatalf("NewFacebookTokenManager: %v", err)
	}
	return mgr
}

// mustNewFakeFBClient constructs a FacebookShopClient pointed at
// the supplied base URL and HTTP client. The factory matches the
// signedTokenManager pattern from the EC-3-1 TikTok suite.
func mustNewFakeFBClient(t *testing.T, base string, httpClient *http.Client, hook FacebookMetricsHook) *FacebookShopClient {
	t.Helper()
	mgr := signedFacebookTokenManager(t, "tenant-fb")
	cfg := FacebookShopConfig{
		HTTPClient:   httpClient,
		TokenManager: mgr,
		BaseURL:      base,
		AppID:        "test-app-id",
		AppSecret:    []byte(testFacebookSecret),
		CatalogueID:  "cat-12345",
		TenantID:     "tenant-fb",
		Now:          func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
		MetricsHook:  hook,
	}
	c, err := NewFacebookShopClient(slog.Default(), cfg)
	if err != nil {
		t.Fatalf("NewFacebookShopClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// TestFacebookShopClient_CreateProductInCatalogue is the EC-4-2 RED
// acceptance test. Cassette-backed via dnaeon/go-vcr/v3 mirroring
// the v3.3.0 TikTok adapter pattern.
func TestFacebookShopClient_CreateProductInCatalogue(t *testing.T) {
	t.Parallel()

	rec := newReplayFacebookCassette(t, "testdata/cassettes/facebook_catalog_create")
	client := mustNewFakeFBClient(t, facebookCassetteBase, rec.GetDefaultClient(), nil)

	created, err := client.CreateProduct(context.Background(), FacebookProductPayload{
		TenantID:     "tenant-fb",
		RetailerID:   "local-1",
		Name:         "Wireless Earbuds",
		Description:  "Active noise cancelling, 36h battery.",
		CategoryID:   "audio",
		BrandName:    "Acme",
		PriceCents:   4999,
		Currency:     "AUD",
		StockUnits:   25,
		Availability: "in stock",
		Condition:    "new",
		ImageURL:     "https://cdn.example.com/img1.jpg",
		URL:          "https://shop.example.com/p/local-1",
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if created.RemoteID != "fb-cassette-product-001" {
		t.Fatalf("RemoteID = %q", created.RemoteID)
	}
	if created.RetailerID != "local-1" {
		t.Fatalf("RetailerID = %q", created.RetailerID)
	}
}

// TestFacebookShopClient_BulkBatch_100ProductsSingleCall is the
// EC-4-2 RED acceptance test for the bulk import path. Operator
// passes 100 payloads in a single call; client chunks under
// MaxFacebookBatchSize transparently.
func TestFacebookShopClient_BulkBatch_100ProductsSingleCall(t *testing.T) {
	t.Parallel()

	rec := newReplayFacebookCassette(t, "testdata/cassettes/facebook_catalog_batch_100")
	client := mustNewFakeFBClient(t, facebookCassetteBase, rec.GetDefaultClient(), nil)

	payloads := make([]FacebookProductPayload, 0, 100)
	for i := 1; i <= 100; i++ {
		payloads = append(payloads, FacebookProductPayload{
			TenantID:   "tenant-fb",
			RetailerID: makeRetailerID(i),
			Name:       "Bulk Product",
			PriceCents: 1999,
			Currency:   "AUD",
		})
	}
	results, err := client.CreateProductBatch(context.Background(), payloads)
	if err != nil {
		t.Fatalf("CreateProductBatch: %v", err)
	}
	if len(results) != 100 {
		t.Fatalf("results = %d, want 100", len(results))
	}
	for i, r := range results {
		if r.Error != nil {
			t.Fatalf("results[%d].Error = %v", i, r.Error)
		}
		if r.RetailerID != makeRetailerID(i+1) {
			t.Fatalf("results[%d].RetailerID = %q", i, r.RetailerID)
		}
		if r.RemoteID == "" {
			t.Fatalf("results[%d].RemoteID empty", i)
		}
	}
}

func TestFacebookShopClient_AppSecretProofIsAttached(t *testing.T) {
	t.Parallel()
	var capturedQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query()
		_, _ = io.WriteString(w, `{"id":"fb-1"}`)
	}))
	t.Cleanup(srv.Close)
	client := mustNewFakeFBClient(t, srv.URL, srv.Client(), nil)
	_, err := client.CreateProduct(context.Background(), FacebookProductPayload{TenantID: "tenant-fb", RetailerID: "r-1", Name: "X", PriceCents: 100, Currency: "AUD"})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if capturedQuery.Get("appsecret_proof") == "" {
		t.Fatalf("missing appsecret_proof query parameter")
	}
	if capturedQuery.Get("access_token") != "test-page-token" {
		t.Fatalf("access_token = %q", capturedQuery.Get("access_token"))
	}
}

func TestFacebookShopClient_RateLimitMapsToSentinel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":4,"message":"rate limit"}}`))
	}))
	t.Cleanup(srv.Close)
	client := mustNewFakeFBClient(t, srv.URL, srv.Client(), nil)
	_, err := client.CreateProduct(context.Background(), FacebookProductPayload{TenantID: "tenant-fb", RetailerID: "r-1"})
	if !errors.Is(err, ErrFacebookRateLimited) {
		t.Fatalf("err = %v, want ErrFacebookRateLimited", err)
	}
}

func TestFacebookShopClient_AuthFailureMapsToSentinel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":190,"message":"OAuthException"}}`))
	}))
	t.Cleanup(srv.Close)
	client := mustNewFakeFBClient(t, srv.URL, srv.Client(), nil)
	_, err := client.CreateProduct(context.Background(), FacebookProductPayload{TenantID: "tenant-fb", RetailerID: "r-1"})
	if !errors.Is(err, ErrFacebookAuthFailed) {
		t.Fatalf("err = %v, want ErrFacebookAuthFailed", err)
	}
}

func TestFacebookShopClient_GraphErrorMapsToSentinel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":100,"message":"Invalid parameter"}}`))
	}))
	t.Cleanup(srv.Close)
	client := mustNewFakeFBClient(t, srv.URL, srv.Client(), nil)
	_, err := client.CreateProduct(context.Background(), FacebookProductPayload{TenantID: "tenant-fb", RetailerID: "r-1"})
	if !errors.Is(err, ErrFacebookGraphAPIError) {
		t.Fatalf("err = %v, want ErrFacebookGraphAPIError", err)
	}
}

func TestFacebookShopClient_BatchTooLargeRejected(t *testing.T) {
	t.Parallel()
	client := mustNewFakeFBClient(t, "https://nope.local", http.DefaultClient, nil)
	payloads := make([]FacebookProductPayload, 101)
	_, err := client.CreateProductBatch(context.Background(), payloads)
	if !errors.Is(err, ErrFacebookBatchTooLarge) {
		t.Fatalf("err = %v, want ErrFacebookBatchTooLarge", err)
	}
}

func TestFacebookShopClient_BatchEmptyRejected(t *testing.T) {
	t.Parallel()
	client := mustNewFakeFBClient(t, "https://nope.local", http.DefaultClient, nil)
	_, err := client.CreateProductBatch(context.Background(), nil)
	if !errors.Is(err, ErrFacebookUnconfigured) {
		t.Fatalf("err = %v, want ErrFacebookUnconfigured", err)
	}
}

func TestFacebookShopClient_SyncInventoryRequiresRetailer(t *testing.T) {
	t.Parallel()
	client := mustNewFakeFBClient(t, "https://nope.local", http.DefaultClient, nil)
	err := client.SyncInventory(context.Background(), FacebookInventoryUpdate{TenantID: "tenant-fb"})
	if !errors.Is(err, ErrFacebookUnconfigured) {
		t.Fatalf("err = %v, want ErrFacebookUnconfigured", err)
	}
}

func TestFacebookShopClient_PushOrderStatusRequiresOrderID(t *testing.T) {
	t.Parallel()
	client := mustNewFakeFBClient(t, "https://nope.local", http.DefaultClient, nil)
	err := client.PushOrderStatus(context.Background(), FacebookOrderStatusPush{TenantID: "tenant-fb"})
	if !errors.Is(err, ErrFacebookUnconfigured) {
		t.Fatalf("err = %v, want ErrFacebookUnconfigured", err)
	}
}

func TestFacebookShopClient_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	client := mustNewFakeFBClient(t, "https://nope.local", http.DefaultClient, nil)
	_ = client.Close(context.Background())
	_, err := client.CreateProduct(context.Background(), FacebookProductPayload{RetailerID: "r"})
	if !errors.Is(err, ErrFacebookClosed) {
		t.Fatalf("err = %v, want ErrFacebookClosed", err)
	}
}

func TestFacebookShopClient_ConfigValidation(t *testing.T) {
	t.Parallel()
	mgr := signedFacebookTokenManager(t, "tenant-fb")
	cases := map[string]FacebookShopConfig{
		"missing tokenmgr": {AppID: "id", AppSecret: []byte(testFacebookSecret), CatalogueID: "c", TenantID: "t"},
		"missing app_id":   {TokenManager: mgr, AppSecret: []byte(testFacebookSecret), CatalogueID: "c", TenantID: "t"},
		"short secret":     {TokenManager: mgr, AppID: "id", AppSecret: []byte("short"), CatalogueID: "c", TenantID: "t"},
		"missing catalog":  {TokenManager: mgr, AppID: "id", AppSecret: []byte(testFacebookSecret), TenantID: "t"},
		"missing tenant":   {TokenManager: mgr, AppID: "id", AppSecret: []byte(testFacebookSecret), CatalogueID: "c"},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewFacebookShopClient(nil, cfg)
			if err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestFacebookShopClient_OAuthExchange(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/access_token" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "long-lived-page-token",
			"token_type":   "bearer",
			"expires_in":   60 * 60 * 24 * 60,
		})
	}))
	t.Cleanup(srv.Close)
	client := mustNewFakeFBClient(t, srv.URL, srv.Client(), nil)
	tok, err := client.Exchange(context.Background(), FacebookOAuthBootstrapRequest{
		TenantID: "tenant-fb", PageID: "page-1", ShortLivedToken: "short", Scopes: []string{"catalog_management"},
	})
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.AccessToken != "long-lived-page-token" || tok.TenantID != "tenant-fb" || tok.PageID != "page-1" {
		t.Fatalf("tok = %+v", tok)
	}
}

func TestFacebookShopClient_OAuthExchangeUnauthorised(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	client := mustNewFakeFBClient(t, srv.URL, srv.Client(), nil)
	_, err := client.Exchange(context.Background(), FacebookOAuthBootstrapRequest{TenantID: "t", PageID: "p", ShortLivedToken: "s"})
	if !errors.Is(err, ErrFacebookAuthFailed) {
		t.Fatalf("err = %v", err)
	}
}

type recorderFBHook struct {
	calls atomic.Int64
}

func (r *recorderFBHook) RecordAPICall(string, string, string, float64) { r.calls.Add(1) }
func (r *recorderFBHook) RecordSignatureFailure(string, string)         {}

func TestFacebookShopClient_MetricsHookFires(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"fb-1"}`))
	}))
	t.Cleanup(srv.Close)
	hook := &recorderFBHook{}
	client := mustNewFakeFBClient(t, srv.URL, srv.Client(), hook)
	if _, err := client.CreateProduct(context.Background(), FacebookProductPayload{TenantID: "tenant-fb", RetailerID: "r-1", Name: "x", PriceCents: 100}); err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if got := hook.calls.Load(); got == 0 {
		t.Fatalf("expected metrics hook to fire, got %d", got)
	}
}

func TestFacebookCassettesContainNoSecrets(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"testdata/cassettes/facebook_catalog_create.yaml",
		"testdata/cassettes/facebook_catalog_batch_100.yaml",
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read cassette: %v", err)
			}
			lower := strings.ToLower(string(data))
			for _, marker := range []string{"app_secret", "client_secret", "appsecret_proof", "access_token", "bearer "} {
				if strings.Contains(lower, marker) {
					t.Fatalf("cassette %s contains forbidden marker %q", path, marker)
				}
			}
		})
	}
}

func TestChunkPayloads_TableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		size   int
		input  int
		chunks int
	}{
		{"empty", 50, 0, 0},
		{"one chunk", 50, 30, 1},
		{"exact chunk", 50, 50, 1},
		{"two chunks", 50, 100, 2},
		{"odd remainder", 50, 75, 2},
		{"size zero", 0, 5, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := make([]FacebookProductPayload, tc.input)
			out := chunkPayloads(in, tc.size)
			if len(out) != tc.chunks {
				t.Fatalf("chunks = %d, want %d", len(out), tc.chunks)
			}
		})
	}
}

func TestFormatFacebookPrice_Cents(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cents    int
		currency string
		want     string
	}{
		{0, "AUD", "0.00 AUD"},
		{100, "AUD", "1.00 AUD"},
		{4999, "USD", "49.99 USD"},
		{12345, "", "123.45 AUD"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := formatFacebookPrice(tc.cents, tc.currency); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func makeRetailerID(i int) string {
	return "bulk-" + threeDigit(i)
}

func threeDigit(i int) string {
	switch {
	case i < 10:
		return "00" + intToString(i)
	case i < 100:
		return "0" + intToString(i)
	default:
		return intToString(i)
	}
}

// intToString avoids strconv to keep the helper file dependency-free.
func intToString(i int) string {
	if i == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

func newReplayFacebookCassette(t *testing.T, cassetteName string) *recorder.Recorder {
	t.Helper()
	rec, err := recorder.NewWithOptions(&recorder.Options{
		CassetteName: cassetteName,
		Mode:         recorder.ModeReplayOnly,
	})
	if err != nil {
		t.Fatalf("new cassette recorder %s: %v", cassetteName, err)
	}
	rec.SetMatcher(matchFacebookReplay)
	t.Cleanup(func() {
		if err := rec.Stop(); err != nil {
			t.Errorf("stop cassette recorder %s: %v", cassetteName, err)
		}
	})
	return rec
}

func matchFacebookReplay(req *http.Request, recorded cassette.Request) bool {
	recordedURL, err := url.Parse(recorded.URL)
	if err != nil {
		return false
	}
	return req.Method == recorded.Method && req.URL.Path == recordedURL.Path
}
