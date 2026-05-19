package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrInvalidVersion = errors.New("invalid version string")

type VersionInfo struct {
	Major      int
	Minor      int
	Patch      int
	PreRelease string
	BuildMeta  string
}

func ParseVersion(s string) (VersionInfo, error) {
	// Strip leading 'v'
	s = strings.TrimPrefix(s, "v")
	// Split build meta
	var buildMeta string
	if idx := strings.Index(s, "+"); idx >= 0 {
		buildMeta = s[idx+1:]
		s = s[:idx]
	}
	// Split pre-release
	var preRelease string
	if idx := strings.Index(s, "-"); idx >= 0 {
		preRelease = s[idx+1:]
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return VersionInfo{}, ErrInvalidVersion
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	patch, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return VersionInfo{}, ErrInvalidVersion
	}
	return VersionInfo{Major: major, Minor: minor, Patch: patch, PreRelease: preRelease, BuildMeta: buildMeta}, nil
}

func (v VersionInfo) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.PreRelease != "" {
		s += "-" + v.PreRelease
	}
	if v.BuildMeta != "" {
		s += "+" + v.BuildMeta
	}
	return s
}

type ChangeEntry struct {
	Version string
	Changes []string
}

// changeLog is a minimal in-memory registry for testing.
var changeLog = []ChangeEntry{
	{Version: "v10.0.0", Changes: []string{"discount engine", "product variants", "review system"}},
	{Version: "v9.0.0", Changes: []string{"auth middleware", "webhook retry", "currency converter"}},
	{Version: "v8.0.0", Changes: []string{"cart abandonment", "inventory sync"}},
}

func Changelog(from, to string) ([]ChangeEntry, error) {
	fromV, err := ParseVersion(from)
	if err != nil {
		return nil, err
	}
	toV, err := ParseVersion(to)
	if err != nil {
		return nil, err
	}
	var result []ChangeEntry
	for _, e := range changeLog {
		v, err := ParseVersion(e.Version)
		if err != nil {
			continue
		}
		if versionGE(v, fromV) && versionLE(v, toV) {
			result = append(result, e)
		}
	}
	return result, nil
}

type FeatureSupport struct {
	Feature string
	Since   string
}

type Matrix struct {
	APIVersions map[string][]FeatureSupport
}

func CompatibilityMatrix() Matrix {
	return Matrix{
		APIVersions: map[string][]FeatureSupport{
			"v1": {{Feature: "cart", Since: "v8.0.0"}, {Feature: "auth", Since: "v9.0.0"}},
			"v2": {{Feature: "cart", Since: "v8.0.0"}, {Feature: "discount", Since: "v10.0.0"}},
		},
	}
}

func versionGE(a, b VersionInfo) bool {
	if a.Major != b.Major {
		return a.Major >= b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor >= b.Minor
	}
	return a.Patch >= b.Patch
}

func versionLE(a, b VersionInfo) bool {
	if a.Major != b.Major {
		return a.Major <= b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor <= b.Minor
	}
	return a.Patch <= b.Patch
}
