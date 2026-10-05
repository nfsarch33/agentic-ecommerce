package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
)

// The digest is DETERMINISTIC money math on the API's decimal strings: no
// float ever touches a total, and the rendering is byte-stable. The
// reconciliation row cross-checks the computed gross against the store's
// own sales report to the cent.
//
// MUTANT: drop the refund summation from collectDigest and
// TestCollectDigestNumbersAndReconciliation goes red — refunded/net are
// wrong the moment any order carries a refund.

func TestParseCents(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"45.00", 4500, false},
		{"0", 0, false},
		{"7.5", 750, false},
		{"-12.50", -1250, false},
		{"12.345", 0, true},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := parseCents(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("parseCents(%q) must error, got %d", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("parseCents(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}

func TestRenderDigestGolden(t *testing.T) {
	t.Parallel()
	got := renderDigest(Digest{
		Date: "2026-10-04", Orders: 3, GrossCents: 12500, RefundedCents: 2500, NetCents: 10000,
		Reviews: 2, ReviewsAvailable: true, HealthOK: true,
		ReportGrossCents: 12500, ReportAvailable: true, Reconciled: true,
	})
	want := `OPS DIGEST 2026-10-04
orders:            3
gross:             $125.00
refunded:          $25.00
net:               $100.00
reviews:           2
site-health:       ok
woo-report:        $125.00 (reconciled)
`
	if got != want {
		t.Fatalf("render mismatch:\n%s\nwant:\n%s", got, want)
	}
}

// A fixed instant on a Melbourne October day: AEDT (UTC+11). 2026-10-05
// 08:00 AEDT = 2026-10-04 21:00 UTC; the digest day is 2026-10-04 local.
func TestDigestWindowPreviousShopLocalDay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC)
	start, end, date := digestWindow(now)
	if date != "2026-10-04" {
		t.Fatalf("date = %s, want 2026-10-04", date)
	}
	// The digest day is the 2026-10-04 Melbourne LOCAL day: start at its
	// midnight (still AEST +10 — DST began 02:00 that morning) and END at
	// the NEXT local midnight (AEDT +11), because the local day is 23h on
	// a spring-forward. A fixed +24h end would land at 2026-10-05 01:00
	// local and double-count that hour in both digests.
	if got := start.UTC().Format(time.RFC3339); got != "2026-10-03T14:00:00Z" {
		t.Fatalf("start = %s", got)
	}
	if got := end.UTC().Format(time.RFC3339); got != "2026-10-04T13:00:00Z" {
		t.Fatalf("end = %s", got)
	}
}

// stubQueries records the raw query each endpoint served, so the tests
// assert the WINDOW the digest actually asked for.
type stubQueries struct {
	reviews url.Values
	sales   url.Values
}

func wooStub(t *testing.T, ordersJSON, reviewsJSON, salesJSON string, health int) (*httptest.Server, *stubQueries) {
	t.Helper()
	served := &stubQueries{}
	mux := http.NewServeMux()
	mux.HandleFunc("/wp-json/wc/v3/orders", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ordersJSON))
	})
	mux.HandleFunc("/wp-json/wc/v3/products/reviews", func(w http.ResponseWriter, r *http.Request) {
		served.reviews = r.URL.Query()
		if reviewsJSON == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reviewsJSON))
	})
	mux.HandleFunc("/wp-json/wc/v3/reports/sales", func(w http.ResponseWriter, r *http.Request) {
		served.sales = r.URL.Query()
		if salesJSON == "" {
			http.NotFound(w, nil) //nolint:gosec // test stub
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(salesJSON))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(health)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, served
}

func TestCollectDigestNumbersAndReconciliation(t *testing.T) {
	t.Parallel()
	orders := `[
		{"id":1,"total":"45.00","refunds":[{"refund_id":9,"total":"-5.00"}]},
		{"id":2,"total":"80.00"},
		{"id":3,"total":"0.00","refunds":[{"refund_id":10,"total":"-20.00"}]}
	]`
	server, served := wooStub(t, orders, `[{"reviewer":"ada"},{"reviewer":"bob"}]`,
		`{"total_sales":"125.00"}`, http.StatusOK)

	now := time.Date(2026, 10, 5, 7, 0, 0, 0, time.UTC)
	d, err := collectDigest(context.Background(),
		woocommerce.NewClient(woocommerce.Config{BaseURL: server.URL}, server.Client()),
		server.Client(), server.URL, now)
	if err != nil {
		t.Fatalf("collectDigest: %v", err)
	}
	// The reviews count must be the WINDOW's reviews and the sales report
	// must be the DAY's — assert the queries, not just the outputs.
	// now = Oct 5 07:00Z = Oct 5 18:00 AEDT -> digest day Oct 4: window
	// [Oct 4 00:00 AEST, Oct 5 00:00 AEDT) = [Oct 3 14:00Z, Oct 4 13:00Z).
	if got := served.reviews.Get("after"); got != "2026-10-03T14:00:00Z" || served.reviews.Get("before") != "2026-10-04T13:00:00Z" {
		t.Fatalf("reviews query window: after=%s before=%s", served.reviews.Get("after"), served.reviews.Get("before"))
	}
	if served.sales.Get("date_min") != "2026-10-04" || served.sales.Get("date_max") != "2026-10-04" {
		t.Fatalf("sales query day: date_min=%s date_max=%s", served.sales.Get("date_min"), served.sales.Get("date_max"))
	}
	if d.Orders != 3 || d.GrossCents != 12500 || d.RefundedCents != 2500 || d.NetCents != 10000 {
		t.Fatalf("numbers: orders=%d gross=%d refunded=%d net=%d", d.Orders, d.GrossCents, d.RefundedCents, d.NetCents)
	}
	if d.Reviews != 2 || !d.HealthOK {
		t.Fatalf("reviews=%d health=%v", d.Reviews, d.HealthOK)
	}
	if !d.ReportAvailable || !d.Reconciled {
		t.Fatalf("report available=%v reconciled=%v (gross %d vs report %d)", d.ReportAvailable, d.Reconciled, d.GrossCents, d.ReportGrossCents)
	}
}

func TestCollectDigestFlagsDifferingReport(t *testing.T) {
	t.Parallel()
	orders := `[{"id":1,"total":"45.00"}]`
	server, _ := wooStub(t, orders, `[]`, `{"total_sales":"44.00"}`, http.StatusOK)

	d, err := collectDigest(context.Background(),
		woocommerce.NewClient(woocommerce.Config{BaseURL: server.URL}, server.Client()),
		server.Client(), server.URL, time.Date(2026, 10, 5, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("collectDigest: %v", err)
	}
	if d.ReportAvailable && d.Reconciled {
		t.Fatal("a report differing from the computed gross must not read as reconciled")
	}
}

func TestCollectDigestWithoutReportEndpoint(t *testing.T) {
	t.Parallel()
	orders := `[{"id":1,"total":"10.00"}]`
	server, _ := wooStub(t, orders, `[]`, "", http.StatusOK)

	d, err := collectDigest(context.Background(),
		woocommerce.NewClient(woocommerce.Config{BaseURL: server.URL}, server.Client()),
		server.Client(), server.URL, time.Date(2026, 10, 5, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("collectDigest: %v", err)
	}
	if d.ReportAvailable {
		t.Fatal("a store without the sales report endpoint must render unavailable, not reconciled")
	}
	if got := renderDigest(d); !strings.Contains(got, "unavailable (cannot reconcile)") {
		t.Fatalf("render must name the missing report:\n%s", got)
	}
}

func TestCollectDigestReviewsUnavailableRendersNotZero(t *testing.T) {
	t.Parallel()
	orders := `[{"id":1,"total":"10.00"}]`
	server, _ := wooStub(t, orders, "", "", http.StatusOK) // reviews handler 500s

	d, err := collectDigest(context.Background(),
		woocommerce.NewClient(woocommerce.Config{BaseURL: server.URL}, server.Client()),
		server.Client(), server.URL, time.Date(2026, 10, 5, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("collectDigest: %v", err)
	}
	if d.ReviewsAvailable {
		t.Fatal("a reviews endpoint error must not read as available")
	}
	if got := renderDigest(d); !strings.Contains(got, "reviews:           unavailable") {
		t.Fatalf("render must say unavailable, not a confident zero:\n%s", got)
	}
}
