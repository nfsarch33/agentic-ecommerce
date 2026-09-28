package publishgate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// memStore is an in-memory Store for unit tests. Its Claim implements the
// same contract as PGStore so the gate tests exercise real claim races.
type memStore struct {
	mu          sync.Mutex
	approvals   map[string]Decision
	ledger      map[string]*memRow
	claimCalls  int
	completeKnd map[string]string
}

type memRow struct {
	status    string
	writeKind string
	remoteID  string
	attempts  int
	owner     string
	until     time.Time
}

func newMemStore() *memStore {
	return &memStore{approvals: map[string]Decision{}, ledger: map[string]*memRow{}, completeKnd: map[string]string{}}
}

func (m *memStore) RecordApproval(_ context.Context, d Decision) (Decision, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if first, ok := m.approvals[d.WorkflowID]; ok {
		return first, false, nil
	}
	m.approvals[d.WorkflowID] = d
	return d, true, nil
}

func (m *memStore) ApprovalFor(_ context.Context, workflowID string) (Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.approvals[workflowID]
	if !ok {
		return Decision{}, ErrNoApproval
	}
	return d, nil
}

func (m *memStore) Claim(_ context.Context, idemKey, _, _, _, _, _, owner string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claimCalls++
	row, ok := m.ledger[idemKey]
	if !ok {
		m.ledger[idemKey] = &memRow{status: "in_progress", attempts: 1, owner: owner, until: time.Now().Add(ttl)}
		return nil
	}
	if row.status == "completed" {
		return ErrAlreadyCompleted
	}
	if row.owner != owner && time.Now().Before(row.until) {
		return ErrLeaseHeld
	}
	row.attempts++
	row.status = "in_progress"
	row.owner = owner
	row.until = time.Now().Add(ttl)
	return nil
}

func (m *memStore) Complete(_ context.Context, idemKey, status, writeKind, remoteID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := m.ledger[idemKey]
	row.status, row.writeKind, row.remoteID = status, writeKind, remoteID
	m.completeKnd[idemKey] = writeKind
	row.owner, row.until = "", time.Time{}
	return nil
}

func (m *memStore) Fail(_ context.Context, idemKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := m.ledger[idemKey]
	row.status = "failed"
	row.owner, row.until = "", time.Time{}
	return nil
}

// fakeRemote records calls and serves scripted products.
type fakeRemote struct {
	mu      sync.Mutex
	bySKU   map[string]*RemoteProduct
	nextID  int
	gets    int
	posts   int
	puts    int
	failSKU bool // FindBySKU errors when true
}

func (f *fakeRemote) FindBySKU(_ context.Context, sku string) (*RemoteProduct, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.failSKU {
		return nil, errors.New("store 5xx")
	}
	return f.bySKU[sku], nil
}

func (f *fakeRemote) Update(_ context.Context, remoteID string, fields map[string]string) (*RemoteProduct, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts++
	return &RemoteProduct{ID: remoteID, Fields: fields}, nil
}

func (f *fakeRemote) Create(_ context.Context, fields map[string]string) (*RemoteProduct, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts++
	f.nextID++
	return &RemoteProduct{ID: "wp-100", Fields: fields}, nil
}

func approvedStore(t *testing.T) *memStore {
	t.Helper()
	s := newMemStore()
	_, _, err := s.RecordApproval(context.Background(), Decision{
		WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", Approved: true, Actor: "rev", UpdateID: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPublishFailsClosedWithoutApprovalRow(t *testing.T) {
	s := newMemStore() // no approval recorded
	g := &Gate{Store: s, Remote: &fakeRemote{}, Owner: "w1"}
	_, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-x", SKU: "S", Fields: map[string]string{"a": "1"}})
	if !errors.Is(err, ErrNoApproval) {
		t.Fatalf("err = %v, want ErrNoApproval (fail closed)", err)
	}
	if r := g.Remote.(*fakeRemote); r.gets+r.posts+r.puts != 0 {
		t.Fatalf("remote touched with no approval: gets=%d posts=%d puts=%d", r.gets, r.posts, r.puts)
	}
}

func TestPublishRejectedDecisionNeverWrites(t *testing.T) {
	s := newMemStore()
	if _, _, err := s.RecordApproval(context.Background(), Decision{WorkflowID: "wf-1", Approved: false, UpdateID: "k1"}); err != nil {
		t.Fatal(err)
	}
	r := &fakeRemote{}
	g := &Gate{Store: s, Remote: r, Owner: "w1"}
	_, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", SKU: "S", Fields: map[string]string{"a": "1"}})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
	if r.gets+r.posts+r.puts != 0 {
		t.Fatal("rejected decision wrote to the store")
	}
}

func TestPublishCreatesWhenSKUAbsent(t *testing.T) {
	s := approvedStore(t)
	r := &fakeRemote{bySKU: map[string]*RemoteProduct{}}
	g := &Gate{Store: s, Remote: r, Owner: "w1"}
	res, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S", Fields: map[string]string{"name": "A"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.WriteKind != "create" || r.posts != 1 || r.gets != 1 {
		t.Fatalf("res=%+v posts=%d gets=%d, want create with one GET then one POST", res, r.posts, r.gets)
	}
}

func TestPublishUpdatesByIDWhenLiveDiffers(t *testing.T) {
	s := approvedStore(t)
	r := &fakeRemote{bySKU: map[string]*RemoteProduct{"S": {ID: "wp-9", Fields: map[string]string{"name": "Old"}}}}
	g := &Gate{Store: s, Remote: r, Owner: "w1"}
	res, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S", Fields: map[string]string{"name": "New"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.WriteKind != "update" || res.RemoteID != "wp-9" || r.puts != 1 || r.posts != 0 {
		t.Fatalf("res=%+v puts=%d posts=%d, want update of wp-9", res, r.puts, r.posts)
	}
}

func TestPublishSkipsWhenLiveMatchesFingerprint(t *testing.T) {
	s := approvedStore(t)
	fields := map[string]string{"name": "Same", "price": "10.00"}
	r := &fakeRemote{bySKU: map[string]*RemoteProduct{"S": {ID: "wp-7", Fields: map[string]string{"name": "Same ", "price": "10.00"}}}}
	g := &Gate{Store: s, Remote: r, Owner: "w1"}
	res, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S", Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	if res.WriteKind != "none" || res.RemoteID != "wp-7" || r.posts+r.puts != 0 {
		t.Fatalf("res=%+v posts=%d puts=%d, want none (live already matches)", res, r.posts, r.puts)
	}
}

func TestLedgerCompletedShortCircuits(t *testing.T) {
	s := approvedStore(t)
	r := &fakeRemote{bySKU: map[string]*RemoteProduct{}}
	g := &Gate{Store: s, Remote: r, Owner: "w1"}
	req := PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S", Fields: map[string]string{"name": "A"}}
	if _, err := g.Publish(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	// Second publish of the same key: ledger completed -> none, zero remote calls.
	if _, err := g.Publish(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if r.gets != 1 || r.posts != 1 {
		t.Fatalf("gets=%d posts=%d, want exactly one each across both publishes (completed short-circuit)", r.gets, r.posts)
	}
}

func TestLeaseHeldReturnsRetryable(t *testing.T) {
	s := approvedStore(t)
	// Foreign claim: same key, live lease, different owner.
	key, err := Key("p1", map[string]string{"name": "A"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Claim(context.Background(), key, "t1", "wf-1", "p1", "S", "fp", "other-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	g := &Gate{Store: s, Remote: &fakeRemote{}, Owner: "w1"}
	_, err = g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S", Fields: map[string]string{"name": "A"}})
	if !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("err = %v, want ErrLeaseHeld", err)
	}
}

func TestExpiredLeaseIsReclaimed(t *testing.T) {
	s := approvedStore(t)
	key, err := Key("p1", map[string]string{"name": "A"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Claim(context.Background(), key, "t1", "wf-1", "p1", "S", "fp", "other-worker", -time.Second); err != nil {
		t.Fatal(err)
	}
	g := &Gate{Store: s, Remote: &fakeRemote{bySKU: map[string]*RemoteProduct{}}, Owner: "w1"}
	res, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S", Fields: map[string]string{"name": "A"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.WriteKind != "create" {
		t.Fatalf("res=%+v, want create after reclaiming the expired lease", res)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	s := approvedStore(t)
	r := &fakeRemote{bySKU: map[string]*RemoteProduct{}}
	g := &Gate{Store: s, Remote: r, DryRun: true, Owner: "w1"}
	res, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S", Fields: map[string]string{"name": "A"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.WriteKind != "dry_run" || res.Status != "dry_run" {
		t.Fatalf("res=%+v, want dry_run", res)
	}
	if r.gets+r.posts+r.puts != 0 {
		t.Fatal("dry-run touched the store")
	}
}

func TestLookupFailureReleasesLease(t *testing.T) {
	s := approvedStore(t)
	r := &fakeRemote{failSKU: true}
	g := &Gate{Store: s, Remote: r, Owner: "w1"}
	if _, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S", Fields: map[string]string{"name": "A"}}); err == nil {
		t.Fatal("lookup failure must error")
	}
	// Lease released: a retry (remote healthy now) succeeds immediately.
	r.failSKU = false
	r.bySKU = map[string]*RemoteProduct{}
	g2 := &Gate{Store: s, Remote: r, Owner: "w1"}
	res, err := g2.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S", Fields: map[string]string{"name": "A"}})
	if err != nil {
		t.Fatalf("retry after lease release: %v", err)
	}
	if res.WriteKind != "create" {
		t.Fatalf("res=%+v, want create on retry", res)
	}
}

// Mutant-proven sentinel wiring: RecordApproval keeps the FIRST decision.
func TestRecordApprovalKeepsFirstDecision(t *testing.T) {
	s := newMemStore()
	_, inserted, err := s.RecordApproval(context.Background(), Decision{WorkflowID: "wf-1", Approved: true, UpdateID: "k1"})
	if err != nil || !inserted {
		t.Fatalf("first: inserted=%v err=%v", inserted, err)
	}
	stored, inserted, err := s.RecordApproval(context.Background(), Decision{WorkflowID: "wf-1", Approved: false, UpdateID: "k2"})
	if err != nil || inserted {
		t.Fatalf("second: inserted=%v err=%v (want false, first decision kept)", inserted, err)
	}
	if !stored.Approved {
		t.Fatal("stored decision flipped; the first must win")
	}
}

func TestKeyAndMatchesVendored(t *testing.T) {
	k1, err := Key("p1", map[string]string{"b": "2", "a": "1"})
	if err != nil {
		t.Fatal(err)
	}
	k2, _ := Key("p1", map[string]string{"a": "1", "b": "2"})
	if k1 != k2 {
		t.Fatal("key must be stable under map order")
	}
	if _, err := Key("", map[string]string{"a": "1"}); !errors.Is(err, ErrKeyBadDraftID) {
		t.Fatalf("empty id err = %v", err)
	}
	if _, err := Key("a:b", map[string]string{"a": "1"}); !errors.Is(err, ErrKeyBadDraftIDChar) {
		t.Fatalf("colon id err = %v", err)
	}
	if _, err := Key("p", nil); !errors.Is(err, ErrKeyNoFields) {
		t.Fatalf("no fields err = %v", err)
	}
	if !Matches(map[string]string{"n": " A "}, map[string]string{"n": "A"}) {
		t.Fatal("trim-insensitive match failed")
	}
	if Matches(map[string]string{}, map[string]string{}) {
		t.Fatal("empty desired must not match")
	}
}
