// Package api implements the v9.0.0 API version negotiation surface.
// It provides middleware and helpers that route incoming requests to
// the appropriate version handler and signal version semantics back
// to the client via response headers.
//
// Two versions ship in the v9.0.0 release baseline:
//
//	v1: stable through host v9.x. No breaking changes; new fields
//	    only. The canonical spec lives at api/openapi.yaml.
//
//	v2: preview. Subject to change without notice while clients
//	    pilot the next-generation response shapes. The spec lives
//	    at api/openapi-v2-preview.yaml. Every v2 response carries
//	    X-API-Version: 2-preview so clients can detect early.
//
// The negotiation surface is intentionally narrow: a client either
// hits an explicit /api/v1/... or /api/v2/... path, or supplies an
// Accept header that the middleware translates into the same routing
// decision via NegotiateVersion.
package api

import (
	"net/http"
	"strings"
)

// Version is the typed version identifier the middleware emits.
type Version string

const (
	// VersionV1 is the stable v1 API. Default when no Accept media
	// type opts into a newer version.
	VersionV1 Version = "1"
	// VersionV2Preview is the in-development preview API. Clients
	// must explicitly opt in via Accept header or by hitting an
	// /api/v2/... path; otherwise the v1 surface is used.
	VersionV2Preview Version = "2-preview"
)

// HeaderAPIVersion is the response header that documents which API
// version the handler used. Clients can pin themselves by inspecting
// this header.
const HeaderAPIVersion = "X-API-Version"

// HeaderAPIDeprecation is set on responses from preview endpoints to
// remind clients the surface may change. Empty for stable surfaces.
const HeaderAPIDeprecation = "X-API-Deprecation"

// MediaTypeV1 is the canonical v1 media type used in Accept negotiation.
const MediaTypeV1 = "application/vnd.ec.v1+json"

// MediaTypeV2Preview is the preview media type. Clients send this in
// Accept to opt into the v2 response shape when a v2 endpoint exists;
// the middleware falls back to v1 if no v2 handler is mounted.
const MediaTypeV2Preview = "application/vnd.ec.v2+json"

// NegotiateVersion inspects the Accept header and the URL path and
// returns the resolved Version. Path-based v2 opt-in
// (e.g. /api/v2/...) always wins, so a typo'd Accept header cannot
// silently downgrade an explicit v2 caller. A v1 path is considered
// non-opinionated -- the Accept header may still upgrade the response
// to v2 preview when both surfaces exist for the same logical path.
//
// Cyclomatic complexity stays under 10 by delegating each branch to a
// helper that returns true when it has a definitive answer.
func NegotiateVersion(r *http.Request) Version {
	if hasV2Path(r.URL.Path) {
		return VersionV2Preview
	}
	if v, ok := versionFromAccept(r.Header.Get("Accept")); ok {
		return v
	}
	return VersionV1
}

func hasV2Path(path string) bool {
	return strings.HasPrefix(path, "/api/v2/")
}

func versionFromAccept(accept string) (Version, bool) {
	if accept == "" {
		return "", false
	}
	for _, candidate := range strings.Split(accept, ",") {
		mediaType := strings.TrimSpace(candidate)
		if i := strings.Index(mediaType, ";"); i != -1 {
			mediaType = strings.TrimSpace(mediaType[:i])
		}
		switch mediaType {
		case MediaTypeV2Preview:
			return VersionV2Preview, true
		case MediaTypeV1:
			return VersionV1, true
		}
	}
	return "", false
}

// VersionHeader writes the appropriate X-API-Version (and
// X-API-Deprecation for preview surfaces) onto the response. Handlers
// invoke this once they know which version they served.
func VersionHeader(w http.ResponseWriter, v Version) {
	w.Header().Set(HeaderAPIVersion, string(v))
	if v == VersionV2Preview {
		w.Header().Set(HeaderAPIDeprecation, "preview; semantics may change without notice")
	}
}

// WithVersionHeaders returns a middleware that auto-negotiates the
// request version and stamps the response with the matching headers
// before delegating to next.
//
// Handlers that need to branch on the version should call
// NegotiateVersion themselves; the middleware only handles the
// outbound stamping so 100% of v1/v2 responses carry consistent
// version metadata for client logging and analytics.
func WithVersionHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := NegotiateVersion(r)
		VersionHeader(w, v)
		next.ServeHTTP(w, r)
	})
}

// IsPreview reports whether the version is a non-stable preview that
// clients must explicitly opt into.
func (v Version) IsPreview() bool { return v == VersionV2Preview }
