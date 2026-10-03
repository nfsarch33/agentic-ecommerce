// Package storeproxy is the counting egress proxy in front of every store
// REST API. It holds the store REST key — callers reach the store through it
// with NO credentials of their own, so a direct caller gets 401 from the
// store and every write is attributable to an approval row.
//
// Fail-closed by construction: the proxy refusing to start (or being stopped)
// means no request leaves the worker at all, and a write whose audit intent
// cannot be recorded is refused before it is forwarded.
package storeproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ApprovalHeader carries the approval row id on every write. The gate sets it
// from the publish context; the proxy refuses writes without it.
const ApprovalHeader = "X-Approval-Id"

// ErrWriteWithoutApproval is returned (HTTP 403) when a write arrives without
// an approval id: an unauditable write must never reach the store.
var ErrWriteWithoutApproval = errors.New("storeproxy: write without approval id")

// ErrPathNotAllowed is returned (HTTP 403) for any path outside the REST
// allowlist: the proxy's key must not open the whole store origin.
var ErrPathNotAllowed = errors.New("storeproxy: path outside the store REST allowlist")

// ErrAuditUnavailable is returned (HTTP 503) when a write's audit intent
// cannot be recorded: the write is refused rather than unauditable.
var ErrAuditUnavailable = errors.New("storeproxy: audit log unavailable, write refused (fail closed)")

// ErrFormBodyOnWrite is returned (HTTP 403) when a write carries a form (or
// undeclared) body: forms are the second carrier for the WordPress globals
// the query guard refuses — $_POST is where rest_route wins on a write.
var ErrFormBodyOnWrite = errors.New("storeproxy: write bodies must be JSON")

// allowedPrefixes are the REST prefixes the worker's adapter uses (the
// WooCommerce v3 REST API only). Anything else on the store origin — other
// API versions, admin pages, settings — is not the proxy's business and is
// refused with an audit row.
var allowedPrefixes = []string{"/wp-json/wc/v3/"}

// scrubbedHeaders are never forwarded: caller credentials and hop-by-hop
// state. The store must see ONLY the proxy's own credentials.
var scrubbedHeaders = []string{
	ApprovalHeader,
	"Authorization",
	"Cookie",
	"Proxy-Authorization",
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
	// Method-override headers: the store's REST layer honours them as
	// global parameters, so an unapproved "read" becomes a write there.
	"X-HTTP-Method-Override",
	"X-HTTP-Method",
	"X-Method-Override",
}

// AuditRow is one NDJSON line. The PATH ONLY never carries the query string:
// the query is where store credentials ride, so the log must not contain
// them.
//
// Write attempts are recorded WRITE-AHEAD: a write first appends a row with
// Kind "intent" (Status 0) and the write is refused unless that append and
// its fsync succeed — a crash between the store applying a write and its
// completion row can then never hide the write. The outcome row afterwards
// carries Kind "done" (store answered) or "unknown" (the forward errored
// after the request left — a timeout or reset: the write MAY be applied, the
// nightly join counts the intent and a human reads the outcome). Refusals
// carry Kind "refused". Only "intent" rows count as writes in the join.
type AuditRow struct {
	Time       string `json:"ts"`
	Kind       string `json:"kind"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	ApprovalID string `json:"approval_id,omitempty"`
	Status     int    `json:"status"`
}

// Config for the proxy handler.
type Config struct {
	// StoreBaseURL is the origin the proxy forwards to (no trailing slash).
	StoreBaseURL string
	// ConsumerKey/ConsumerSecret are the store REST credentials the proxy
	// holds and injects on forward. Callers never see them.
	ConsumerKey    string
	ConsumerSecret string
	// AuditLogPath is the NDJSON audit log (append, fsync per row).
	AuditLogPath string
	// Now is the clock (tests inject).
	Now func() time.Time
}

// Proxy forwards requests to the store, injecting credentials and auditing.
type Proxy struct {
	cfg    Config
	client *http.Client

	mu  sync.Mutex
	log *os.File
}

// NewProxy opens the audit log for append and returns the proxy.
func NewProxy(cfg Config) (*Proxy, error) {
	if cfg.StoreBaseURL == "" {
		return nil, errors.New("storeproxy: store base URL is required")
	}
	if cfg.ConsumerKey == "" || cfg.ConsumerSecret == "" {
		return nil, errors.New("storeproxy: the proxy must hold the store REST key (fail closed, not open)")
	}
	if cfg.AuditLogPath == "" {
		return nil, errors.New("storeproxy: audit log path is required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(cfg.AuditLogPath), 0o755); err != nil {
		return nil, fmt.Errorf("storeproxy: audit log dir: %w", err)
	}
	f, err := os.OpenFile(cfg.AuditLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("storeproxy: open audit log: %w", err)
	}
	return &Proxy{cfg: cfg, log: f, client: &http.Client{Timeout: 60 * time.Second}}, nil
}

// Close releases the audit log.
func (p *Proxy) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.log == nil {
		return nil
	}
	err := p.log.Close()
	p.log = nil
	return err
}

// methodClass is an ALLOWLIST, not a denylist: a verb the proxy does not
// know (PURGE, PROPFIND, anything custom) is refused, not treated as a
// read — unknown verbs are how a write slips past a denylist.
func methodClass(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead:
		return "read"
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return "write"
	}
	return "reject"
}

func isWrite(method string) bool { return methodClass(method) == "write" }

// phpKey mirrors how PHP mangles incoming parameter names before WordPress
// reads them: every dot and space in the name becomes an underscore, so
// ".method", "%20method" and a plain "_method" all arrive as the _method
// global. Matching the raw key alone sees none of the first two.
func phpKey(k string) string {
	k = strings.ReplaceAll(k, ".", "_")
	return strings.ReplaceAll(k, " ", "_")
}

// reservedWPGlobals are the WordPress/REST globals that re-verb or reroute a
// request after the proxy has classified it (_method overrides the verb,
// rest_route replaces the route, _envelope/_jsonp reshape the response).
var reservedWPGlobals = map[string]bool{
	"_method":    true,
	"rest_route": true,
	"_envelope":  true,
	"_jsonp":     true,
}

// queryKeyShape is the strict WooCommerce argument shape: lowercase letters,
// digits and underscores, with optional single-level array suffixes. Anything
// else in a query key is not a store argument and gets refused.
var queryKeyShape = regexp.MustCompile(`^[a-z0-9_]+(\[[a-z0-9_]*\])*$`)

// queryGuard is the single admission check for query keys: normalised the
// way PHP will normalise them, a key must be a plain store argument — not a
// reserved global and not a foreign shape.
func queryGuard(q url.Values) bool {
	for k := range q {
		nk := phpKey(k)
		if reservedWPGlobals[nk] || !queryKeyShape.MatchString(nk) {
			return false
		}
	}
	return true
}

// writeBodyAllowed refuses the two body classes PHP parses into $_POST on
// the store — form-urlencoded and multipart — because $_POST is where the
// reserved globals the query guard refuses get a second chance on a write.
// With no declared Content-Type the store leaves the body in php://input and
// $_POST stays empty, so an undeclared body carries nothing.
func writeBodyAllowed(contentType string) bool {
	if contentType == "" {
		return true
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false // unparseable type: refuse rather than guess
	}
	return mt != "application/x-www-form-urlencoded" && !strings.HasPrefix(mt, "multipart/")
}

func pathAllowed(path string) bool {
	if strings.Contains(path, "..") {
		return false
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// pathUnescaped reports whether the wire path carries any percent-encoding
// the decoded check cannot see (%2e%2e passes the ".." test as a decoded
// string while a front server may normalise it back to .. on the wire).
func pathUnescaped(rawPath, decodedPath string) bool {
	return rawPath != "" && rawPath != decodedPath || strings.Contains(decodedPath, "%")
}

func (p *Proxy) row(kind, method, path, approval string, status int) AuditRow {
	return AuditRow{
		Time:       p.cfg.Now().UTC().Format(time.RFC3339Nano),
		Kind:       kind,
		Method:     method,
		Path:       path,
		ApprovalID: approval,
		Status:     status,
	}
}

// ServeHTTP forwards one request. Reads are limited to the REST allowlist;
// writes additionally require the approval header and a fsync'd write-ahead
// intent row. Incoming credential query parameters AND credential headers
// are stripped before the proxy's own are set, so a caller cannot smuggle
// credentials past the audit point.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	approval := strings.TrimSpace(r.Header.Get(ApprovalHeader))
	path := r.URL.Path

	if !pathAllowed(path) || pathUnescaped(r.URL.RawPath, path) {
		p.audit(p.row("refused", r.Method, path, approval, http.StatusForbidden))
		http.Error(w, ErrPathNotAllowed.Error(), http.StatusForbidden)
		return
	}
	// The store's REST layer honours the _method query parameter and the
	// override headers as GLOBAL parameters: an unapproved GET becomes a
	// DELETE there while the proxy audits a read. Keys are matched after
	// PHP name-mangling (".method" and "%20method" are _method by the time
	// WordPress reads them), and the rest_route-family globals are refused
	// outright: they reroute the request off the audited path.
	if !queryGuard(r.URL.Query()) || r.Header.Get("X-HTTP-Method-Override") != "" ||
		r.Header.Get("X-HTTP-Method") != "" || r.Header.Get("X-Method-Override") != "" {
		p.audit(p.row("refused", r.Method, path, approval, http.StatusForbidden))
		http.Error(w, ErrWriteWithoutApproval.Error(), http.StatusForbidden)
		return
	}
	switch methodClass(r.Method) {
	case "reject":
		p.audit(p.row("refused", r.Method, path, approval, http.StatusMethodNotAllowed))
		http.Error(w, "storeproxy: method not allowed through the counting proxy", http.StatusMethodNotAllowed)
		return
	case "write":
		if approval == "" {
			p.audit(p.row("refused", r.Method, path, "", http.StatusForbidden))
			http.Error(w, ErrWriteWithoutApproval.Error(), http.StatusForbidden)
			return
		}
		if !writeBodyAllowed(r.Header.Get("Content-Type")) {
			p.audit(p.row("refused", r.Method, path, approval, http.StatusForbidden))
			http.Error(w, ErrFormBodyOnWrite.Error(), http.StatusForbidden)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "storeproxy: read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	// Write-ahead: the intent row exists, fsync'd, before the request
	// leaves. A write we cannot record is a write we refuse.
	if isWrite(r.Method) {
		if err := p.auditSync(p.row("intent", r.Method, path, approval, 0)); err != nil {
			http.Error(w, ErrAuditUnavailable.Error(), http.StatusServiceUnavailable)
			return
		}
	}

	base, err := url.Parse(strings.TrimRight(p.cfg.StoreBaseURL, "/"))
	if err != nil {
		http.Error(w, "storeproxy: bad store base: "+err.Error(), http.StatusBadGateway)
		return
	}
	target := *base
	target.Path = base.Path + path

	q := r.URL.Query()
	q.Del("consumer_key")
	q.Del("consumer_secret")
	q.Set("consumer_key", p.cfg.ConsumerKey)
	q.Set("consumer_secret", p.cfg.ConsumerSecret)
	target.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), strings.NewReader(string(body)))
	if err != nil {
		http.Error(w, "storeproxy: build forward: "+err.Error(), http.StatusBadGateway)
		return
	}
	req.Header = r.Header.Clone()
	for _, h := range scrubbedHeaders {
		req.Header.Del(h)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		// The request LEFT the proxy and the outcome is unknown (a timeout
		// may have applied the write server-side). The intent row already
		// counts the attempt; the unknown outcome is what a human reads.
		p.audit(p.row("unknown", r.Method, path, approval, 0))
		http.Error(w, "storeproxy: forward: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	p.audit(p.row("done", r.Method, path, approval, resp.StatusCode))

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// audit appends one row best-effort (outcome rows; the intent path uses
// auditSync because only intents gate writes).
func (p *Proxy) audit(row AuditRow) {
	_ = p.auditSync(row)
}

// auditSync appends one row and fsyncs it, returning the error so the
// write-ahead path can refuse the write.
func (p *Proxy) auditSync(row AuditRow) error {
	line, err := json.Marshal(row)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.log == nil {
		return errors.New("storeproxy: audit log closed")
	}
	if _, err := p.log.Write(append(line, '\n')); err != nil {
		return err
	}
	return p.log.Sync()
}
