package china

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestTaobaoAdapter_ProductDetailFetchesReviews is the EC-1-2 RED
// test: given a product ID, returns price history, review count,
// monthly sales volume.
func TestTaobaoAdapter_ProductDetailFetchesReviews(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/detail") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(productTaobaoJSON{
			ID:             "tb-12345",
			Title:          "Bluetooth Speaker",
			NativeCategory: "audio",
			PriceCNY:       59.0,
			MOQ:            5,
			LeadTimeDays:   10,
			SellerID:       "seller-001",
			SellerName:     "Audio Store",
			SellerRating:   4.7,
			SellerLevel:    "tmall_gold",
			ReviewCount:    1240,
			MonthlySales:   3500,
			URL:            "https://item.taobao.com/item.htm?id=tb-12345",
		})
	}))
	defer server.Close()

	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:       server.URL,
		SessionCookie: "session=test",
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	p, err := client.ProductDetail(context.Background(), ProductDetailRequest{ExternalID: "tb-12345"})
	if err != nil {
		t.Fatalf("ProductDetail: %v", err)
	}
	if p.ReviewCount != 1240 {
		t.Fatalf("review_count = %d, want 1240", p.ReviewCount)
	}
	if p.MonthlySalesUnit != 3500 {
		t.Fatalf("monthly_sales = %d, want 3500", p.MonthlySalesUnit)
	}
	if p.Category != "electronics" {
		t.Fatalf("category mapping audio -> electronics, got %q", p.Category)
	}
	if !p.SupplierVerified {
		t.Fatalf("expected SupplierVerified=true for tmall_gold seller")
	}
	if p.PriceCNYCents != 5900 {
		t.Fatalf("price_cny_cents = %d", p.PriceCNYCents)
	}
}

func TestTaobaoAdapter_SearchExponentialBackoffOn429(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(searchTaobaoResponse{Products: []productTaobaoJSON{
			{ID: "ok", NativeCategory: "audio"},
		}})
	}))
	defer server.Close()

	var sleeps atomic.Int32
	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:        server.URL,
		SessionCookie:  "session=test",
		BackoffInitial: 1 * time.Millisecond,
		BackoffMax:     10 * time.Millisecond,
		MaxRetries:     3,
		Sleep: func(d time.Duration) {
			sleeps.Add(1)
		},
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	products, err := client.Search(context.Background(), SearchRequest{Keyword: "speaker"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("got %d products, want 1", len(products))
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3 (2 fails + 1 success)", attempts.Load())
	}
	if sleeps.Load() != 2 {
		t.Fatalf("sleeps = %d, want 2 backoffs between 3 attempts", sleeps.Load())
	}
}

func TestTaobaoAdapter_BackoffExhaustedReturnsRateLimitedError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:        server.URL,
		SessionCookie:  "session=test",
		BackoffInitial: 1 * time.Millisecond,
		BackoffMax:     2 * time.Millisecond,
		MaxRetries:     2,
		Sleep:          func(d time.Duration) {}, // no-op
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	_, err = client.Search(context.Background(), SearchRequest{Keyword: "x"})
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if !errors.Is(err, ErrTaobaoRateLimited) {
		t.Fatalf("error not wrapping ErrTaobaoRateLimited: %v", err)
	}
}

func TestTaobaoAdapter_RejectsEmptyKeyword(t *testing.T) {
	t.Parallel()

	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:       "http://x",
		SessionCookie: "session=test",
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	_, err = client.Search(context.Background(), SearchRequest{Keyword: " "})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("error = %v, want ErrInvalidQuery", err)
	}
}

func TestTaobaoAdapter_RequiresSessionCookie(t *testing.T) {
	t.Parallel()

	_, err := NewTaobaoClient(nil, ConfigTaobao{BaseURL: "http://x"})
	if !errors.Is(err, ErrAdapterUnconfigured) {
		t.Fatalf("error = %v, want ErrAdapterUnconfigured", err)
	}
}

func TestTaobaoAdapter_ProductDetailRejectsEmptyID(t *testing.T) {
	t.Parallel()

	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:       "http://x",
		SessionCookie: "session=test",
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	_, err = client.ProductDetail(context.Background(), ProductDetailRequest{ExternalID: ""})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("error = %v, want ErrInvalidQuery", err)
	}
}

func TestTaobaoAdapter_UnknownCategoryFlagsButReturns(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchTaobaoResponse{Products: []productTaobaoJSON{
			{ID: "x", NativeCategory: "exotic_unmapped", PriceCNY: 10},
		}})
	}))
	defer server.Close()

	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:       server.URL,
		SessionCookie: "session=test",
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	products, err := client.Search(context.Background(), SearchRequest{Keyword: "x"})
	if err != nil {
		t.Fatalf("Search returned error for unmapped category (should pass through): %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("got %d, want 1", len(products))
	}
	if products[0].Category != "unknown" {
		t.Fatalf("expected category=unknown, got %q", products[0].Category)
	}
}

func TestTaobaoAdapter_BackoffHonoursContextCancel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:        server.URL,
		SessionCookie:  "session=test",
		BackoffInitial: 1 * time.Millisecond,
		MaxRetries:     5,
		Sleep:          time.Sleep,
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	_, err = client.Search(ctx, SearchRequest{Keyword: "x"})
	if err == nil {
		t.Fatal("expected error from cancelled ctx")
	}
}

func TestTaobaoAdapter_SourceConstantIsTaobao(t *testing.T) {
	t.Parallel()

	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:       "http://x",
		SessionCookie: "session=test",
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()
	if got := client.Source(); got != SourceTaobao {
		t.Fatalf("Source = %q, want %q", got, SourceTaobao)
	}
}

func TestSupportedCategoriesContainsTopTen(t *testing.T) {
	t.Parallel()

	cats := SupportedCategories()
	if len(cats) < 10 {
		t.Fatalf("got %d categories, want >= 10", len(cats))
	}
	want := []string{"electronics", "kitchen", "fashion", "beauty", "fitness", "outdoor", "toys", "pets", "baby", "books"}
	for _, c := range want {
		found := false
		for _, x := range cats {
			if x == c {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("category %q missing from %v", c, cats)
		}
	}
}

func TestTaobaoAdapter_Non429ErrorReturnsImmediately(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	var sleeps atomic.Int32
	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:        server.URL,
		SessionCookie:  "session=test",
		BackoffInitial: time.Millisecond,
		MaxRetries:     5,
		Sleep:          func(d time.Duration) { sleeps.Add(1) },
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	_, err = client.Search(context.Background(), SearchRequest{Keyword: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v, want contain status 500", err)
	}
	if sleeps.Load() != 0 {
		t.Fatalf("non-429 should not sleep, got %d sleeps", sleeps.Load())
	}
}

func TestTaobaoAdapter_CloseDoesNotBlock(t *testing.T) {
	t.Parallel()

	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:       "http://x",
		SessionCookie: "session=test",
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestTaobaoAdapter_SearchCallAfterCloseFails(t *testing.T) {
	t.Parallel()

	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:        "http://127.0.0.1:1",
		SessionCookie:  "session=test",
		BackoffInitial: time.Millisecond,
		MaxRetries:     0,
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, err = client.Search(context.Background(), SearchRequest{Keyword: "x"})
	if err == nil {
		t.Fatal("expected error post-close")
	}
}
