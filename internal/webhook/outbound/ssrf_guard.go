package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// ErrSSRFBlocked is returned when an outbound URL fails the SSRF
// guard (private IP range, cloud metadata IP, disallowed scheme).
var ErrSSRFBlocked = errors.New("ssrf guard blocked outbound request")

// SSRFGuardConfig tunes the SSRF guard. Zero values pick up the
// production-friendly defaults so the bare `NewSSRFGuard(nil)` call
// works.
type SSRFGuardConfig struct {
	// AllowInsecureHTTP, when true, permits http:// targets. Defaults
	// to false; the API surface only allows https:// outbound.
	AllowInsecureHTTP bool
	// Resolver overrides the DNS resolver. Defaults to net.DefaultResolver.
	Resolver Resolver
}

// Resolver mirrors the subset of net.Resolver we use. Tests inject a
// stub that returns deterministic IPs without touching DNS.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// SSRFGuard enforces the v2.8.0 OWASP A10 (SSRF) mitigation on every
// outbound HTTP target. Construction is cheap; goroutine-safe.
type SSRFGuard struct {
	cfg      SSRFGuardConfig
	resolver Resolver
	permit   bool
}

// NewSSRFGuard returns a guard with the supplied configuration. Pass
// nil cfg for production defaults (https-only, system resolver).
func NewSSRFGuard(cfg *SSRFGuardConfig) *SSRFGuard {
	resolved := SSRFGuardConfig{}
	if cfg != nil {
		resolved = *cfg
	}
	resolver := resolved.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &SSRFGuard{cfg: resolved, resolver: resolver}
}

// NewPermissiveSSRFGuard returns a guard that bypasses every check.
// Tests that point at httptest.Server (loopback) inject this so the
// outbound client wiring works without compromising the strict
// default for production callers.
func NewPermissiveSSRFGuard() *SSRFGuard {
	return &SSRFGuard{permit: true}
}

// CheckURL inspects the supplied raw URL. Returns ErrSSRFBlocked when
// any rule trips. Cyclomatic complexity stays under 10 by delegating
// to focused helpers.
func (g *SSRFGuard) CheckURL(ctx context.Context, raw string) error {
	if g.permit {
		return nil
	}
	parsed, err := parseAndValidateScheme(raw, g.cfg.AllowInsecureHTTP)
	if err != nil {
		return err
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrSSRFBlocked)
	}
	addrs, err := g.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: dns lookup %s: %v", ErrSSRFBlocked, host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%w: no addresses for %s", ErrSSRFBlocked, host)
	}
	for _, addr := range addrs {
		if reason := classifyIP(addr.IP); reason != "" {
			return fmt.Errorf("%w: %s resolves to %s (%s)", ErrSSRFBlocked, host, addr.IP, reason)
		}
	}
	return nil
}

func parseAndValidateScheme(raw string, allowInsecure bool) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: parse url: %v", ErrSSRFBlocked, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "https":
		return parsed, nil
	case "http":
		if !allowInsecure {
			return nil, fmt.Errorf("%w: http scheme requires ALLOW_INSECURE_OUTBOUND_WEBHOOK", ErrSSRFBlocked)
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("%w: scheme %q not allowed", ErrSSRFBlocked, parsed.Scheme)
	}
}

// classifyIP returns a human-readable reason if the IP is in a
// blocked range. Empty string means the IP is acceptable.
func classifyIP(ip net.IP) string {
	if ip == nil {
		return "nil ip"
	}
	if ip.IsUnspecified() {
		return "unspecified"
	}
	if ip.IsLoopback() {
		return "loopback"
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return "link-local"
	}
	if ip.IsPrivate() {
		return "private"
	}
	if isCloudMetadataIP(ip) {
		return "cloud metadata"
	}
	if isIPv6UniqueLocal(ip) {
		return "ipv6 unique-local"
	}
	if isIPv4Multicast(ip) {
		return "multicast"
	}
	return ""
}

// cloudMetadataIPs lists the well-known IMDS / ECS task metadata
// addresses that must never be reachable from a tenant-supplied URL.
var cloudMetadataIPs = []string{
	"169.254.169.254",                     // AWS, Azure, GCP IMDS
	"169.254.170.2",                       // ECS task metadata
	net.IPv4(100, 100, 100, 200).String(), // Alibaba Cloud
	"fd00:ec2::254",                       // IPv6 IMDS
}

var (
	cloudMetadataOnce sync.Once
	cloudMetadataSet  map[string]struct{}
)

func isCloudMetadataIP(ip net.IP) bool {
	cloudMetadataOnce.Do(func() {
		cloudMetadataSet = make(map[string]struct{}, len(cloudMetadataIPs))
		for _, raw := range cloudMetadataIPs {
			if parsed := net.ParseIP(raw); parsed != nil {
				cloudMetadataSet[parsed.String()] = struct{}{}
			}
		}
	})
	_, ok := cloudMetadataSet[ip.String()]
	return ok
}

// isIPv6UniqueLocal mirrors net.IsPrivate's handling of fc00::/7,
// which the stdlib only treats as private when running on Go 1.21+.
// We re-implement it to keep the rule explicit and stable across
// Go releases.
func isIPv6UniqueLocal(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		return false
	}
	return len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc
}

func isIPv4Multicast(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] >= 224 && ip4[0] <= 239
}

// HTTPDoerWithGuard wraps an HTTPDoer so every outbound request is
// gated by the SSRF guard. Requests whose URL fails the guard return
// ErrSSRFBlocked without ever reaching the network.
type HTTPDoerWithGuard struct {
	inner HTTPDoer
	guard *SSRFGuard
}

// NewHTTPDoerWithGuard returns a doer that delegates to inner only
// after guard.CheckURL succeeds.
func NewHTTPDoerWithGuard(inner HTTPDoer, guard *SSRFGuard) *HTTPDoerWithGuard {
	if inner == nil {
		inner = &http.Client{}
	}
	if guard == nil {
		guard = NewSSRFGuard(nil)
	}
	return &HTTPDoerWithGuard{inner: inner, guard: guard}
}

// Do checks the request URL and either forwards to the inner doer or
// returns ErrSSRFBlocked. Implements HTTPDoer.
func (h *HTTPDoerWithGuard) Do(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("%w: nil request", ErrSSRFBlocked)
	}
	if err := h.guard.CheckURL(req.Context(), req.URL.String()); err != nil {
		return nil, err
	}
	return h.inner.Do(req)
}
