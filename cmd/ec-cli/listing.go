package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

// listing.go — the v1 listing flow's executable form (v18960-141):
// `ec-cli listing check --file draft.json` runs the deterministic
// constraint gate on a model-drafted listing; `ec-cli listing publish
// --file draft.json` re-runs the gate, then creates the product through
// the WooCommerce adapter behind an exactly-once SKU guard and verifies
// the store round-trip. Store credentials ride env vars
// (EC_STORE_URL, EC_STORE_KEY, EC_STORE_SECRET) — never argv, never the
// draft file. The draft contract mirrors the drafting prompt: title
// <=140, exactly 5 bullets <=120 each, description of exactly 2
// non-empty paragraphs, search keywords <=200, plus the commerce fields.

type listingDraft struct {
	SKU            string   `json:"sku"`
	Title          string   `json:"title"`
	Bullets        []string `json:"bullets"`
	Description    string   `json:"description"`
	SearchKeywords string   `json:"search_keywords"`
	PriceCents     int      `json:"price_cents"`
	Currency       string   `json:"currency"`
	Stock          int      `json:"stock"`
	ImageSrc       string   `json:"image_src"`
	ImageAlt       string   `json:"image_alt"`
}

func runListing(ctx context.Context, args []string, deps appDeps) int {
	if len(args) < 1 || (args[0] != "check" && args[0] != "publish") {
		fmt.Fprintln(deps.stderr, "ec-cli listing: subcommand required (check | publish)")
		return 2
	}
	fs := flag.NewFlagSet("listing "+args[0], flag.ContinueOnError)
	fs.SetOutput(deps.stderr)
	file := fs.String("file", "", "listing draft JSON (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(deps.stderr, "ec-cli listing: --file is required")
		return 2
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli listing: read %s: %v\n", *file, err)
		return 2
	}
	var draft listingDraft
	if err := json.Unmarshal(raw, &draft); err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli listing: parse %s: %v\n", *file, err)
		return 2
	}
	if !checkListing(draft, deps.stdout) {
		return 1
	}
	if args[0] == "check" {
		return 0
	}
	return publishListing(ctx, draft, deps)
}

// checkListing prints one PASS/FAIL line per constraint and reports
// whether the draft is publishable. Deterministic by construction: no
// model, no network.
func checkListing(d listingDraft, out writer) bool {
	ok := true
	fail := func(format string, a ...any) {
		fmt.Fprintf(out, "FAIL "+format+"\n", a...)
		ok = false
	}
	pass := func(name string) { fmt.Fprintf(out, "PASS %s\n", name) }

	if strings.TrimSpace(d.SKU) == "" {
		fail("sku: empty")
	} else {
		pass("sku")
	}
	if t := d.Title; strings.TrimSpace(t) == "" || len(t) > 140 {
		fail("title: must be non-empty and <=140 chars (got %d)", len(t))
	} else {
		pass("title")
	}
	if len(d.Bullets) != 5 {
		fail("bullets: exactly 5 required (got %d)", len(d.Bullets))
	} else {
		tooLong := 0
		for i, b := range d.Bullets {
			if strings.TrimSpace(b) == "" || len(b) > 120 {
				tooLong = i + 1
			}
		}
		if tooLong != 0 {
			fail("bullets: bullet %d must be non-empty and <=120 chars", tooLong)
		} else {
			pass("bullets")
		}
	}
	if paras := nonEmptyParas(d.Description); len(paras) != 2 {
		fail("description: exactly 2 non-empty paragraphs required (got %d)", len(paras))
	} else {
		pass("description")
	}
	if len(d.SearchKeywords) > 200 {
		fail("search_keywords: <=200 chars required (got %d)", len(d.SearchKeywords))
	} else {
		pass("search_keywords")
	}
	if d.PriceCents <= 0 {
		fail("price_cents: must be positive (got %d)", d.PriceCents)
	} else if _, err := catalog.NewMoney(d.PriceCents, d.Currency); err != nil {
		fail("currency: %v", err)
	} else {
		pass("price")
	}
	if d.Stock < 0 {
		fail("stock: must be non-negative (got %d)", d.Stock)
	} else {
		pass("stock")
	}
	if u, err := url.Parse(d.ImageSrc); err != nil || !isStoreImageURL(u) {
		fail("image_src: must be https, or http on a loopback host for sandbox stores (got %q)", d.ImageSrc)
	} else {
		pass("image_src")
	}
	if strings.TrimSpace(d.ImageAlt) == "" {
		fmt.Fprintln(out, "WARN image_alt: empty (accessibility: alt text is expected on listings)")
	}
	return ok
}

func nonEmptyParas(s string) []string {
	var paras []string
	for _, p := range strings.Split(strings.TrimSpace(s), "\n\n") {
		if strings.TrimSpace(p) != "" {
			paras = append(paras, p)
		}
	}
	return paras
}

// isStoreImageURL accepts https anywhere (production stores) and plain
// http only on loopback or RFC1918 private hosts (sandbox stores the SOP
// boots locally; the store container fetches the image itself, and its
// own address is private by construction).
func isStoreImageURL(u *url.URL) bool {
	if u.Scheme == "https" && u.Host != "" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasPrefix(host, "127.") {
		return true
	}
	for _, prefix := range []string{"10.", "192.168.", "172."} {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}
	return false
}

func publishListing(ctx context.Context, d listingDraft, deps appDeps) int {
	storeURL := deps.getenv("EC_STORE_URL")
	key := deps.getenv("EC_STORE_KEY")
	secret := deps.getenv("EC_STORE_SECRET")
	if storeURL == "" || key == "" || secret == "" {
		fmt.Fprintln(deps.stderr, "ec-cli listing publish: EC_STORE_URL, EC_STORE_KEY and EC_STORE_SECRET are required")
		return 2
	}
	money, err := catalog.NewMoney(d.PriceCents, d.Currency)
	if err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli listing publish: price: %v\n", err)
		return 1
	}
	product, err := catalog.NewProduct(catalog.ProductInput{
		SKU:         d.SKU,
		Title:       d.Title,
		Description: d.Title + " — " + strings.Join(d.Bullets, " "),
		Price:       money,
		Stock:       d.Stock,
		Status:      catalog.StatusActive,
		Images:      []catalog.Image{{URL: d.ImageSrc, Alt: d.ImageAlt}},
	})
	if err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli listing publish: build product: %v\n", err)
		return 1
	}
	client := woocommerce.NewClient(woocommerce.Config{BaseURL: storeURL, ConsumerKey: key, ConsumerSecret: secret}, &http.Client{Timeout: 30 * time.Second})
	existing, err := client.FindProductBySKU(ctx, product.SKU())
	if err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli listing publish: store lookup failed: %v\n", err)
		return 1
	}
	if existing != nil {
		fmt.Fprintf(deps.stderr, "ec-cli listing publish: sku %s is already listed as id %d — exactly-once refuses to duplicate (updates go through the publish gate)\n", product.SKU(), existing.ID)
		return 1
	}
	if err := client.UpsertProduct(ctx, product); err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli listing publish: create failed: %v\n", err)
		return 1
	}
	stored, err := client.FindProductBySKU(ctx, product.SKU())
	if err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli listing publish: verify lookup failed: %v\n", err)
		return 1
	}
	if stored == nil {
		fmt.Fprintf(deps.stderr, "ec-cli listing publish: sku %s not readable back after create\n", product.SKU())
		return 1
	}
	fmt.Fprintf(deps.stdout, "LISTED id=%d sku=%s status=%s price=%s images=%d\n", stored.ID, stored.SKU, stored.Status, stored.Regular, len(stored.Images))
	return 0
}
