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

// TestAdapter1688_SearchByKeywordReturnsProductList is the EC-1-1 RED
// test: given keyword "wireless earbuds", the adapter returns >=10
// products with price, MOQ, supplier_rating populated.
func TestAdapter1688_SearchByKeywordReturnsProductList(t *testing.T) {
	t.Parallel()

	server := newCassette1688Server(t, fixture1688Earbuds())
	defer server.Close()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       server.URL,
		SessionCookie: "session=test",
		RateInterval:  time.Millisecond, // tighten for tests
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	}()

	products, err := client.Search(context.Background(), SearchRequest{Keyword: "wireless earbuds"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(products) < 10 {
		t.Fatalf("got %d products, want >= 10", len(products))
	}
	for i, p := range products {
		if p.PriceCNYCents <= 0 {
			t.Fatalf("product %d: price not populated: %+v", i, p)
		}
		if p.MOQ <= 0 {
			t.Fatalf("product %d: MOQ not populated: %+v", i, p)
		}
		if p.SupplierRating <= 0 {
			t.Fatalf("product %d: supplier rating not populated: %+v", i, p)
		}
		if p.Source != Source1688 {
			t.Fatalf("product %d: source = %q, want %q", i, p.Source, Source1688)
		}
		if p.FetchedAt.IsZero() {
			t.Fatalf("product %d: fetched_at not populated", i)
		}
	}
}

func TestAdapter1688_RateLimitedReturns429Sentinel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       server.URL,
		SessionCookie: "session=test",
		RateInterval:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	_, err = client.Search(context.Background(), SearchRequest{Keyword: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, Err1688RateLimited) {
		t.Fatalf("error not wrapping Err1688RateLimited: %v", err)
	}
}

func TestAdapter1688_RejectsEmptyKeyword(t *testing.T) {
	t.Parallel()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       "http://test",
		SessionCookie: "session=test",
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	_, err = client.Search(context.Background(), SearchRequest{Keyword: "  "})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("error = %v, want ErrInvalidQuery", err)
	}
}

func TestAdapter1688_RequiresSessionCookie(t *testing.T) {
	t.Parallel()

	_, err := New1688Client(nil, Config1688{
		BaseURL: "http://test",
	})
	if !errors.Is(err, ErrAdapterUnconfigured) {
		t.Fatalf("error = %v, want ErrAdapterUnconfigured", err)
	}
}

func TestAdapter1688_RateLimitEnforcesInterval(t *testing.T) {
	t.Parallel()

	var hitCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(search1688Response{Products: []product1688JSON{{ID: "ok"}}})
	}))
	defer server.Close()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       server.URL,
		SessionCookie: "session=test",
		RateInterval:  60 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	start := time.Now()
	for i := 0; i < 3; i++ {
		_, err := client.Search(context.Background(), SearchRequest{Keyword: "earbuds"})
		if err != nil {
			t.Fatalf("Search %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Fatalf("rate limit not enforced: 3 calls in %s (want >= 100ms gap accumulation)", elapsed)
	}
	if hitCount.Load() != 3 {
		t.Fatalf("hit count = %d, want 3", hitCount.Load())
	}
}

func TestAdapter1688_RateWaitHonoursContextCancel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(search1688Response{Products: []product1688JSON{{ID: "ok"}}})
	}))
	defer server.Close()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       server.URL,
		SessionCookie: "session=test",
		RateInterval:  10 * time.Second, // ensure waitRate sleeps
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	if _, err := client.Search(context.Background(), SearchRequest{Keyword: "x"}); err != nil {
		t.Fatalf("first search: %v", err)
	}
	// Second call: cancel context before rate-window expires.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = client.Search(ctx, SearchRequest{Keyword: "x"})
	if err == nil {
		t.Fatal("expected ctx.Err")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
}

func TestAdapter1688_ProductDetailReturnsProduct(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/detail") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(product1688JSON{
			ID:               "abc",
			Title:            "Single Earbud",
			PriceCNY:         12.50,
			MOQ:              30,
			LeadTimeDays:     14,
			SupplierID:       "sup-x",
			SupplierName:     "Test Co",
			SupplierRating:   4.5,
			SupplierVerified: true,
			ReviewCount:      120,
		})
	}))
	defer server.Close()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       server.URL,
		SessionCookie: "session=test",
		RateInterval:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	p, err := client.ProductDetail(context.Background(), ProductDetailRequest{ExternalID: "abc"})
	if err != nil {
		t.Fatalf("ProductDetail: %v", err)
	}
	if p.ExternalID != "abc" {
		t.Fatalf("external_id = %q", p.ExternalID)
	}
	if p.PriceCNYCents != 1250 {
		t.Fatalf("price_cny_cents = %d, want 1250", p.PriceCNYCents)
	}
	if p.SupplierRating != 4.5 {
		t.Fatalf("rating = %v", p.SupplierRating)
	}
}

func TestAdapter1688_ProductDetailRejectsEmptyID(t *testing.T) {
	t.Parallel()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       "http://x",
		SessionCookie: "session=test",
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	_, err = client.ProductDetail(context.Background(), ProductDetailRequest{ExternalID: ""})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("error = %v, want ErrInvalidQuery", err)
	}
}

func TestAdapter1688_ProductDetailRateLimitedReturnsSentinel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       server.URL,
		SessionCookie: "session=test",
		RateInterval:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	_, err = client.ProductDetail(context.Background(), ProductDetailRequest{ExternalID: "abc"})
	if !errors.Is(err, Err1688RateLimited) {
		t.Fatalf("error = %v, want Err1688RateLimited", err)
	}
}

func TestAdapter1688_SearchPropagates500ErrorBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       server.URL,
		SessionCookie: "session=test",
		RateInterval:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	_, err = client.Search(context.Background(), SearchRequest{Keyword: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v, want contain status 500", err)
	}
}

func TestAdapter1688_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       "http://x",
		SessionCookie: "session=test",
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatalf("close 1: %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatalf("close 2 (idempotent): %v", err)
	}
}

func TestAdapter1688_SourceConstantIs1688(t *testing.T) {
	t.Parallel()

	client, err := New1688Client(nil, Config1688{
		BaseURL:       "http://x",
		SessionCookie: "session=test",
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()
	if got := client.Source(); got != Source1688 {
		t.Fatalf("Source = %q, want %q", got, Source1688)
	}
}

func TestMaxResultsOrDefaultClamps(t *testing.T) {
	t.Parallel()

	if got := maxResultsOrDefault(0); got != 50 {
		t.Fatalf("maxResultsOrDefault(0) = %d, want 50", got)
	}
	if got := maxResultsOrDefault(999); got != 200 {
		t.Fatalf("maxResultsOrDefault(999) = %d, want 200 (clamped)", got)
	}
	if got := maxResultsOrDefault(50); got != 50 {
		t.Fatalf("maxResultsOrDefault(50) = %d, want 50", got)
	}
}

func TestURLEncodeHandlesUnicode(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"hello world": "hello+world",
		"abc-_.~":     "abc-_.~",
	}
	for in, want := range cases {
		if got := urlEncode(in); got != want {
			t.Fatalf("urlEncode(%q) = %q, want %q", in, got, want)
		}
	}
	if got := urlEncode("耳机"); !strings.HasPrefix(got, "%E8") {
		t.Fatalf("urlEncode(unicode) = %q, want %%E8 prefix", got)
	}
}

// newCassette1688Server returns an httptest.Server replaying the
// supplied product fixtures as the JSON shape Client1688 expects.
func newCassette1688Server(t *testing.T, fixtures []product1688JSON) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/search") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(search1688Response{Products: fixtures}); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}))
}

// fixture1688Earbuds returns 12 deterministic test products covering
// the EC-1-1 acceptance criteria (>=10 products with price + MOQ +
// supplier rating populated).
func fixture1688Earbuds() []product1688JSON {
	out := make([]product1688JSON, 0, 12)
	for i := 0; i < 12; i++ {
		out = append(out, product1688JSON{
			ID:               "earbud-" + string(rune('A'+i)),
			Title:            "Wireless Earbuds Model " + string(rune('A'+i)),
			Category:         "electronics",
			SubCategory:      "audio",
			PriceCNY:         15.0 + float64(i)*2,
			MOQ:              10 + i*5,
			LeadTimeDays:     5 + i,
			SupplierID:       "sup-" + string(rune('A'+i)),
			SupplierName:     "Earbud Supplier " + string(rune('A'+i)),
			SupplierRating:   4.0 + (float64(i%5) * 0.1),
			SupplierVerified: i%2 == 0,
			ReviewCount:      50 + i*30,
			MonthlySales:     100 + i*50,
			URL:              "https://detail.1688.com/offer/earbud-" + string(rune('A'+i)) + ".html",
		})
	}
	return out
}
