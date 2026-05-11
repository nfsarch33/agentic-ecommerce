package outbound

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubResolver struct {
	hosts map[string][]net.IPAddr
	err   error
}

func (s *stubResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	if s.err != nil {
		return nil, s.err
	}
	if addrs, ok := s.hosts[host]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{Err: "stub: no such host", Name: host}
}

func ip(s string) net.IPAddr { return net.IPAddr{IP: net.ParseIP(s)} }

func TestSSRFGuardBlocksPrivateRanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ip   string
	}{
		{name: "rfc1918_10", ip: "10.0.0.5"},
		{name: "rfc1918_172", ip: "172.16.5.5"},
		{name: "rfc1918_192", ip: "192.168.1.1"},
		{name: "loopback_v4", ip: "127.0.0.1"},
		{name: "loopback_v6", ip: "::1"},
		{name: "linklocal_v4", ip: "169.254.10.10"},
		{name: "ipv6_ula_fc", ip: "fc00::1"},
		{name: "ipv6_ula_fd", ip: "fd00::1"},
		{name: "imds_v4", ip: "169.254.169.254"},
		{name: "ecs_metadata", ip: "169.254.170.2"},
		{name: "imds_v6", ip: "fd00:ec2::254"},
		{name: "alibaba_metadata", ip: net.IPv4(100, 100, 100, 200).String()},
		{name: "unspecified_v4", ip: "0.0.0.0"},
		{name: "ipv4_multicast", ip: "224.0.0.1"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			guard := NewSSRFGuard(&SSRFGuardConfig{
				Resolver: &stubResolver{hosts: map[string][]net.IPAddr{
					"target.example.com": {ip(tc.ip)},
				}},
			})
			err := guard.CheckURL(context.Background(), "https://target.example.com/path")
			if !errors.Is(err, ErrSSRFBlocked) {
				t.Fatalf("expected ErrSSRFBlocked for %s, got %v", tc.ip, err)
			}
		})
	}
}

func TestSSRFGuardAllowsPublicIP(t *testing.T) {
	t.Parallel()
	guard := NewSSRFGuard(&SSRFGuardConfig{
		Resolver: &stubResolver{hosts: map[string][]net.IPAddr{
			"public.example.com": {ip("8.8.8.8")},
		}},
	})
	if err := guard.CheckURL(context.Background(), "https://public.example.com/path"); err != nil {
		t.Fatalf("expected public IP to be allowed, got %v", err)
	}
}

func TestSSRFGuardRejectsHTTPByDefault(t *testing.T) {
	t.Parallel()
	guard := NewSSRFGuard(&SSRFGuardConfig{
		Resolver: &stubResolver{hosts: map[string][]net.IPAddr{
			"public.example.com": {ip("8.8.8.8")},
		}},
	})
	err := guard.CheckURL(context.Background(), "http://public.example.com/path")
	if !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("expected http to be blocked, got %v", err)
	}
}

func TestSSRFGuardAllowsHTTPWhenInsecureFlagSet(t *testing.T) {
	t.Parallel()
	guard := NewSSRFGuard(&SSRFGuardConfig{
		AllowInsecureHTTP: true,
		Resolver: &stubResolver{hosts: map[string][]net.IPAddr{
			"public.example.com": {ip("8.8.8.8")},
		}},
	})
	if err := guard.CheckURL(context.Background(), "http://public.example.com/path"); err != nil {
		t.Fatalf("expected http to be allowed with flag, got %v", err)
	}
}

func TestSSRFGuardRejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()
	guard := NewSSRFGuard(nil)
	err := guard.CheckURL(context.Background(), "file:///etc/passwd")
	if !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("expected file:// blocked, got %v", err)
	}
	err = guard.CheckURL(context.Background(), "gopher://example.com")
	if !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("expected gopher:// blocked, got %v", err)
	}
}

func TestSSRFGuardRejectsEmptyHost(t *testing.T) {
	t.Parallel()
	guard := NewSSRFGuard(nil)
	err := guard.CheckURL(context.Background(), "https:///path-only")
	if !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("expected empty host to be blocked, got %v", err)
	}
}

func TestSSRFGuardRejectsDNSFailure(t *testing.T) {
	t.Parallel()
	guard := NewSSRFGuard(&SSRFGuardConfig{
		Resolver: &stubResolver{err: errors.New("nope")},
	})
	err := guard.CheckURL(context.Background(), "https://target.example.com/path")
	if !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("expected dns failure to be blocked, got %v", err)
	}
}

func TestSSRFGuardRebindBlockedOnAnyResolvedIP(t *testing.T) {
	// DNS rebinding: the host returns one public + one private IP. A
	// naive check that picks just the first record would let the
	// connection through; the guard rejects when *any* resolved IP is
	// private to mitigate.
	t.Parallel()
	guard := NewSSRFGuard(&SSRFGuardConfig{
		Resolver: &stubResolver{hosts: map[string][]net.IPAddr{
			"rebind.example.com": {ip("8.8.8.8"), ip("169.254.169.254")},
		}},
	})
	err := guard.CheckURL(context.Background(), "https://rebind.example.com/x")
	if !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("expected rebind to be blocked, got %v", err)
	}
}

type stubDoer struct {
	resp *http.Response
	err  error
	hits int
}

func (s *stubDoer) Do(req *http.Request) (*http.Response, error) {
	s.hits++
	return s.resp, s.err
}

func TestHTTPDoerWithGuardBlocksBeforeNetwork(t *testing.T) {
	t.Parallel()
	inner := &stubDoer{resp: &http.Response{StatusCode: 200, Body: http.NoBody}}
	guard := NewSSRFGuard(&SSRFGuardConfig{
		Resolver: &stubResolver{hosts: map[string][]net.IPAddr{
			"private.example.com": {ip("10.0.0.1")},
		}},
	})
	doer := NewHTTPDoerWithGuard(inner, guard)
	req, _ := http.NewRequest(http.MethodPost, "https://private.example.com/x", nil)
	_, err := doer.Do(req)
	if !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("expected ErrSSRFBlocked, got %v", err)
	}
	if inner.hits != 0 {
		t.Fatalf("inner doer should not be called when SSRF blocks (hits=%d)", inner.hits)
	}
}

func TestHTTPDoerWithGuardForwardsAllowed(t *testing.T) {
	t.Parallel()
	inner := &stubDoer{resp: &http.Response{StatusCode: 204, Body: http.NoBody}}
	guard := NewSSRFGuard(&SSRFGuardConfig{
		Resolver: &stubResolver{hosts: map[string][]net.IPAddr{
			"public.example.com": {ip("8.8.8.8")},
		}},
	})
	doer := NewHTTPDoerWithGuard(inner, guard)
	req, _ := http.NewRequest(http.MethodPost, "https://public.example.com/x", nil)
	resp, err := doer.Do(req)
	if err != nil {
		t.Fatalf("expected forwarded call, got %v", err)
	}
	if resp == nil || resp.StatusCode != 204 {
		t.Fatalf("expected 204 from inner doer, got %#v", resp)
	}
	if inner.hits != 1 {
		t.Fatalf("inner doer hit count = %d, want 1", inner.hits)
	}
}

func TestHTTPDoerWithGuardNilRequest(t *testing.T) {
	t.Parallel()
	doer := NewHTTPDoerWithGuard(nil, nil)
	if _, err := doer.Do(nil); !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("expected nil request blocked, got %v", err)
	}
}

func TestHTTPDoerWithGuardEndToEndAgainstHTTPTestServer(t *testing.T) {
	// httptest.Server binds to 127.0.0.1, which the guard MUST block.
	// This test asserts the wiring is wired correctly: the guard fires
	// against the resolved loopback IP regardless of how harmless the
	// upstream looks.
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	doer := NewHTTPDoerWithGuard(http.DefaultClient, NewSSRFGuard(nil))
	req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
	if _, err := doer.Do(req); !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("expected loopback to be blocked, got %v", err)
	}
}
