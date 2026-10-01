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
	logPath := filepath.Join(t.TempDir(), "audit.ndjson")
	p, err := NewProxy(Config{
		StoreBaseURL:   store.URL,
		ConsumerKey:    canaryKey,
		ConsumerSecret: canarySecret,
		AuditLogPath:   logPath,
		Now:            func() time.Time { return time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, logPath
}

// MUTANT: drop the credential injection (the query.Set lines) and this test
// goes red — the store sees no key and rejects with 401.
func TestProxyInjectsStoreKeyOnForward(t *testing.T) {
	var gotKey, gotSecret string
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("consumer_key")
		gotSecret = r.URL.Query().Get("consumer_secret")
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, _ := newTestProxy(t, store)

	resp := httptest.NewRequest(http.MethodGet, "/wp-json/wc/v3/products?per_page=1", nil)
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, resp)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if gotKey != canaryKey || gotSecret != canarySecret {
		t.Fatalf("proxy did not inject the store key: key=%q secret=%q", gotKey, gotSecret)
	}
}

// MUTANT: forward the caller's query untouched and a caller could smuggle
// credentials past the audit point; this test goes red because the store
// sees the smuggled key, not the proxy's.
func TestProxyStripsSmuggledCredentials(t *testing.T) {
	var gotKey string
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("consumer_key")
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, _ := newTestProxy(t, store)

	req := httptest.NewRequest(http.MethodGet, "/wp-json/wc/v3/products?consumer_key=ck_SMUGGLED", nil)
	p.ServeHTTP(httptest.NewRecorder(), req)
	if gotKey != canaryKey {
		t.Fatalf("smuggled key forwarded: %q", gotKey)
	}
}

// MUTANT: remove the approval check in ServeHTTP and the write is forwarded
// unauditable; this test goes red (200 instead of 403, and the store was hit).
func TestWriteWithoutApprovalRefused(t *testing.T) {
	forwarded := 0
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded++
		w.WriteHeader(http.StatusOK)
	}))
	defer store.Close()
	p, logPath := newTestProxy(t, store)

	req := httptest.NewRequest(http.MethodPut, "/wp-json/wc/v3/products/42", strings.NewReader(`{"stock_quantity":1}`))
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("write without approval = %d, want 403", rr.Code)
	}
	if forwarded != 0 {
		t.Fatalf("an unauditable write reached the store (%d forwards)", forwarded)
	}
	if rows := readLog(t, logPath); len(rows) != 0 {
		t.Fatalf("refused write must not be audited as a store write: %+v", rows)
	}
}

// MUTANT: log r.URL.String() instead of r.URL.Path and the canary leaks into
// the audit log; the canary assertion goes red.
func TestAuditRowCarriesApprovalAndStatusWithoutQuery(t *testing.T) {
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))
	defer store.Close()
	p, logPath := newTestProxy(t, store)

	req := httptest.NewRequest(http.MethodPost, "/wp-json/wc/v3/products?foo=bar", strings.NewReader(`{}`))
	req.Header.Set(ApprovalHeader, "product-publish-abc-123")
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status passthrough = %d, want 201", rr.Code)
	}
	rows := readLog(t, logPath)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Method != http.MethodPost || r.Path != "/wp-json/wc/v3/products" || r.ApprovalID != "product-publish-abc-123" || r.Status != http.StatusCreated {
		t.Fatalf("audit row wrong: %+v", r)
	}
	if blob, _ := os.ReadFile(logPath); strings.Contains(string(blob), canaryKey) || strings.Contains(string(blob), "foo=bar") {
		t.Fatalf("audit log must carry the path only, no query or credentials: %s", blob)
	}
}

// MUTANT: skip the fsync (or the audit call on the error path) and a
// forwarded write can be missing from the log the nightly join reads.
func TestFailedForwardWritesNoAuditRow(t *testing.T) {
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	p, logPath := newTestProxy(t, store)
	store.Close() // the store goes away AFTER the proxy was built

	req := httptest.NewRequest(http.MethodPost, "/wp-json/wc/v3/products", strings.NewReader(`{}`))
	req.Header.Set(ApprovalHeader, "ap-1")
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 when the store is down", rr.Code)
	}
	if rows := readLog(t, logPath); len(rows) != 0 {
		t.Fatalf("a forward that never reached the store must not be audited: %+v", rows)
	}
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
		if err := jsonUnmarshal(line, &r); err != nil {
			t.Fatalf("bad audit line %q: %v", line, err)
		}
		rows = append(rows, r)
	}
	return rows
}

func jsonUnmarshal(s string, v any) error {
	return json.NewDecoder(strings.NewReader(s)).Decode(v)
}
