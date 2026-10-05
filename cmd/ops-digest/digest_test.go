package main

import (
	"context"
	"net/http"
	"strings"
	"net/http/httptest"
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
		Reviews: 2, HealthOK: true,
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
	// 2026-10-04 00:00 Melbourne is still AEST (+10): DST began that morning
	// at 02:00, so the digest day's midnight is 2026-10-03 14:00 UTC. The
	// 24h span crosses the transition — the boundary takes the offset in
	// effect AT the boundary, which is what the shop's calendar did too.
	if got := start.UTC().Format(time.RFC3339); got != "2026-10-03T14:00:00Z" {
		t.Fatalf("start = %s", got)
	}
	if got := end.UTC().Format(time.RFC3339); got != "2026-10-04T14:00:00Z" {
		t.Fatalf("end = %s", got)
	}
}

func wooStub(t *testing.T, ordersJSON, reviewsJSON, salesJSON string, health int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/wp-json/wc/v3/orders", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ordersJSON))
	})
	mux.HandleFunc("/wp-json/wc/v3/products/reviews", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reviewsJSON))
	})
	mux.HandleFunc("/wp-json/wc/v3/reports/sales", func(w http.ResponseWriter, _ *http.Request) {
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
	return server
}

func TestCollectDigestNumbersAndReconciliation(t *testing.T) {
	t.Parallel()
	orders := `[
		{"id":1,"total":"45.00","refunds":[{"refund_id":9,"total":"-5.00"}]},
		{"id":2,"total":"80.00"},
		{"id":3,"total":"0.00","refunds":[{"refund_id":10,"total":"-20.00"}]}
	]`
	server := wooStub(t, orders, `[{"reviewer":"ada"},{"reviewer":"bob"}]`,
		`{"total_sales":"125.00"}`, http.StatusOK)

	now := time.Date(2026, 10, 5, 7, 0, 0, 0, time.UTC)
	d, err := collectDigest(context.Background(),
		woocommerce.NewClient(woocommerce.Config{BaseURL: server.URL}, server.Client()),
		server.Client(), server.URL, now)
	if err != nil {
		t.Fatalf("collectDigest: %v", err)
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
	server := wooStub(t, orders, `[]`, `{"total_sales":"44.00"}`, http.StatusOK)

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
	server := wooStub(t, orders, `[]`, "", http.StatusOK)

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
