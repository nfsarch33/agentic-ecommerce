//go:build fleet_listing

// package fleetlisting — the listing flow as a fleet contract: the whole
// flow, in order and end to end, as an agent runtime must solve it:
//
//	draft (AI port, cost-ledgered) → ec-cli listing check → publish →
//	second publish refused (exactly-once) → verified LISTED row.
//
// Deterministic by construction: the draft call rides a scripted generator
// through the same RecordingGenerator wrapper the runtime uses (a live
// lane swaps in the real drafting model), and the store is a fake
// WooCommerce endpoint speaking the adapter's wire shape. The gate and
// publish run the REAL ec-cli binary as a subprocess — the agent's tool
// boundary is the process, not the function.
//
// Named mutant this contract kills: "the verifier is always green" — a
// harness that publishes despite a FAIL gate verdict. The negative-control
// row feeds a corrupted draft, asserts the gate refuses, and asserts the
// store was never written; a mutant that ignores the gate turns that
// store-empty assertion red.
//
// Run explicitly: go test -tags fleet_listing ./tests/integration/fleetlisting/...
package fleetlisting_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/costledger"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// copyJSON is what the drafting model returns: ONLY the copy fields. The
// commerce fields (sku, price, stock, image) are the agent's own per the
// SOP — assembling them is the harness's job below.
const copyJSON = `{
  "title": "Bamboo Desk Organizer 5 Compartments with Drawer, Pre-Assembled Eco Office Storage",
  "bullets": ["Five fixed compartments fit pens and cards", "Drawer for small desk clutter", "Pre-assembled, no tools needed", "Heat-treated bamboo, no varnish smell", "Flat-packed recycled carton"],
  "description": "A five-compartment bamboo organizer that clears a working desk in one move.\n\nThe drawer takes the small clutter that usually migrates across the desk, and the pre-assembled body lands ready to use.",
  "search_keywords": "bamboo desk organizer, eco office storage, desk drawer organiser"
}`

type scriptedGenerator struct{ content string }

func (g scriptedGenerator) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	return port.AICompletionResponse{Content: g.content, TokensUsed: 441, TokensIn: 1820, TokensOut: 441}, nil
}

type captureRecorder struct {
	rows []costledger.Row
}

func (r *captureRecorder) Record(_ context.Context, row costledger.Row) error {
	r.rows = append(r.rows, row)
	return nil
}

// fakeStore serves the adapter's two routes against per-test state; the
// closed-over slice lets the contract assert what actually landed.
func fakeStore(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	stored := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products":
			sku := r.URL.Query().Get("sku")
			for _, p := range stored {
				if p["sku"] == sku {
					_ = json.NewEncoder(w).Encode([]map[string]any{p})
					return
				}
			}
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodPost && r.URL.Path == "/wp-json/wc/v3/products":
			var incoming map[string]any
			_ = json.NewDecoder(r.Body).Decode(&incoming)
			incoming["id"] = float64(15)
			incoming["status"] = "publish"
			stored = append(stored, incoming)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(incoming)
		default:
			t.Errorf("fake store saw %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return server, &stored
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test dir")
		}
		dir = parent
	}
}

func ecCli(t *testing.T, args ...string) (int, string) {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command("go", append([]string{"run", "./cmd/ec-cli"}, args...)...)
	cmd.Dir = root
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	rc := 0
	if ee, ok := err.(*exec.ExitError); ok {
		rc = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("go run ec-cli: %v\n%s", err, out.String())
	}
	return rc, out.String()
}

func writeDraftFile(t *testing.T, draft any) string {
	t.Helper()
	raw, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "draft.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runListingFlow is THE harness the fleet agent runs, in one place: gate
// the draft, then publish ONLY when every gate row passed. A red gate is
// terminal — publish is never attempted (publishRC -1). The named mutant
// is the gate going always-green: the helper then publishes the corrupted
// draft, the store fills, and the store-empty assertions go red.
func runListingFlow(t *testing.T, draftPath string) (gateRC, publishRC int, gateOut, publishOut string) {
	t.Helper()
	gateRC, gateOut = ecCli(t, "listing", "check", "--file", draftPath)
	if gateRC != 0 {
		return gateRC, -1, gateOut, ""
	}
	publishRC, publishOut = ecCli(t, "listing", "publish", "--file", draftPath)
	return gateRC, publishRC, gateOut, publishOut
}

type draft struct {
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

// TestFleetListingContract is the flow the fleet agent must solve, in
// order: a ledgered draft, a green gate, a publish the store accepts, and
// a second publish the exactly-once guard refuses.
func TestFleetListingContract(t *testing.T) {
	// 1. Draft: the AI call goes through the runtime's ledger wrapper —
	// one row per listing draft, tokens and the model recorded.
	rec := &captureRecorder{}
	gen := &costledger.RecordingGenerator{
		Inner:    scriptedGenerator{content: copyJSON},
		Recorder: rec,
	}
	ctx := costledger.WithAttrs(context.Background(), costledger.Attrs{
		JobID: "fleet-listing-1", TenantID: "sandbox", Action: "listing-draft",
	})
	resp, err := gen.Complete(ctx, port.AICompletionRequest{Model: "qwen3.8-27b-local"})
	if err != nil {
		t.Fatalf("draft call: %v", err)
	}
	var draftCopy struct {
		Title          string   `json:"title"`
		Bullets        []string `json:"bullets"`
		Description    string   `json:"description"`
		SearchKeywords string   `json:"search_keywords"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &draftCopy); err != nil {
		t.Fatalf("draft copy is not JSON: %v", err)
	}
	d := draft{
		SKU:            "bdo-5c-002", // the harness owns the commerce fields; the model owns the copy
		Title:          draftCopy.Title,
		Bullets:        draftCopy.Bullets,
		Description:    draftCopy.Description,
		SearchKeywords: draftCopy.SearchKeywords,
		PriceCents:     2990,
		Currency:       "AUD",
		Stock:          25,
		ImageSrc:       serverImageURL,
		ImageAlt:       "bamboo organizer on a desk",
	}
	if len(rec.rows) != 1 {
		t.Fatalf("ledger rows = %d, want exactly 1 per draft call", len(rec.rows))
	}
	row := rec.rows[0]
	if row.Action != "listing-draft" || row.Model != "qwen3.8-27b-local" || row.Status != "ok" {
		t.Fatalf("ledger row misattributed: %+v", row)
	}
	if row.TokensIn <= 0 || row.TokensOut <= 0 {
		t.Fatalf("ledger row must carry tokens in/out, got %+v", row)
	}

	// 2+3. The flow helper: gate, then publish only on a green gate.
	store, stored := fakeStore(t)
	t.Setenv("EC_STORE_URL", store.URL)
	t.Setenv("EC_STORE_KEY", "ck_test")
	t.Setenv("EC_STORE_SECRET", "cs_test")
	gateRC, publishRC, gateOut, publishOut := runListingFlow(t, writeDraftFile(t, d))
	if gateRC != 0 {
		t.Fatalf("gate rc = %d, want 0; output:\n%s", gateRC, gateOut)
	}
	for _, rowName := range []string{"PASS sku", "PASS title", "PASS bullets", "PASS description", "PASS search_keywords", "PASS price", "PASS stock", "PASS image_src"} {
		if !strings.Contains(gateOut, rowName) {
			t.Fatalf("gate output missing %q:\n%s", rowName, gateOut)
		}
	}
	if publishRC != 0 {
		t.Fatalf("publish rc = %d, want 0; output:\n%s", publishRC, publishOut)
	}
	if !strings.Contains(publishOut, "LISTED id=15 sku=BDO-5C-002 status=publish price=29.90 images=1") {
		t.Fatalf("publish output missing the verified LISTED line:\n%s", publishOut)
	}
	if n := len(*stored); n != 1 {
		t.Fatalf("store rows = %d, want exactly 1", n)
	}

	// 4. Exactly-once: the same SKU again is REFUSED, and the refusal
	// names the guard; the store still holds exactly one row.
	rc, out := ecCli(t, "listing", "publish", "--file", writeDraftFile(t, d))
	if rc != 1 {
		t.Fatalf("second publish rc = %d, want 1 (exactly-once refuses); output:\n%s", rc, out)
	}
	if !strings.Contains(out, "already listed") || !strings.Contains(out, "exactly-once") {
		t.Fatalf("refusal must name the exactly-once guard:\n%s", out)
	}
	if n := len(*stored); n != 1 {
		t.Fatalf("store rows after refused re-publish = %d, want still exactly 1", n)
	}
}

// serverImageURL is a FIXED sandbox shape: plain http on loopback is what
// the gate accepts for sandbox stores, and the fake store never serves
// images itself (the adapter hands the URL to the store, which would fetch
// it in a real sandbox).
var serverImageURL = "http://127.0.0.1:8181/wp-content/uploads/bdo-5c-002.jpg"

// TestFleetListingContract_RefusesToPublishThroughARedGate is the named
// mutant's death: "the verifier is always green". A corrupted draft (the
// title over the limit) must FAIL the gate, and runListingFlow treats that
// as terminal — publish is never attempted (publishRC -1) and nothing
// reaches the store. The one-line mutant is checkListing returning true
// unconditionally: the helper then publishes the corrupted draft (publish
// re-runs the same mutated gate), the store fills, and both the publishRC
// and the store-empty assertions below go red.
func TestFleetListingContract_RefusesToPublishThroughARedGate(t *testing.T) {
	bad := draft{
		SKU:            "bdo-5c-bad",
		Title:          strings.Repeat("x", 141), // over the 140 limit
		Bullets:        []string{"b1", "b2", "b3", "b4", "b5"},
		Description:    "Paragraph one.\n\nParagraph two.",
		SearchKeywords: "bamboo desk organizer",
		PriceCents:     2990,
		Currency:       "AUD",
		Stock:          25,
		ImageSrc:       serverImageURL,
		ImageAlt:       "alt",
	}
	// The whole flow through the ONE helper: a red gate is terminal —
	// publish is never attempted (publishRC -1), the FAIL row names the
	// violated constraint, and the store stays empty. Under the mutant
	// (checkListing always true) the helper's gate passes, publish runs
	// with the same mutated gate inside it, and BOTH the publishRC and
	// the store-count assertions go red.
	store, stored := fakeStore(t)
	t.Setenv("EC_STORE_URL", store.URL)
	t.Setenv("EC_STORE_KEY", "ck_test")
	t.Setenv("EC_STORE_SECRET", "cs_test")
	gateRC, publishRC, gateOut, _ := runListingFlow(t, writeDraftFile(t, bad))
	if gateRC != 1 {
		t.Fatalf("corrupted draft gate rc = %d, want 1; output:\n%s", gateRC, gateOut)
	}
	if !strings.Contains(gateOut, "FAIL title") {
		t.Fatalf("gate must print the FAIL row:\n%s", gateOut)
	}
	if publishRC != -1 {
		t.Fatalf("a red gate is TERMINAL: the flow must not attempt publish, got publish rc %d", publishRC)
	}
	if n := len(*stored); n != 0 {
		t.Fatalf("a red-gated draft reached the store (%d rows) — the always-green mutant is live", n)
	}
}
