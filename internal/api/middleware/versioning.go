package middleware

import (
	"net/http"
	"regexp"
	"sync"
)

var (
	acceptVersionRe = regexp.MustCompile(`application/vnd\.ec\.(v\d+)\+json`)
	urlVersionRe    = regexp.MustCompile(`/api/(v\d+)/`)
)

// VersionRouter routes HTTP requests to the correct versioned handler.
type VersionRouter struct {
	mu          sync.RWMutex
	handlers    map[string]http.Handler
	deprecated  map[string]bool
	versionList []string // ordered by registration time
}

func NewVersionRouter() *VersionRouter {
	return &VersionRouter{
		handlers:   make(map[string]http.Handler),
		deprecated: make(map[string]bool),
	}
}

// Register adds a handler for the given version string (e.g. "v2").
func (vr *VersionRouter) Register(version string, h http.Handler) {
	vr.mu.Lock()
	defer vr.mu.Unlock()
	vr.handlers[version] = h
	vr.versionList = append(vr.versionList, version)
}

// Deprecate marks a version as deprecated; responses will include a Deprecation header.
func (vr *VersionRouter) Deprecate(version string) {
	vr.mu.Lock()
	defer vr.mu.Unlock()
	vr.deprecated[version] = true
}

// Handler returns an http.Handler that dispatches to the correct version.
func (vr *VersionRouter) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := extractVersion(r)

		vr.mu.RLock()
		h, ok := vr.handlers[version]
		latest := vr.latestVersion()
		deprecated := vr.deprecated[version]
		vr.mu.RUnlock()

		if version == "" {
			// fallback to latest
			vr.mu.RLock()
			h = vr.handlers[latest]
			vr.mu.RUnlock()
			if h == nil {
				http.NotFound(w, r)
				return
			}
			h.ServeHTTP(w, r)
			return
		}

		if !ok {
			http.NotFound(w, r)
			return
		}
		if deprecated {
			w.Header().Set("Deprecation", "true")
		}
		h.ServeHTTP(w, r)
	})
}

func extractVersion(r *http.Request) string {
	// 1. Accept header: application/vnd.ec.v2+json
	if m := acceptVersionRe.FindStringSubmatch(r.Header.Get("Accept")); len(m) == 2 {
		return m[1]
	}
	// 2. URL path: /api/v1/...
	if m := urlVersionRe.FindStringSubmatch(r.URL.Path); len(m) == 2 {
		return m[1]
	}
	return ""
}

// latestVersion returns the last registered version (highest index = latest).
func (vr *VersionRouter) latestVersion() string {
	if len(vr.versionList) == 0 {
		return ""
	}
	return vr.versionList[len(vr.versionList)-1]
}
