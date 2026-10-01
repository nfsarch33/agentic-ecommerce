// Package storeproxy is the counting egress proxy in front of every store
// REST API (v18900-5). It holds the store REST key — callers reach the store
// through it with NO credentials of their own, so a direct caller gets 401
// from the store and every write is attributable to an approval row.
//
// Fail-closed by construction: the proxy refusing to start (or being stopped)
// means no request leaves the worker at all.
package storeproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// AuditRow is one NDJSON line per forwarded request. The PATH ONLY never
// carries the query string: the query is where WooCommerce credentials ride,
// so the log must not contain them (acceptance 4).
type AuditRow struct {
	Time       string `json:"ts"`
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

func isWrite(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// ServeHTTP forwards one request. Reads pass through; writes require the
// approval header. Incoming credential query parameters are stripped before
// the proxy's own are injected, so a caller cannot smuggle credentials past
// the audit point.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	approval := strings.TrimSpace(r.Header.Get(ApprovalHeader))
	if isWrite(r.Method) && approval == "" {
		http.Error(w, ErrWriteWithoutApproval.Error(), http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "storeproxy: read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	target, err := url.Parse(strings.TrimRight(p.cfg.StoreBaseURL, "/") + r.URL.Path)
	if err != nil {
		http.Error(w, "storeproxy: bad target: "+err.Error(), http.StatusBadGateway)
		return
	}
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
	req.Header.Del(ApprovalHeader)

	resp, err := p.client.Do(req)
	if err != nil {
		// The forward failed (store unreachable). No audit row: nothing
		// reached the store, and the join must not count a phantom write.
		http.Error(w, "storeproxy: forward: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	p.audit(AuditRow{
		Time:       p.cfg.Now().UTC().Format(time.RFC3339Nano),
		Method:     r.Method,
		Path:       r.URL.Path,
		ApprovalID: approval,
		Status:     resp.StatusCode,
	})

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// audit appends one row, fsync'd: a crash must not lose the record of a
// write that already reached the store.
func (p *Proxy) audit(row AuditRow) {
	line, err := json.Marshal(row)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.log == nil {
		return
	}
	if _, err := p.log.Write(append(line, '\n')); err == nil {
		_ = p.log.Sync()
	}
}
