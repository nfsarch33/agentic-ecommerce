package marketplace

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// EventName is a typed alias for an event-bus event identifier that a
// plugin manifest may subscribe to. Plugins consume only schemas they
// have explicitly named.
type EventName string

// Permission is a coarse capability flag the plugin requests at install
// time. Permissions are advisory in v2.4.0 and gate the routes/event
// subscriptions the sandbox will mount.
type Permission string

const (
	PermissionReadCatalog     Permission = "catalog.read"
	PermissionReadOrders      Permission = "orders.read"
	PermissionWriteOrders     Permission = "orders.write"
	PermissionReadMemberships Permission = "memberships.read"
	PermissionReadDigital     Permission = "digital.read"
	PermissionEmitEvents      Permission = "events.emit"
)

// DependencyRef is a (slug, semverConstraint) pair. The constraint
// supports the prefix forms `^X.Y.Z` (caret, compatible) and `=X.Y.Z`
// (exact). An empty constraint is treated as `^X.Y.Z` of the resolved
// version. Older `~`/`>=` patterns are deliberately out of scope for
// v2.4.0 to keep dependency.go small and idiomatic.
type DependencyRef struct {
	Slug       string `json:"slug"`
	Constraint string `json:"constraint"`
}

// Manifest is the typed plugin manifest that adapters store in
// marketplace_plugins. JSON tags match the on-the-wire shape used by
// /api/v1/marketplace/plugins.
type Manifest struct {
	Slug               string          `json:"slug"`
	Name               string          `json:"name"`
	Version            string          `json:"version"`
	Vendor             string          `json:"vendor"`
	Description        string          `json:"description,omitempty"`
	Category           string          `json:"category,omitempty"`
	EventSubscriptions []EventName     `json:"event_subscriptions,omitempty"`
	Permissions        []Permission    `json:"permissions,omitempty"`
	Dependencies       []DependencyRef `json:"dependencies,omitempty"`
	HomepageURL        string          `json:"homepage_url,omitempty"`
}

// slugPattern enforces kebab-case: lowercase, digits, hyphens; must
// start with a letter and end with a letter or digit.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// semverPattern enforces strict MAJOR.MINOR.PATCH (no pre-release,
// no build metadata). Sufficient for v2.4.0 and avoids depending on
// golang.org/x/mod/semver (which is not in go.mod).
var semverPattern = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)$`)

// constraintPattern accepts `^X.Y.Z`, `=X.Y.Z`, or empty. Empty is
// treated as caret in the matcher.
var constraintPattern = regexp.MustCompile(`^([\^=]?)([0-9]+)\.([0-9]+)\.([0-9]+)$`)

// IsValidSlug reports whether s matches the kebab-case rule used for
// plugin and tenant slugs.
func IsValidSlug(s string) bool {
	return slugPattern.MatchString(s)
}

// IsValidSemver reports whether s parses as MAJOR.MINOR.PATCH.
func IsValidSemver(s string) bool {
	return semverPattern.MatchString(s)
}

// Validate enforces the structural rules of a Manifest. It is the
// single gateway every Registry.Install path goes through.
//
// Cyclomatic complexity is intentionally kept under 10 by chaining
// helper validators that each handle one field family.
func (m Manifest) Validate() error {
	if err := m.validateIdentity(); err != nil {
		return err
	}
	if err := m.validateVersion(); err != nil {
		return err
	}
	if err := m.validateDependencies(); err != nil {
		return err
	}
	return nil
}

func (m Manifest) validateIdentity() error {
	if !IsValidSlug(m.Slug) {
		return fmt.Errorf("%w: slug=%q", ErrSlugInvalid, m.Slug)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("%w: name is empty", ErrManifestInvalid)
	}
	if strings.TrimSpace(m.Vendor) == "" {
		return fmt.Errorf("%w: vendor is empty", ErrManifestInvalid)
	}
	return nil
}

func (m Manifest) validateVersion() error {
	if !IsValidSemver(m.Version) {
		return fmt.Errorf("%w: version=%q", ErrSemverInvalid, m.Version)
	}
	return nil
}

func (m Manifest) validateDependencies() error {
	seen := make(map[string]struct{}, len(m.Dependencies))
	for _, dep := range m.Dependencies {
		if dep.Slug == m.Slug {
			return fmt.Errorf("%w: self-dependency %q", ErrManifestInvalid, dep.Slug)
		}
		if !IsValidSlug(dep.Slug) {
			return fmt.Errorf("%w: dep slug=%q", ErrSlugInvalid, dep.Slug)
		}
		if _, dup := seen[dep.Slug]; dup {
			return fmt.Errorf("%w: duplicate dependency %q", ErrManifestInvalid, dep.Slug)
		}
		seen[dep.Slug] = struct{}{}
		if dep.Constraint == "" {
			continue
		}
		if !constraintPattern.MatchString(dep.Constraint) {
			return fmt.Errorf("%w: dep constraint=%q", ErrSemverInvalid, dep.Constraint)
		}
	}
	return nil
}

// SemverParts is the parsed (major, minor, patch) triple used by the
// dependency resolver and the constraint matcher.
type SemverParts struct {
	Major int
	Minor int
	Patch int
}

// ParseSemver returns the parts of a semver string. Returns
// ErrSemverInvalid for malformed input.
func ParseSemver(s string) (SemverParts, error) {
	matches := semverPattern.FindStringSubmatch(s)
	if matches == nil {
		return SemverParts{}, fmt.Errorf("%w: %q", ErrSemverInvalid, s)
	}
	return semverPartsFromMatches(matches[1], matches[2], matches[3])
}

func semverPartsFromMatches(major, minor, patch string) (SemverParts, error) {
	parts, err := parseSemverParts(major, minor, patch)
	if err != nil {
		return SemverParts{}, err
	}
	return parts, nil
}

func parseSemverParts(major, minor, patch string) (SemverParts, error) {
	maj, err := strconv.Atoi(major)
	if err != nil {
		return SemverParts{}, fmt.Errorf("%w: major", ErrSemverInvalid)
	}
	min, err := strconv.Atoi(minor)
	if err != nil {
		return SemverParts{}, fmt.Errorf("%w: minor", ErrSemverInvalid)
	}
	pat, err := strconv.Atoi(patch)
	if err != nil {
		return SemverParts{}, fmt.Errorf("%w: patch", ErrSemverInvalid)
	}
	return SemverParts{Major: maj, Minor: min, Patch: pat}, nil
}

// ConstraintSatisfied reports whether candidate satisfies constraint.
// An empty constraint always returns true. The supported forms:
//
//	`^X.Y.Z`  caret -- candidate.major == X and candidate >= constraint
//	`=X.Y.Z`  exact -- candidate equals constraint
//	`X.Y.Z`   bare -- treated as caret (the common shorthand)
//
// This intentionally stays small. v2.5.0+ may extend to `~` / `>=`.
func ConstraintSatisfied(constraint, candidate string) (bool, error) {
	if constraint == "" {
		return true, nil
	}
	matches := constraintPattern.FindStringSubmatch(constraint)
	if matches == nil {
		return false, fmt.Errorf("%w: constraint=%q", ErrSemverInvalid, constraint)
	}
	op := matches[1]
	wanted, err := semverPartsFromMatches(matches[2], matches[3], matches[4])
	if err != nil {
		return false, err
	}
	got, err := ParseSemver(candidate)
	if err != nil {
		return false, err
	}
	if op == "=" {
		return got == wanted, nil
	}
	return caretSatisfied(wanted, got), nil
}

func caretSatisfied(wanted, got SemverParts) bool {
	if got.Major != wanted.Major {
		return false
	}
	if got.Minor < wanted.Minor {
		return false
	}
	if got.Minor == wanted.Minor && got.Patch < wanted.Patch {
		return false
	}
	return true
}
