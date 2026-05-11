// File scope: v3.5.0 EC-6-2 FXRateProvider adapters.
//
// Two adapters ship in v3.5.0:
//
//   - StaticFXRateProvider: in-memory fixture used by tests + the
//     development bootstrap (operator can ship a static rate while
//     wiring the live source).
//   - FXRateFileCacheProvider: reads a JSON cache file written by
//     a daily refresh script. The HTTP fetcher is deferred to v3.5.1
//     per the plan ("stub HTTP client + file-backed cache for now").
//     The cache shape is OPEN so a future RBA / fawazahmed0 fetcher
//     can write to it without code changes.
//
// The plan says env var ECOMMERCE_FX_RATE_API_URL feeds the future
// HTTP fetcher; the file cache here uses ECOMMERCE_FX_RATE_CACHE_PATH
// when callers want env-driven config (otherwise pass the path
// explicitly to the constructor).
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FXCacheEnvVar is the env var the optional file-cache provider reads
// when callers do not supply an explicit path. Documented for cmd/*
// composition roots so the launchd plist / docker compose env block
// has one canonical name.
const FXCacheEnvVar = "ECOMMERCE_FX_RATE_CACHE_PATH"

// FXAPIURLEnvVar is the env var reserved for the v3.5.1 live HTTP
// fetcher (per ADR-028 "RBA published rates OR free
// fawazahmed0/exchange-api"). The v3.5.0 calculator does NOT call
// this URL; the env var is documented here so the contract is fixed.
const FXAPIURLEnvVar = "ECOMMERCE_FX_RATE_API_URL"

// StaticFXRateProvider is the in-memory FXRateProvider used by
// tests + the bootstrap path. Goroutine-safe: the rate is set at
// construction and never mutated.
type StaticFXRateProvider struct {
	rate FXRate
}

// NewStaticFXRateProvider returns a provider that always reports
// the supplied rate. Empty FetchedAt -> stamped to time.Now().UTC()
// so callers cannot accidentally produce a stale rate by forgetting
// the timestamp.
func NewStaticFXRateProvider(rate FXRate) *StaticFXRateProvider {
	if rate.FetchedAt.IsZero() {
		rate.FetchedAt = time.Now().UTC()
	}
	if rate.Source == "" {
		rate.Source = "static"
	}
	return &StaticFXRateProvider{rate: rate}
}

// LatestRate implements FXRateProvider.
func (p *StaticFXRateProvider) LatestRate(_ context.Context) (FXRate, error) {
	return p.rate, nil
}

// FXRateCacheRecord is the JSON shape persisted on disk by the
// file-cache provider. Open enough that a future v3.5.1 HTTP fetcher
// can write the same shape.
type FXRateCacheRecord struct {
	AUDPerCNY float64   `json:"aud_per_cny"`
	FetchedAt time.Time `json:"fetched_at"`
	Source    string    `json:"source"`
}

// FXRateFileCacheProvider reads an FXRateCacheRecord from disk.
// Goroutine-safe via an internal RWMutex around the cached value;
// the file is reread when the mtime changes (so an external daily
// refresh script's write is picked up without a process restart).
type FXRateFileCacheProvider struct {
	path string

	mu       sync.RWMutex
	cached   FXRate
	cacheMod time.Time
}

// NewFXRateFileCacheProvider constructs a provider. The path may
// be empty; in that case the FXCacheEnvVar fallback is consulted.
// Returns an error when the path resolves empty AND the env var is
// also unset.
func NewFXRateFileCacheProvider(path string) (*FXRateFileCacheProvider, error) {
	if path == "" {
		path = os.Getenv(FXCacheEnvVar)
	}
	if path == "" {
		return nil, fmt.Errorf("%w: cache path empty (set %s or pass explicit path)", ErrFXRateUnconfigured, FXCacheEnvVar)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("billing: fx cache abs path: %w", err)
	}
	return &FXRateFileCacheProvider{path: abs}, nil
}

// Path returns the resolved cache file path. Useful for diagnostics.
func (p *FXRateFileCacheProvider) Path() string { return p.path }

// LatestRate implements FXRateProvider. Caches the parsed value
// keyed by file mtime so a subsequent call is allocation-free
// when the cache hasn't changed.
func (p *FXRateFileCacheProvider) LatestRate(_ context.Context) (FXRate, error) {
	info, err := os.Stat(p.path)
	if err != nil {
		return FXRate{}, fmt.Errorf("billing: fx cache stat: %w", err)
	}
	mod := info.ModTime()
	p.mu.RLock()
	if !p.cacheMod.IsZero() && p.cacheMod.Equal(mod) {
		out := p.cached
		p.mu.RUnlock()
		return out, nil
	}
	p.mu.RUnlock()
	rate, err := p.readFromDisk()
	if err != nil {
		return FXRate{}, err
	}
	p.mu.Lock()
	p.cached = rate
	p.cacheMod = mod
	p.mu.Unlock()
	return rate, nil
}

// readFromDisk loads + decodes the cache file. Pulled out so the
// LatestRate body stays small.
func (p *FXRateFileCacheProvider) readFromDisk() (FXRate, error) {
	f, err := os.Open(p.path)
	if err != nil {
		return FXRate{}, fmt.Errorf("billing: fx cache open: %w", err)
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		return FXRate{}, fmt.Errorf("billing: fx cache read: %w", err)
	}
	var rec FXRateCacheRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		return FXRate{}, fmt.Errorf("billing: fx cache decode: %w", err)
	}
	return FXRate{
		AUDPerCNY: rec.AUDPerCNY,
		FetchedAt: rec.FetchedAt,
		Source:    rec.Source,
	}, nil
}

// WriteFXRateCacheFile is a small helper for tests + the
// to-be-written v3.5.1 daily refresh script. Writes the supplied
// rate to disk in the FXRateCacheRecord JSON shape.
func WriteFXRateCacheFile(path string, rate FXRate) error {
	if path == "" {
		return errors.New("billing: fx cache write: empty path")
	}
	if rate.AUDPerCNY <= 0 {
		return fmt.Errorf("%w: rate non-positive", ErrInvalidFXRate)
	}
	if rate.FetchedAt.IsZero() {
		rate.FetchedAt = time.Now().UTC()
	}
	if rate.Source == "" {
		rate.Source = "operator"
	}
	rec := FXRateCacheRecord{
		AUDPerCNY: rate.AUDPerCNY,
		FetchedAt: rate.FetchedAt.UTC(),
		Source:    rate.Source,
	}
	body, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("billing: fx cache encode: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("billing: fx cache mkdir: %w", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("billing: fx cache write: %w", err)
	}
	return nil
}

// httpFXRateClient is a typed seam for the v3.5.1 live fetcher.
// v3.5.0 ships the constructor + signature so the operator setup
// has stable surface; the actual fetch is intentionally deferred
// (not wired into LatestRate). The seam keeps godoc + the env var
// contract alive without expanding shellable code paths.
type httpFXRateClient struct { //nolint:unused // v3.5.1 seam
	url    string
	client *http.Client
}

// newHTTPFXRateClient returns the v3.5.1 seam. Kept package-private
// so the public API in v3.5.0 stays "static + file cache only".
func newHTTPFXRateClient(url string, timeout time.Duration) *httpFXRateClient { //nolint:unused // v3.5.1 seam
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &httpFXRateClient{
		url:    url,
		client: &http.Client{Timeout: timeout},
	}
}
