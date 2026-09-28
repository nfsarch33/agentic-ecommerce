package publishgate

import (
	"context"
	"errors"

	"fmt"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestHarnessS1: 100 pending drafts with NO decisions: the gate refuses
// every publish (fail closed) and the store is untouched - zero remote calls.
func TestHarnessS1(t *testing.T) {
	base, key, secret, ok := wcFixtureEnv()
	if !ok {
		t.Skip("fixture env not found; skipping S1")
	}
	store := pgStore(t)
	_ = base
	_ = key
	_ = secret
	ctx := context.Background()
	var mu sync.Mutex
	calls := 0
	adapter := &countingAdapter{inner: &wooAdapter{client: woocommerce.NewClient(woocommerce.Config{BaseURL: base, ConsumerKey: key, ConsumerSecret: secret}, nil)}, onCreate: func() {}}
	adapter.inner.gets = 0
	_ = adapter
	// wrap to count every remote call (gets, creates, updates)
	counting := &allCountingAdapter{inner: adapter.inner, bump: func() { mu.Lock(); calls++; mu.Unlock() }}
	for i := 0; i < 100; i++ {
		wf := fmt.Sprintf("wf-h1-%02d", i)
		// no RecordApproval: the decision is missing on purpose
		g := &Gate{Store: store, Remote: counting, Owner: "h1", LeaseTTL: time.Minute}
		if _, err := g.Publish(ctx, PublishRequest{WorkflowID: wf, TenantID: "t1", ProductID: fmt.Sprintf("00000000-0000-0000-0000-%012d", 2000+i), SKU: fmt.Sprintf("HARN-S1-%02d", i), Fields: map[string]string{"name": "S1"}}); !errors.Is(err, ErrNoApproval) {
			t.Fatalf("S1 key %d: err = %v, want ErrNoApproval (fail closed)", i, err)
		}
	}
	if calls != 0 {
		t.Fatalf("S1: remote calls = %d, want 0 (no approval, no write)", calls)
	}
	n := queryCount(t, store, "SELECT count(*) FROM publish_ledger")
	if n != 0 {
		t.Fatalf("S1: ledger rows = %d, want 0", n)
	}
	t.Logf("S1 OK: 100 drafts unapproved, 0 remote writes, 0 ledger rows")
}

// allCountingAdapter counts every remote interaction.
type allCountingAdapter struct {
	inner *wooAdapter
	bump  func()
}

func (a *allCountingAdapter) FindBySKU(ctx context.Context, sku string) (*RemoteProduct, error) {
	a.bump()
	return a.inner.FindBySKU(ctx, sku)
}

func (a *allCountingAdapter) Update(ctx context.Context, remoteID string, fields map[string]string) (*RemoteProduct, error) {
	a.bump()
	return a.inner.Update(ctx, remoteID, fields)
}

func (a *allCountingAdapter) Create(ctx context.Context, fields map[string]string) (*RemoteProduct, error) {
	a.bump()
	return a.inner.Create(ctx, fields)
}

// TestHarnessS2: 50 distinct keys, one approved decision each; every second
// decision on the same workflow is refused by first-decision-wins; exactly
// 50 remote writes occur (one create per key), and the completed ledger
// holds 50 rows. The store-side double-decision refusal is what mc-api maps
// to 409 already_decided.
func TestHarnessS2(t *testing.T) {
	base, key, secret, ok := wcFixtureEnv()
	if !ok {
		t.Skip("fixture env not found; skipping S2")
	}
	store := pgStore(t)
	wc := &wooAdapter{client: woocommerce.NewClient(woocommerce.Config{BaseURL: base, ConsumerKey: key, ConsumerSecret: secret}, nil)}
	ctx := context.Background()

	purge := func(sku string) { deleteAllBySKU(t, wc, ctx, sku) }
	var mu sync.Mutex
	creates := 0
	adapter := &countingAdapter{inner: wc, onCreate: func() { mu.Lock(); creates++; mu.Unlock() }}

	for i := 0; i < 50; i++ {
		sku := fmt.Sprintf("HARN-S2-%02d", i)
		purge(sku)
		wf := fmt.Sprintf("wf-h2-%02d", i)
		if _, _, err := store.RecordApproval(ctx, Decision{WorkflowID: wf, TenantID: "t1", ProductID: fmt.Sprintf("00000000-0000-0000-0000-%012d", i), Approved: true, Actor: "h", UpdateID: "k"}); err != nil {
			t.Fatal(err)
		}
		g := &Gate{Store: store, Remote: adapter, Owner: "h2", LeaseTTL: time.Minute}
		if _, err := g.Publish(ctx, PublishRequest{WorkflowID: wf, TenantID: "t1", ProductID: fmt.Sprintf("00000000-0000-0000-0000-%012d", i), SKU: sku, Fields: map[string]string{"name": fmt.Sprintf("S2 %02d", i), "regular_price": "9.99"}}); err != nil {
			t.Fatalf("S2 key %d: %v", i, err)
		}
		// The second decision on the same workflow is refused.
		_, inserted, err := store.RecordApproval(ctx, Decision{WorkflowID: wf, TenantID: "t1", ProductID: fmt.Sprintf("00000000-0000-0000-0000-%012d", i), Approved: false, Actor: "h", UpdateID: "k2"})
		if err != nil || inserted {
			t.Fatalf("S2 key %d: second decision inserted=%v err=%v (want false,nil)", i, inserted, err)
		}
	}
	if creates != 50 {
		t.Fatalf("S2: creates = %d, want exactly 50", creates)
	}
	n := queryCount(t, store, "SELECT count(*) FROM publish_ledger WHERE status='completed'")
	if n != 50 {
		t.Fatalf("S2: completed ledger rows = %d, want 50", n)
	}
	t.Logf("S2 OK: 50 writes, 50 completed ledger rows, 50 second-decisions refused")
}

// TestHarnessS3a: the killed-worker shape. A first attempt claims the lease
// and dies before completing (simulated by claiming then abandoning with a
// short lease); the second attempt must reclaim the expired lease, GET the
// SKU first, and complete - with exactly one remote write per key.
func TestHarnessS3a(t *testing.T) {
	base, key, secret, ok := wcFixtureEnv()
	if !ok {
		t.Skip("fixture env not found; skipping S3a")
	}
	store := pgStore(t)
	wc := &wooAdapter{client: woocommerce.NewClient(woocommerce.Config{BaseURL: base, ConsumerKey: key, ConsumerSecret: secret}, nil)}
	ctx := context.Background()

	var mu sync.Mutex
	creates := 0
	adapter := &countingAdapter{inner: wc, onCreate: func() { mu.Lock(); creates++; mu.Unlock() }}

	for i := 0; i < 30; i++ {
		sku := fmt.Sprintf("HARN-S3A-%02d", i)
		deleteAllBySKU(t, wc, ctx, sku)
		wf := fmt.Sprintf("wf-h3-%02d", i)
		pid := fmt.Sprintf("00000000-0000-0000-0000-%012d", 1000+i)
		if _, _, err := store.RecordApproval(ctx, Decision{WorkflowID: wf, TenantID: "t1", ProductID: pid, Approved: true, Actor: "h", UpdateID: "k"}); err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{"name": fmt.Sprintf("S3A %02d", i), "regular_price": "7.77"}
		k, err := Key(fmt.Sprintf("00000000-0000-0000-0000-%012d", 1000+i), fields)
		if err != nil {
			t.Fatal(err)
		}
		fp, _ := Fingerprint(fields)
		// Attempt 1 claims the lease then is SIGKILLed (short lease).
		if err := store.Claim(ctx, k, "t1", wf, pid, sku, fp, "killed-worker", 2*time.Second); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second) // lease expires
		// Attempt 2 reclaims and completes.
		g := &Gate{Store: store, Remote: adapter, Owner: "retry", LeaseTTL: time.Minute}
		res, err := g.Publish(ctx, PublishRequest{WorkflowID: wf, TenantID: "t1", ProductID: fmt.Sprintf("00000000-0000-0000-0000-%012d", 1000+i), SKU: sku, Fields: fields})
		if err != nil {
			t.Fatalf("S3a key %d: %v", i, err)
		}
		if res.WriteKind != "create" {
			t.Fatalf("S3a key %d: write kind %s, want create (the killed attempt wrote nothing)", i, res.WriteKind)
		}
	}
	if creates != 30 {
		t.Fatalf("S3a: creates = %d, want exactly 30 (one per key)", creates)
	}
	t.Logf("S3a OK: 30/30 keys published, exactly one remote write per key after lease reclaim")
}

// countingAdapter wraps the fixture adapter counting creates (the proxy count).
type countingAdapter struct {
	inner    *wooAdapter
	onCreate func()
}

func (a *countingAdapter) FindBySKU(ctx context.Context, sku string) (*RemoteProduct, error) {
	return a.inner.FindBySKU(ctx, sku)
}

func (a *countingAdapter) Update(ctx context.Context, remoteID string, fields map[string]string) (*RemoteProduct, error) {
	return a.inner.Update(ctx, remoteID, fields)
}

func (a *countingAdapter) Create(ctx context.Context, fields map[string]string) (*RemoteProduct, error) {
	if a.onCreate != nil {
		a.onCreate()
	}
	return a.inner.Create(ctx, fields)
}

func queryCount(t *testing.T, s *PGStore, q string) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), q).Scan(&n); err != nil {
		t.Fatalf("query %s: %v", q, err)
	}
	return n
}

func deleteProduct(t *testing.T, ctx context.Context, id int) bool {
	t.Helper()
	base, key, secret, _ := wcFixtureEnv()
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/wp-json/wc/v3/products/%d?force=true", base, id), nil)
	req.SetBasicAuth(key, secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 300
}

func deleteAllBySKU(t *testing.T, c *wooAdapter, ctx context.Context, sku string) {
	t.Helper()
	for {
		p, err := c.client.FindProductBySKU(ctx, sku)
		if err != nil || p == nil {
			return
		}
		if !deleteProduct(t, ctx, p.ID) {
			return
		}
	}
}
