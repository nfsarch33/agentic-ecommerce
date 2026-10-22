package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
)

// The v1 listing flow's gate: every constraint row is
// deterministic — no model, no network — and publish rides the gate plus
// an exactly-once SKU guard against a fake WooCommerce store.

func validDraft() listingDraft {
	return listingDraft{
		SKU:            "bdo-5c-001",
		Title:          "Bamboo Desk Organizer 5 Compartments with Drawer, Pre-Assembled Eco Office Storage",
		Bullets:        []string{"b1", "b2", "b3", "b4", "b5"},
		Description:    "Paragraph one about the organizer.\n\nParagraph two about the desk setup.",
		SearchKeywords: "bamboo desk organizer, eco office storage",
		PriceCents:     2990,
		Currency:       "AUD",
		Stock:          25,
		ImageSrc:       "https://store.example/wp-content/uploads/product.jpg",
		ImageAlt:       "bamboo organizer on a desk",
	}
}

func writeDraft(t *testing.T, d listingDraft) string {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal draft: %v", err)
	}
	path := filepath.Join(t.TempDir(), "draft.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write draft: %v", err)
	}
	return path
}

func TestListingCheck_ValidDraftPassesEveryRow(t *testing.T) {
	var out strings.Builder
	deps := appDeps{stdout: &out, stderr: &out, getenv: func(string) string { return "" }}
	rc := runListing(context.Background(), []string{"check", "--file", writeDraft(t, validDraft())}, deps)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0; output:\n%s", rc, out.String())
	}
	for _, row := range []string{"PASS sku", "PASS title", "PASS bullets", "PASS description", "PASS search_keywords", "PASS price", "PASS stock", "PASS image_src"} {
		if !strings.Contains(out.String(), row) {
			t.Fatalf("output missing %q:\n%s", row, out.String())
		}
	}
}

func TestListingCheck_EachViolationFailsItsRow(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*listingDraft)
		want   string
	}{
		{"empty sku", func(d *listingDraft) { d.SKU = "" }, "FAIL sku"},
		{"long title", func(d *listingDraft) { d.Title = strings.Repeat("x", 141) }, "FAIL title"},
		{"four bullets", func(d *listingDraft) { d.Bullets = d.Bullets[:4] }, "FAIL bullets"},
		{"long bullet", func(d *listingDraft) { d.Bullets[2] = strings.Repeat("x", 121) }, "FAIL bullets"},
		{"three paragraphs", func(d *listingDraft) { d.Description += "\n\nthird" }, "FAIL description"},
		{"long keywords", func(d *listingDraft) { d.SearchKeywords = strings.Repeat("k", 201) }, "FAIL search_keywords"},
		{"zero price", func(d *listingDraft) { d.PriceCents = 0 }, "FAIL price_cents"},
		{"bad currency", func(d *listingDraft) { d.Currency = "AUD;" }, "FAIL currency"},
		{"negative stock", func(d *listingDraft) { d.Stock = -1 }, "FAIL stock"},
		{"plain-http remote image", func(d *listingDraft) { d.ImageSrc = "http://store.example/p.jpg" }, "FAIL image_src"},
		{"loopback http image is fine", func(d *listingDraft) { d.ImageSrc = "http://127.0.0.1:8181/wp-content/uploads/p.jpg" }, "PASS image_src"},
		{"private-range sandbox image is fine", func(d *listingDraft) { d.ImageSrc = "http://10.89.7.8/wp-content/uploads/p.jpg" }, "PASS image_src"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draft := validDraft()
			tc.mutate(&draft)
			var out strings.Builder
			deps := appDeps{stdout: &out, stderr: &out, getenv: func(string) string { return "" }}
			rc := runListing(context.Background(), []string{"check", "--file", writeDraft(t, draft)}, deps)
			wantFail := strings.HasPrefix(tc.want, "FAIL")
			if wantFail && (rc != 1 || !strings.Contains(out.String(), tc.want)) {
				t.Fatalf("rc = %d, want 1 with %q; output:\n%s", rc, tc.want, out.String())
			}
			if !wantFail && rc != 0 {
				t.Fatalf("rc = %d, want 0 (this mutation is valid); output:\n%s", rc, out.String())
			}
		})
	}
}

// fakeStore serves the two GET /products?sku= lookups and the POST create
// a publish drives, against per-test state.
func fakeStore(t *testing.T, existing *woocommerce.Product) (*httptest.Server, *[]woocommerce.Product) {
	t.Helper()
	stored := []woocommerce.Product{}
	if existing != nil {
		stored = append(stored, *existing)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products":
			sku := r.URL.Query().Get("sku")
			for _, p := range stored {
				if p.SKU == sku {
					_ = json.NewEncoder(w).Encode([]woocommerce.Product{p})
					return
				}
			}
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodPost && r.URL.Path == "/wp-json/wc/v3/products":
			var incoming woocommerce.Product
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("fake store decode: %v", err)
			}
			incoming.ID = 42
			incoming.Status = "publish"
			stored = append(stored, incoming)
			_ = json.NewEncoder(w).Encode(incoming)
		default:
			t.Fatalf("fake store saw %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return server, &stored
}

func TestListingPublish_CreatesThroughAdapterAndVerifies(t *testing.T) {
	server, stored := fakeStore(t, nil)
	var out strings.Builder
	env := map[string]string{"EC_STORE_URL": server.URL, "EC_STORE_KEY": "ck", "EC_STORE_SECRET": "cs"}
	deps := appDeps{stdout: &out, stderr: &out, getenv: func(k string) string { return env[k] }}
	rc := runListing(context.Background(), []string{"publish", "--file", writeDraft(t, validDraft())}, deps)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0; output:\n%s", rc, out.String())
	}
	if len(*stored) != 1 || (*stored)[0].SKU != "BDO-5C-001" {
		t.Fatalf("stored = %#v, want one BDO-5C-001", *stored)
	}
	if !strings.Contains(out.String(), "LISTED id=42 sku=BDO-5C-001 status=publish") {
		t.Fatalf("output missing LISTED line:\n%s", out.String())
	}
}

func TestListingPublish_RefusesDuplicateSKU(t *testing.T) {
	existing := woocommerce.Product{ID: 7, SKU: "BDO-5C-001", Name: "already there"}
	server, stored := fakeStore(t, &existing)
	var out strings.Builder
	env := map[string]string{"EC_STORE_URL": server.URL, "EC_STORE_KEY": "ck", "EC_STORE_SECRET": "cs"}
	deps := appDeps{stdout: &out, stderr: &out, getenv: func(k string) string { return env[k] }}
	rc := runListing(context.Background(), []string{"publish", "--file", writeDraft(t, validDraft())}, deps)
	if rc != 1 || !strings.Contains(out.String(), "already listed as id 7") {
		t.Fatalf("rc = %d, want 1 with exactly-once refusal; output:\n%s", rc, out.String())
	}
	if len(*stored) != 1 {
		t.Fatalf("stored = %#v, want the single pre-existing product untouched", *stored)
	}
}

func TestListingPublish_RequiresStoreEnv(t *testing.T) {
	var out strings.Builder
	deps := appDeps{stdout: &out, stderr: &out, getenv: func(string) string { return "" }}
	rc := runListing(context.Background(), []string{"publish", "--file", writeDraft(t, validDraft())}, deps)
	if rc != 2 || !strings.Contains(out.String(), "EC_STORE_URL") {
		t.Fatalf("rc = %d, want 2 naming the env vars; output:\n%s", rc, out.String())
	}
}

func TestListingPublish_GateRunsBeforeTheStore(t *testing.T) {
	server, _ := fakeStore(t, nil)
	draft := validDraft()
	draft.Bullets = draft.Bullets[:3] // the gate must refuse before any HTTP call
	var out strings.Builder
	env := map[string]string{"EC_STORE_URL": server.URL, "EC_STORE_KEY": "ck", "EC_STORE_SECRET": "cs"}
	deps := appDeps{stdout: &out, stderr: &out, getenv: func(k string) string { return env[k] }}
	rc := runListing(context.Background(), []string{"publish", "--file", writeDraft(t, draft)}, deps)
	if rc != 1 || !strings.Contains(out.String(), "FAIL bullets") {
		t.Fatalf("rc = %d, want 1 with the gate row; output:\n%s", rc, out.String())
	}
}
