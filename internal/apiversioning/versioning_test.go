package apiversioning

import (
	"testing"
	"time"
)

func TestVersion_String(t *testing.T) {
	t.Parallel()

	v := Version{Major: 2, Minor: 1}
	if got := v.String(); got != "v2.1" {
		t.Errorf("String() = %q, want %q", got, "v2.1")
	}
}

func TestRegistry_RegisterGetLatest(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(Version{Major: 1, Minor: 0})
	r.Register(Version{Major: 2, Minor: 0})
	r.Register(Version{Major: 2, Minor: 1})

	v, err := r.Get(2, 0)
	if err != nil {
		t.Fatalf("Get(2,0): %v", err)
	}
	if v.Major != 2 || v.Minor != 0 {
		t.Errorf("Get(2,0) = %v", v)
	}

	latest := r.Latest()
	if latest.Major != 2 || latest.Minor != 1 {
		t.Errorf("Latest() = %v, want v2.1", latest)
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	_, err := r.Get(99, 0)
	if err == nil {
		t.Error("expected error for unknown version")
	}
}

func TestNegotiator_AcceptHeader(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(Version{Major: 2, Minor: 0})
	r.Register(Version{Major: 3, Minor: 1})

	n := Negotiator{Registry: r}

	cases := []struct {
		header string
		major  int
		minor  int
	}{
		{"application/vnd.helixon.v2+json", 2, 0},
		{"application/vnd.helixon.v3.1+json", 3, 1},
		{"v2", 2, 0},
		{"v3.1", 3, 1},
	}

	for _, c := range cases {
		v, err := n.Negotiate(c.header)
		if err != nil {
			t.Errorf("Negotiate(%q): %v", c.header, err)
			continue
		}
		if v.Major != c.major || v.Minor != c.minor {
			t.Errorf("Negotiate(%q) = v%d.%d, want v%d.%d", c.header, v.Major, v.Minor, c.major, c.minor)
		}
	}
}

func TestNegotiator_UnknownVersion(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(Version{Major: 1, Minor: 0})
	n := Negotiator{Registry: r}

	_, err := n.Negotiate("v99")
	if err == nil {
		t.Error("expected error for unknown version")
	}
}

func TestDeprecationHeaders(t *testing.T) {
	t.Parallel()

	sunset := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	v := Version{Major: 1, Minor: 0, Deprecated: true, SunsetAt: &sunset}

	headers := DeprecationHeaders(v)
	if headers["Deprecation"] != "true" {
		t.Errorf("Deprecation header = %q, want true", headers["Deprecation"])
	}
	if _, ok := headers["Sunset"]; !ok {
		t.Error("Sunset header missing")
	}
}

func TestDeprecationHeaders_NotDeprecated(t *testing.T) {
	t.Parallel()

	v := Version{Major: 2, Minor: 0, Deprecated: false}
	headers := DeprecationHeaders(v)
	if len(headers) != 0 {
		t.Errorf("expected empty headers, got %v", headers)
	}
}
