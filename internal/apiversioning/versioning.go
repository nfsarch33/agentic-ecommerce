package apiversioning

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Version represents an API version.
type Version struct {
	Major      int
	Minor      int
	Deprecated bool
	SunsetAt   *time.Time
}

// String returns the version in "vMAJOR.MINOR" format.
func (v Version) String() string {
	return fmt.Sprintf("v%d.%d", v.Major, v.Minor)
}

// Registry is a thread-safe store of API versions.
type Registry struct {
	mu       sync.RWMutex
	versions map[string]Version // key: "MAJOR.MINOR"
}

// NewRegistry returns an initialised Registry.
func NewRegistry() *Registry {
	return &Registry{versions: make(map[string]Version)}
}

func versionKey(major, minor int) string {
	return strconv.Itoa(major) + "." + strconv.Itoa(minor)
}

// Register stores a version.
func (r *Registry) Register(v Version) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.versions[versionKey(v.Major, v.Minor)] = v
}

// Get retrieves a version by major and minor number.
func (r *Registry) Get(major, minor int) (*Version, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.versions[versionKey(major, minor)]
	if !ok {
		return nil, fmt.Errorf("apiversioning: version v%d.%d not found", major, minor)
	}
	cp := v
	return &cp, nil
}

// Latest returns the highest registered version (largest major, then minor).
func (r *Registry) Latest() Version {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest Version
	first := true
	for _, v := range r.versions {
		if first || v.Major > latest.Major || (v.Major == latest.Major && v.Minor > latest.Minor) {
			latest = v
			first = false
		}
	}
	return latest
}

// Negotiator negotiates a version from an Accept header.
type Negotiator struct {
	Registry *Registry
}

// Negotiate parses the acceptHeader and returns the requested Version.
// Supported formats:
//   - "application/vnd.helixon.v2+json"
//   - "application/vnd.helixon.v2.1+json"
//   - "v2"
//   - "v2.1"
func (n Negotiator) Negotiate(acceptHeader string) (Version, error) {
	major, minor, err := parseVersionStr(acceptHeader)
	if err != nil {
		return Version{}, fmt.Errorf("apiversioning: cannot negotiate from %q: %w", acceptHeader, err)
	}
	v, err := n.Registry.Get(major, minor)
	if err != nil {
		return Version{}, err
	}
	return *v, nil
}

// parseVersionStr extracts major and minor from various version string formats.
func parseVersionStr(s string) (major, minor int, err error) {
	// Strip MIME type wrapper: "application/vnd.helixon.v2+json" -> "v2"
	if idx := strings.Index(s, "vnd.helixon."); idx >= 0 {
		s = s[idx+len("vnd.helixon."):]
		if plus := strings.Index(s, "+"); plus >= 0 {
			s = s[:plus]
		}
	}

	// Strip leading "v".
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 2)

	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, errors.New("invalid major version: " + parts[0])
	}
	if len(parts) == 1 {
		return maj, 0, nil
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, errors.New("invalid minor version: " + parts[1])
	}
	return maj, min, nil
}

// DeprecationHeaders returns HTTP headers for a deprecated version.
func DeprecationHeaders(v Version) map[string]string {
	headers := make(map[string]string)
	if !v.Deprecated {
		return headers
	}
	headers["Deprecation"] = "true"
	if v.SunsetAt != nil {
		headers["Sunset"] = v.SunsetAt.UTC().Format(time.RFC1123)
	}
	return headers
}
