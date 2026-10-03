package storeproxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const canaryKey = "ck_CANARY_NEVER_IN_LOG"
const canarySecret = "cs_CANARY_NEVER_IN_LOG"

func newTestProxy(t *testing.T, store *httptest.Server) (*Proxy, string) {
	t.Helper()
	return newTestProxyTimeout(t, store, 0)
}

func newTestProxyTimeout(t *testing.T, store *httptest.Server, clientTimeout time.Duration) (*Proxy, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "audit.ndjson")
	p, err := NewProxy(Config{
		StoreBaseURL:   store.URL,
		ConsumerKey:    canaryKey,
		ConsumerSecret: canarySecret,
		AuditLogPath:   logPath,
		Now:            func() time.Time { return time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	if clientTimeout > 0 {
		p.client.Timeout = clientTimeout
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, logPath
}

func writeReq(method, path, body, approval string, extra ...[2]string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if approval != "" {
		req.Header.Set(ApprovalHeader, approval)
	}
	for _, kv := range extra {
		req.Header.Set(kv[0], kv[1])
	}
	return req
}

func readLog(t *testing.T, path string) []AuditRow {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var rows []AuditRow
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var r AuditRow
		if err := json.NewDecoder(strings.NewReader(line)).Decode(&r); err != nil {
			t.Fatalf("bad audit line %q: %v", line, err)
		}
		rows = append(rows, r)
	}
	return rows
}

// MUTANT: drop the credential injection and the store sees no key — 401.
func TestProxyInjectsStoreKeyOnForward(t *testing.T) {
	var gotKey, gotSecret, gotAuth string
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("consumer_key")
		gotSecret = r.URL.Query().Get("consumer_secret")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, _ := newTestProxy(t, store)

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, writeReq(http.MethodGet, "/wp-json/wc/v3/products?per_page=1", "", "",
		[2]string{"Authorization", "Basic c216umno6c216umno="}))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if gotKey != canaryKey || gotSecret != canarySecret {
		t.Fatalf("proxy did not inject the store key: key=%q secret=%q", gotKey, gotSecret)
	}
	if gotAuth != "" {
		t.Fatalf("caller Authorization reached the store: %q", gotAuth)
	}
}

// MUTANT: forward the caller's headers untouched and a smuggled Basic pair
// (the store checks it BEFORE the query params) passes the audit point.
func TestSmuggledCredentialsNeverReachTheStore(t *testing.T) {
	var sawQueryKey, sawAuth, sawCookie bool
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawQueryKey = r.URL.Query().Get("consumer_key") == "ck_SMUGGLED"
		sawAuth = r.Header.Get("Authorization") != ""
		sawCookie = r.Header.Get("Cookie") != ""
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, _ := newTestProxy(t, store)

	req := writeReq(http.MethodPut, "/wp-json/wc/v3/products/42?consumer_key=ck_SMUGGLED&consumer_secret=x", `{}`, "ap-1",
		[2]string{"Authorization", "Basic c216umno6c216umno="},
		[2]string{"Cookie", "session=steal"},
		[2]string{"Proxy-Authorization", "Basic zzz"})
	p.ServeHTTP(httptest.NewRecorder(), req)
	if sawQueryKey || sawAuth || sawCookie {
		t.Fatalf("smuggled credentials forwarded: query=%v auth=%v cookie=%v", sawQueryKey, sawAuth, sawCookie)
	}
}

// MUTANT: remove the allowlist check and any local process can reach any
// path on the store origin with the proxy's key injected; this goes red.
func TestOffAllowlistPathRefusedWithAuditRow(t *testing.T) {
	forwarded := 0
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded++
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, logPath := newTestProxy(t, store)

	for _, path := range []string{"/wp-json/wc/v2/products", "/wp-admin/options.php", "/wp-json/wc/v3/../../users", "/customers"} {
		rr := httptest.NewRecorder()
		p.ServeHTTP(rr, writeReq(http.MethodGet, path, "", ""))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("path %s = %d, want 403", path, rr.Code)
		}
	}
	if forwarded != 0 {
		t.Fatalf("off-allowlist requests reached the store (%d forwards)", forwarded)
	}
	rows := readLog(t, logPath)
	if len(rows) != 4 {
		t.Fatalf("audit rows = %d, want one refused row per attempt", len(rows))
	}
	for _, r := range rows {
		if r.Kind != "refused" || r.Status != http.StatusForbidden {
			t.Fatalf("refusal row wrong: %+v", r)
		}
	}
}

// MUTANT: drop the write-ahead intent and a timeout after the store applied
// the write leaves no trace; this test goes red (no intent/unknown rows).
func TestTimeoutAfterSendLeavesIntentAndUnknownRows(t *testing.T) {
	applied := make(chan struct{}, 1)
	release := make(chan struct{})
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		applied <- struct{}{}
		<-release // the store applies the write, then hangs past the client timeout
		w.WriteHeader(http.StatusCreated)
	}))
	defer store.Close()
	defer close(release)

	p, logPath := newTestProxyTimeout(t, store, 150*time.Millisecond)
	done := make(chan struct{})
	go func() {
		p.ServeHTTP(httptest.NewRecorder(), writeReq(http.MethodPost, "/wp-json/wc/v3/products", `{}`, "ap-t"))
		close(done)
	}()
	<-applied
	started := time.Now()
	<-done
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond {
		t.Fatalf("the forward returned in %v, before the 150ms client timeout could fire", elapsed)
	}

	rows := readLog(t, logPath)
	var intents, unknowns int
	for _, r := range rows {
		switch r.Kind {
		case "intent":
			intents++
			if r.ApprovalID != "ap-t" {
				t.Fatalf("intent row missing approval: %+v", r)
			}
		case "unknown":
			unknowns++
		}
	}
	if intents != 1 || unknowns != 1 {
		t.Fatalf("timeout evidence rows: intents=%d unknowns=%d, want 1/1 (rows %+v)", intents, unknowns, rows)
	}
}

// MUTANT: treat an audit-append failure as non-fatal and an unauditable
// write is forwarded anyway; this goes red (0 forwards, 503).
func TestUnwritableAuditLogRefusesWrites(t *testing.T) {
	forwarded := 0
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded++
		w.WriteHeader(http.StatusCreated)
	}))
	defer store.Close()
	p, _ := newTestProxy(t, store)
	p.mu.Lock()
	_ = p.log.Close()
	p.log = nil // the append path now fails
	p.mu.Unlock()

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, writeReq(http.MethodPost, "/wp-json/wc/v3/products", `{}`, "ap-x"))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("write with a dead audit log = %d, want 503 fail-closed", rr.Code)
	}
	if forwarded != 0 {
		t.Fatalf("an unauditable write was forwarded (%d)", forwarded)
	}
}

// MUTANT: log the full URL and the canary leaks; path-only is pinned, and
// the intent→done pair is the shape the nightly join counts.
func TestAuditRowsCarryPathOnlyAndKinds(t *testing.T) {
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer store.Close()
	p, logPath := newTestProxy(t, store)

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, writeReq(http.MethodPost, "/wp-json/wc/v3/products?foo=bar", `{}`, "product-publish-abc-123"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status passthrough = %d, want 201", rr.Code)
	}
	rows := readLog(t, logPath)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want intent+done", len(rows))
	}
	if rows[0].Kind != "intent" || rows[0].Status != 0 || rows[0].ApprovalID != "product-publish-abc-123" {
		t.Fatalf("intent row wrong: %+v", rows[0])
	}
	if rows[1].Kind != "done" || rows[1].Status != http.StatusCreated || rows[1].Path != "/wp-json/wc/v3/products" {
		t.Fatalf("done row wrong: %+v", rows[1])
	}
	if blob, _ := os.ReadFile(logPath); strings.Contains(string(blob), canaryKey) || strings.Contains(string(blob), "foo=bar") {
		t.Fatalf("audit log must carry the path only, no query or credentials: %s", blob)
	}
}

// MUTANT: remove the approval check and an unauditable write is forwarded.
func TestWriteWithoutApprovalRefused(t *testing.T) {
	forwarded := 0
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded++
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, logPath := newTestProxy(t, store)

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, writeReq(http.MethodPut, "/wp-json/wc/v3/products/42", `{}`, ""))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("write without approval = %d, want 403", rr.Code)
	}
	if forwarded != 0 {
		t.Fatalf("an unauditable write reached the store (%d forwards)", forwarded)
	}
	if rows := readLog(t, logPath); len(rows) != 1 || rows[0].Kind != "refused" {
		t.Fatalf("refused write must leave exactly one refused row: %+v", rows)
	}
}

// The verdict's probe table, now COMMITTED (each probe reached the store
// with the proxy key and no approval header on the reviewed head).
//
// MUTANT (any one guard removed): the corresponding probe forwards again.
func TestMethodOverrideBypassesAreRefused(t *testing.T) {
	forwarded := 0
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded++
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, logPath := newTestProxy(t, store)

	probes := []struct {
		name   string
		method string
		path   string
		hdr    [2]string
	}{
		{"_method query on a GET", http.MethodGet, "/wp-json/wc/v3/products/7?_method=DELETE&force=true", [2]string{}},
		{"X-HTTP-Method-Override header", http.MethodGet, "/wp-json/wc/v3/products/7", [2]string{"X-HTTP-Method-Override", "DELETE"}},
		{"X-HTTP-Method header", http.MethodGet, "/wp-json/wc/v3/products/7", [2]string{"X-HTTP-Method", "DELETE"}},
		{"X-Method-Override header", http.MethodGet, "/wp-json/wc/v3/products/7", [2]string{"X-Method-Override", "DELETE"}},
		{"PURGE verb", "PURGE", "/wp-json/wc/v3/products/7", [2]string{}},
		{"PROPFIND verb", "PROPFIND", "/wp-json/wc/v3/products/7", [2]string{}},
		{"custom verb", "FROB", "/wp-json/wc/v3/products/7", [2]string{}},
		{"double-encoded traversal", http.MethodGet, "/wp-json/wc/v3/%252e%252e/%252e%252e/wp/v2/users", [2]string{}},
	}
	var refused int
	for _, pr := range probes {
		var req *http.Request
		if pr.hdr[0] != "" {
			req = writeReq(pr.method, pr.path, "", "", pr.hdr)
		} else {
			req = writeReq(pr.method, pr.path, "", "")
		}
		rr := httptest.NewRecorder()
		p.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden && rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: code=%d, want 403/405", pr.name, rr.Code)
		}
		if forwarded != 0 {
			t.Fatalf("%s: reached the store (%d forwards)", pr.name, forwarded)
		}
		refused++
	}
	rows := readLog(t, logPath)
	if len(rows) != refused {
		t.Fatalf("refused rows = %d, want %d (one per probe)", len(rows), refused)
	}
	for _, r := range rows {
		if r.Kind != "refused" {
			t.Fatalf("non-refused row for a refused probe: %+v", r)
		}
	}
}

// The round-3 probes: the two query bypasses from the SECURITY verdict.
//
// MUTANT: drop the phpKey normalisation (match the raw key only) and the
// ".method"/"%20method" probes forward — the store reads both as _method.
// MUTANT: drop the writeBodyAllowed call and the form POST forwards — its
// rest_route rides $_POST past the query guard.
func TestPHPMangledKeysAndRestRouteRefused(t *testing.T) {
	forwarded := 0
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded++
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, logPath := newTestProxy(t, store)

	probes := []struct {
		name   string
		method string
		path   string
		ct     string
		body   string
	}{
		{"PHP mangles .method into _method", http.MethodGet, "/wp-json/wc/v3/products/7?.method=DELETE", "", ""},
		{"PHP mangles %20method into _method", http.MethodGet, "/wp-json/wc/v3/products/7?%20method=DELETE", "", ""},
		{"rest_route reroutes off the audited path", http.MethodGet, "/wp-json/wc/v3/products?rest_route=/wp/v2/users", "", ""},
		{"form body carries rest_route on a write", http.MethodPost, "/wp-json/wc/v3/products", "application/x-www-form-urlencoded", "rest_route=%2Fwp%2Fv2%2Fusers"},
	}
	for _, pr := range probes {
		var req *http.Request
		if pr.ct != "" {
			// The write probe carries an approval id: the form body must be
			// the ONLY reason it is refused.
			req = writeReq(pr.method, pr.path, pr.body, "appr-form-1", [2]string{"Content-Type", pr.ct})
		} else {
			req = writeReq(pr.method, pr.path, "", "")
		}
		rr := httptest.NewRecorder()
		p.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s: code=%d, want 403", pr.name, rr.Code)
		}
		if forwarded != 0 {
			t.Fatalf("%s: reached the store (%d forwards)", pr.name, forwarded)
		}
	}
	rows := readLog(t, logPath)
	if len(rows) != len(probes) {
		t.Fatalf("refused rows = %d, want %d (one per probe)", len(rows), len(probes))
	}
	for _, r := range rows {
		if r.Kind != "refused" {
			t.Fatalf("non-refused row for a refused probe: %+v", r)
		}
	}
}

// MUTANT: revert methodClass to the denylist (unknown verbs = reads) and
// this goes red — PURGE would forward as an unapproved read.
func TestUnknownVerbIsRejectedNotRead(t *testing.T) {
	forwarded := 0
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded++
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, _ := newTestProxy(t, store)
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, writeReq("PURGE", "/wp-json/wc/v3/products/7", "", ""))
	if rr.Code != http.StatusMethodNotAllowed || forwarded != 0 {
		t.Fatalf("PURGE = %d forwards=%d, want 405/0", rr.Code, forwarded)
	}
}

// MUTANT: drop the unescaped-path guard and %2e%2e traversal forwards.
func TestDoubleEncodedPathRefused(t *testing.T) {
	forwarded := 0
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded++
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, _ := newTestProxy(t, store)
	rr := httptest.NewRecorder()
	// %252e decodes ONCE to %2e — the decoded path carries no "..", so the
	// string check alone passes it; only the unescaped-wire guard refuses.
	p.ServeHTTP(rr, writeReq(http.MethodGet, "/wp-json/wc/v3/%252e%252e/%252e%252e/wp/v2/users", "", ""))
	if rr.Code != http.StatusForbidden || forwarded != 0 {
		t.Fatalf("double-encoded path = %d forwards=%d, want 403/0", rr.Code, forwarded)
	}
}
