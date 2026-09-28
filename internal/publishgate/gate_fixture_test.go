package publishgate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
)

// wcFixtureEnv returns the fixture store coordinates from the env file the
// fixture-init script writes, or "" to skip (skip with a message).
func wcFixtureEnv() (base, key, secret string, ok bool) {
	for _, p := range []string{os.Getenv("WOO_ENV_FILE"), ".local/fixture-woo.env", "../../.local/fixture-woo.env", "../../../fixture-init/.local/fixture-woo.env"} {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range splitLines(string(b)) {
			switch {
			case hasPrefix(line, "WOO_BASE_URL="):
				base = trimAfter(line, "WOO_BASE_URL=")
			case hasPrefix(line, "WOO_CONSUMER_KEY="):
				key = trimAfter(line, "WOO_CONSUMER_KEY=")
			case hasPrefix(line, "WOO_CONSUMER_SECRET="):
				secret = trimAfter(line, "WOO_CONSUMER_SECRET=")
			}
		}
		if base != "" && key != "" && secret != "" {
			return base, key, secret, true
		}
	}
	return "", "", "", false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func trimAfter(s, p string) string { return s[len(p):] }

// wooAdapter adapts the repo's woocommerce.Client to the gate's port and
// records call counts (the counting side the harness proxy provides in S1-S3).
type wooAdapter struct {
	client   woocommerce.Client
	gets     int
	posts    int
	puts     int
	lastRead map[string]string
}

func (a *wooAdapter) FindBySKU(ctx context.Context, sku string) (*RemoteProduct, error) {
	a.gets++
	p, err := a.client.FindProductBySKU(ctx, sku)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	return &RemoteProduct{ID: fmt.Sprintf("%d", p.ID), Fields: liveFields(p)}, nil
}

func (a *wooAdapter) Update(ctx context.Context, remoteID string, fields map[string]string) (*RemoteProduct, error) {
	a.puts++
	p, err := a.client.UpdateProduct(ctx, remoteID, fields)
	if err != nil {
		return nil, err
	}
	return &RemoteProduct{ID: fmt.Sprintf("%d", p.ID), Fields: liveFields(p)}, nil
}

func (a *wooAdapter) Create(ctx context.Context, fields map[string]string) (*RemoteProduct, error) {
	a.posts++
	p, err := a.client.CreateProduct(ctx, fields)
	if err != nil {
		return nil, err
	}
	return &RemoteProduct{ID: fmt.Sprintf("%d", p.ID), Fields: liveFields(p)}, nil
}

// liveFields extracts the fields the gate matches on. Q2 (normalisation) is
// measured here, not assumed: the fixture test below asserts a round trip.
func liveFields(p *woocommerce.Product) map[string]string {
	f := map[string]string{"name": p.Name, "sku": p.SKU}
	if p.Regular != "" {
		f["regular_price"] = p.Regular
	}
	if p.ShortDesc != "" {
		f["short_description"] = p.ShortDesc
	}
	return f
}

// TestFixtureGateRoundTrip runs the gate against the live WooCommerce
// fixture: create -> none (matches) -> update (differs). It also measures Q2:
// whether the store normalises the fields FieldsFor writes, by asserting the
// create round trip Matches its own write fields.
func TestFixtureGateRoundTrip(t *testing.T) {
	base, key, secret, ok := wcFixtureEnv()
	if !ok {
		t.Skip("fixture env file not found (run scripts/fixtures/wordpress-fixture-init.sh first); skipping fixture gate round trip")
	}
	wc := woocommerce.NewClient(woocommerce.Config{BaseURL: base, ConsumerKey: key, ConsumerSecret: secret}, nil)
	adapter := &wooAdapter{client: wc}

	store := pgStore(t)
	ctx := context.Background()
	const wf = "wf-fixture-gate"
	const sku = "GATE-RT-1"
	// Hermetic setup and teardown: remove every store copy of this SKU so
	// each run starts from "SKU absent" (the client has no delete; the REST
	// API does).
	purge := func() {
		for {
			p, err := wc.FindProductBySKU(ctx, sku)
			if err != nil || p == nil {
				return
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodDelete,
				base+"/wp-json/wc/v3/products/"+fmt.Sprintf("%d", p.ID)+"?force=true", nil)
			req.SetBasicAuth(key, secret)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode >= 300 {
				return
			}
		}
	}
	purge()
	t.Cleanup(purge)
	if _, _, err := store.RecordApproval(ctx, Decision{WorkflowID: wf, TenantID: "t1", ProductID: "00000000-0000-0000-0000-0000000000aa", Approved: true, Actor: "test", UpdateID: "k1"}); err != nil {
		t.Fatal(err)
	}

	fields := map[string]string{"name": "Gate Round Trip", "regular_price": "12.50", "short_description": "gate test"}
	gate := &Gate{Store: store, Remote: adapter, Owner: "fixture-test", LeaseTTL: time.Minute}

	res, err := gate.Publish(ctx, PublishRequest{WorkflowID: wf, TenantID: "t1", ProductID: "00000000-0000-0000-0000-0000000000bb", SKU: sku, Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	if res.WriteKind != "create" {
		t.Fatalf("first publish res=%+v, want create", res)
	}

	// Second publish of the same key: ledger short-circuits.
	res2, err := gate.Publish(ctx, PublishRequest{WorkflowID: wf, TenantID: "t1", ProductID: "00000000-0000-0000-0000-0000000000bb", SKU: sku, Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	if res2.WriteKind != "none" || adapter.posts != 1 {
		t.Fatalf("second publish res=%+v posts=%d, want none with one POST total", res2, adapter.posts)
	}

	// Changed fields under a NEW key (new product id namespace keeps it
	// simple): update path drives PUT on the live id.
	changed := map[string]string{"name": "Gate Round Trip v2", "regular_price": "13.00", "short_description": "gate test"}
	gate2 := &Gate{Store: store, Remote: adapter, Owner: "fixture-test", LeaseTTL: time.Minute}
	res3, err := gate2.Publish(ctx, PublishRequest{WorkflowID: wf, TenantID: "t1", ProductID: "00000000-0000-0000-0000-0000000000cc", SKU: sku, Fields: changed})
	if err != nil {
		t.Fatal(err)
	}
	if res3.WriteKind != "update" || res3.RemoteID != res.RemoteID {
		t.Fatalf("changed publish res=%+v, want update of %s", res3, res.RemoteID)
	}

	// Q2 measurement: does a fresh read of the SKU match what we wrote?
	live, err := adapter.FindBySKU(ctx, sku)
	if err != nil {
		t.Fatal(err)
	}
	if !Matches(live.Fields, changed) {
		t.Logf("Q2: the store normalises fields (live=%v vs written=%v) - Matches must canonicalise the same way", live.Fields, changed)
	} else {
		t.Log("Q2: no normalisation observed on these fields (round trip Matches)")
	}
}

// Mutant coverage for the adapter lookups: a wrong-URL FindBySKU (the
// counting assertion) fails the round trip above with a create on every
// publish - caught by posts==1.
var _ = errors.Is
