// ops-digest renders the daily shop operations digest: yesterday's orders,
// refunds, revenue and product reviews plus the store's health, computed
// deterministically from the WooCommerce REST API (no model; money is
// parsed from the API's decimal strings directly into integer cents — no
// float ever touches a total). The digest is written to the runs directory
// and optionally posted to Slack; the WooCommerce sales report is fetched
// alongside so the numbers can be reconciled against the store's own
// reporting without a second tool.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
)

// Digest is one day's computed numbers. Every money field is integer cents
// so the rendering is byte-stable and a diff between two digests means the
// shop moved, not floating point.
type Digest struct {
	Date          string // the digest's shop-local day, YYYY-MM-DD
	Orders        int
	GrossCents    int64
	RefundedCents int64
	NetCents      int64
	Reviews       int
	// ReviewsAvailable is false when the reviews endpoint errored: the
	// render says unavailable rather than a confident zero.
	ReviewsAvailable bool
	HealthOK         bool
	// ReportGrossCents is the WooCommerce sales report's total_sales for
	// the same day, when the endpoint answers; Reconciled says whether it
	// matches the computed gross to the cent.
	ReportGrossCents int64
	ReportAvailable  bool
	Reconciled       bool
}

// renderDigest formats the digest deterministically: fixed field order and
// padding — byte-identical output for identical inputs.
func renderDigest(d Digest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "OPS DIGEST %s\n", d.Date)
	fmt.Fprintf(&b, "orders:            %d\n", d.Orders)
	fmt.Fprintf(&b, "gross:             %s\n", cents(d.GrossCents))
	fmt.Fprintf(&b, "refunded:          %s\n", cents(d.RefundedCents))
	fmt.Fprintf(&b, "net:               %s\n", cents(d.NetCents))
	if d.ReviewsAvailable {
		fmt.Fprintf(&b, "reviews:           %d\n", d.Reviews)
	} else {
		b.WriteString("reviews:           unavailable\n")
	}
	fmt.Fprintf(&b, "site-health:       %s\n", map[bool]string{true: "ok", false: "DOWN"}[d.HealthOK])
	switch {
	case !d.ReportAvailable:
		b.WriteString("woo-report:        unavailable (cannot reconcile)\n")
	case d.Reconciled:
		fmt.Fprintf(&b, "woo-report:        %s (reconciled)\n", cents(d.ReportGrossCents))
	default:
		fmt.Fprintf(&b, "woo-report:        %s (DIFFERS from gross %s)\n", cents(d.ReportGrossCents), cents(d.GrossCents))
	}
	return b.String()
}

func cents(n int64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	return fmt.Sprintf("%s$%d.%02d", sign, n/100, n%100)
}

// parseCents reads a WooCommerce money string ("45.00", "-12.50", "0")
// exactly into integer cents. A malformed string returns an error: a
// digest that silently treats unparseable money as zero is worse than one
// that refuses to render.
func parseCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty money string")
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	whole, frac := s, "00"
	if i := strings.IndexByte(s, '.'); i >= 0 {
		whole, frac = s[:i], s[i+1:]
	}
	if whole == "" {
		whole = "0"
	}
	switch len(frac) {
	case 0:
		frac = "00"
	case 1:
		frac += "0"
	case 2:
	default:
		return 0, fmt.Errorf("money %q has more than two decimals", s)
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money %q: %w", s, err)
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money %q: %w", s, err)
	}
	v := w*100 + f
	if neg {
		v = -v
	}
	return v, nil
}

// melbourne is the shop's reporting timezone: the digest day boundary and
// the 08:00 delivery both run on shop-local time.
var melbourne = mustTZ("Australia/Melbourne")

func mustTZ(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("ops-digest: cannot load " + name + " (tzdata missing — the day boundary would silently move to UTC)")
	}
	return loc
}

// digestWindow returns [start, end) for the shop-local day BEFORE the
// given instant — the digest at 08:00 covers the completed day.
func digestWindow(now time.Time) (time.Time, time.Time, string) {
	local := now.In(melbourne)
	day := local.AddDate(0, 0, -1)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, melbourne)
	// The next LOCAL midnight, not +24h: on DST-change days the local day
	// is 23h or 25h, and a fixed 24h span would double-count the first
	// hour of the next day (or drop the last hour of a 25h day).
	end := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, melbourne)
	return start, end, day.Format("2006-01-02")
}

// review is the slice of the Woo product-review payload the digest counts.
type review struct {
	Reviewer string `json:"reviewer"`
}

// collectDigest pulls the day's numbers. Refunds sum the orders' refund
// sets (Woo keeps the order total at its original value; the refunds array
// carries what actually went back, negative).
func collectDigest(ctx context.Context, client woocommerce.Client, httpClient *http.Client, baseURL string, now time.Time) (Digest, error) {
	start, end, date := digestWindow(now)
	// Page until a short page: a single 100-slot request silently
	// truncates every total on a busy day, and a digest with wrong
	// numbers is worse than no digest.
	var orders []woocommerce.Order
	for page := 1; page <= 10; page++ {
		batch, err := client.ListOrders(ctx, woocommerce.ListOptions{
			PerPage: 100,
			Page:    page,
			After:   start.UTC().Format(time.RFC3339),
			Before:  end.UTC().Format(time.RFC3339),
		})
		if err != nil {
			return Digest{}, fmt.Errorf("list orders page %d: %w", page, err)
		}
		orders = append(orders, batch...)
		if len(batch) < 100 {
			break
		}
		if page == 10 {
			return Digest{}, fmt.Errorf("over %d orders in one day: refusing to render a truncated digest", len(orders))
		}
	}

	d := Digest{Date: date, Orders: len(orders)}
	for _, o := range orders {
		gross, err := parseCents(o.Total)
		if err != nil {
			return Digest{}, fmt.Errorf("order %d total: %w", o.ID, err)
		}
		d.GrossCents += gross
		for _, r := range o.Refunds {
			refunded, err := parseCents(r.Total)
			if err != nil {
				return Digest{}, fmt.Errorf("order %d refund %d: %w", o.ID, r.RefundID, err)
			}
			if refunded < 0 {
				refunded = -refunded
			}
			d.RefundedCents += refunded
		}
	}
	d.NetCents = d.GrossCents - d.RefundedCents

	reviews, reviewsErr := listReviews(ctx, httpClient, baseURL, start, end)
	d.ReviewsAvailable = reviewsErr == nil
	d.Reviews = len(reviews)
	d.HealthOK = probeHealth(ctx, httpClient, baseURL)

	if report, ok := fetchSalesTotal(ctx, httpClient, baseURL, date); ok {
		d.ReportAvailable = true
		d.ReportGrossCents = report
		d.Reconciled = d.ReportGrossCents == d.GrossCents
	}
	return d, nil
}

// listReviews fetches the product reviews INSIDE the digest's window (the
// count is "yesterday's reviews", not "all reviews"): after/before take
// ISO-8601 dates exactly like the orders listing's.
func listReviews(ctx context.Context, httpClient *http.Client, baseURL string, start, end time.Time) ([]review, error) {
	endpoint, err := url.JoinPath(baseURL, "wp-json/wc/v3/products/reviews")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("per_page", "100")
	q.Set("after", start.UTC().Format(time.RFC3339))
	q.Set("before", end.UTC().Format(time.RFC3339))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reviews: HTTP %d", resp.StatusCode)
	}
	var reviews []review
	return reviews, json.NewDecoder(resp.Body).Decode(&reviews)
}

// probeHealth checks the store's own healthz (the fixture store proxy
// exposes one; a store without it is reported DOWN, not assumed ok).
func probeHealth(ctx context.Context, httpClient *http.Client, baseURL string) bool {
	endpoint, err := url.JoinPath(baseURL, "healthz")
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// fetchSalesTotal reads the WooCommerce sales report for the window. The
// classic reports endpoint is best-effort: a Woo build without it returns
// !ok and the digest prints "unavailable (cannot reconcile)" instead of
// failing the day's numbers.
func fetchSalesTotal(ctx context.Context, httpClient *http.Client, baseURL string, day string) (int64, bool) {
	endpoint, err := url.JoinPath(baseURL, "wp-json/wc/v3/reports/sales")
	if err != nil {
		return 0, false
	}
	// The endpoint takes date_min/date_max as SHOP-LOCAL YYYY-MM-DD; a UTC
	// rendering of local midnight names the previous calendar day, and
	// unknown params make it answer its default period — the reconciliation
	// then compares against the wrong window. One day: min == max == day.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		endpoint+"?date_min="+url.QueryEscape(day)+"&date_max="+url.QueryEscape(day), nil)
	if err != nil {
		return 0, false
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	var report struct {
		TotalSales string `json:"total_sales"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return 0, false
	}
	total, err := parseCents(report.TotalSales)
	if err != nil {
		return 0, false
	}
	return total, true
}
