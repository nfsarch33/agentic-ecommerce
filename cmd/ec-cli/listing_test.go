package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
// sourceImageBytes is the draft's source image the fake store serves
// at /img/source.png; the media step fetches it, the sidecar edits it.
var sourceImageBytes = []byte("source-image-bytes-for-the-organizer")

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
		case r.Method == http.MethodGet && r.URL.Path == "/img/source.png":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(sourceImageBytes)
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

// fakeMediaSidecar serves the spec 2.3 /v1/process endpoint for the step
// tests: mode "honest" reports the real sha256 of its deterministic
// output, "lying" reports another checksum, and it records every hit.
func fakeMediaSidecar(t *testing.T, mode string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	jobIDs := &[]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/process" {
			t.Fatalf("sidecar saw %s %s, want POST /v1/process", r.Method, r.URL.Path)
		}
		var req struct {
			JobID    string   `json:"job_id"`
			ImageB64 string   `json:"image_b64"`
			Ops      []string `json:"ops"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("sidecar decode: %v", err)
		}
		for _, op := range req.Ops {
			if op != "bg" && op != "upscale" {
				t.Fatalf("sidecar got op %q, want only bg and upscale", op)
			}
		}
		mu.Lock()
		*jobIDs = append(*jobIDs, req.JobID)
		mu.Unlock()
		out := []byte("edited:" + req.JobID)
		sum := sha256.Sum256(out)
		if mode == "lying" {
			sum = sha256.Sum256([]byte("other-bytes-entirely"))
		}
		_ = json.NewEncoder(w).Encode(struct {
			OutB64 string  `json:"out_b64"`
			SHA256 string  `json:"sha256"`
			Secs   float64 `json:"secs"`
			Cached bool    `json:"cached"`
		}{OutB64: base64.StdEncoding.EncodeToString(out), SHA256: hex.EncodeToString(sum[:]), Secs: 0.01})
	}))
	t.Cleanup(server.Close)
	return server, jobIDs
}

func draftWithServedImage(server *httptest.Server) listingDraft {
	draft := validDraft()
	draft.ImageSrc = server.URL + "/img/source.png"
	return draft
}

func publishEnv(storeURL, mediaURL string) func(string) string {
	env := map[string]string{"EC_STORE_URL": storeURL, "EC_STORE_KEY": "ck", "EC_STORE_SECRET": "cs"}
	if mediaURL != "" {
		env["EC_MEDIA_URL"] = mediaURL
	}
	return func(k string) string { return env[k] }
}

// S6 acceptance 1: the sidecar returns bytes whose sha256 differs from
// the reported one -> the edited image is rejected, the source image is
// published and the record says image_edited false.
func TestImageStep_MismatchFallsBackToSource(t *testing.T) {
	store, stored := fakeStore(t, nil)
	sidecar, jobIDs := fakeMediaSidecar(t, "lying")
	var out strings.Builder
	deps := appDeps{stdout: &out, stderr: &out, getenv: publishEnv(store.URL, sidecar.URL)}
	rc := runListing(context.Background(), []string{"publish", "--file", writeDraft(t, draftWithServedImage(store))}, deps)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0; output:\n%s", rc, out.String())
	}
	if len(*jobIDs) != 1 {
		t.Fatalf("sidecar job ids = %v, want exactly one call", *jobIDs)
	}
	if !strings.Contains(out.String(), "IMAGE edited=false") || !strings.Contains(out.String(), "checksum-mismatch") {
		t.Fatalf("output must record the rejected edit:\n%s", out.String())
	}
	if len(*stored) != 1 || (*stored)[0].Images[0].Src != store.URL+"/img/source.png" {
		t.Fatalf("stored images = %#v, want the source image", (*stored)[0].Images)
	}
}

// S6 acceptance 3: the sidecar is stopped -> the listing still publishes
// with the source image, quickly enough to hold the S9 budget.
func TestImageStep_OutageStillListsWithSource(t *testing.T) {
	store, stored := fakeStore(t, nil)
	// port 1 on loopback: refused immediately, nothing is listening there
	var out strings.Builder
	deps := appDeps{stdout: &out, stderr: &out, getenv: publishEnv(store.URL, "http://127.0.0.1:1")}
	start := time.Now()
	rc := runListing(context.Background(), []string{"publish", "--file", writeDraft(t, draftWithServedImage(store))}, deps)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0; output:\n%s", rc, out.String())
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("the outage path took %s, over the step ceiling plus publish (S9 holds two minutes)", elapsed)
	}
	if !strings.Contains(out.String(), "IMAGE edited=false") || !strings.Contains(out.String(), "media-outage") {
		t.Fatalf("output must record the outage fallback:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "LISTED id=42") {
		t.Fatalf("the listing must still list:\n%s", out.String())
	}
	if len(*stored) != 1 || (*stored)[0].Images[0].Src != store.URL+"/img/source.png" {
		t.Fatalf("stored images = %#v, want the source image", (*stored)[0].Images)
	}
}

// S6 happy path: an honest sidecar -> the edited image is published (a
// data URL built from the verified bytes) and the record names its
// checksum and cache state; the job id is stable for the same draft.
func TestImageStep_EditedImagePublishesWithChecksum(t *testing.T) {
	store, stored := fakeStore(t, nil)
	sidecar, jobIDs := fakeMediaSidecar(t, "honest")
	var out strings.Builder
	deps := appDeps{stdout: &out, stderr: &out, getenv: publishEnv(store.URL, sidecar.URL)}
	rc := runListing(context.Background(), []string{"publish", "--file", writeDraft(t, draftWithServedImage(store))}, deps)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0; output:\n%s", rc, out.String())
	}
	if len(*jobIDs) != 1 {
		t.Fatalf("sidecar job ids = %v, want exactly one call", *jobIDs)
	}
	wantSum := sha256.Sum256([]byte("edited:" + (*jobIDs)[0]))
	if !strings.Contains(out.String(), "IMAGE edited=true sha256="+hex.EncodeToString(wantSum[:])+" cached=false") {
		t.Fatalf("output must record the verified checksum:\n%s", out.String())
	}
	if len(*stored) != 1 {
		t.Fatalf("stored %d products, want 1", len(*stored))
	}
	got := (*stored)[0].Images[0].Src
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("stored image src = %q, want a data URL built from the verified edited bytes", got)
	}
}

// The job id is derived from the SKU and the source checksum: the same
// draft replays the same id (the sidecar answers from its cache with the
// same checksum); a different source changes it.
func TestImageJobIDStableForSameDraft(t *testing.T) {
	a := imageJobID("BDO-5C-001", "aa11")
	b := imageJobID("BDO-5C-001", "aa11")
	c := imageJobID("BDO-5C-001", "bb22")
	if a == "" {
		t.Fatal("imageJobID must not be empty")
	}
	if a != b {
		t.Fatalf("same draft must replay the same job id: %q vs %q", a, b)
	}
	if a == c {
		t.Fatal("a different source image must change the job id")
	}
}
