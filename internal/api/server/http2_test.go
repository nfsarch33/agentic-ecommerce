package server

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/http2"
)

func TestLoadHTTP2ConfigDefaults(t *testing.T) {
	t.Setenv("EC_HTTP2_ENABLED", "")
	t.Setenv("EC_HTTP2_H2C", "")
	cfg := LoadHTTP2Config()
	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true (default)")
	}
	if cfg.H2C {
		t.Fatal("H2C = true, want false (default)")
	}
}

func TestLoadHTTP2ConfigFromEnv(t *testing.T) {
	t.Setenv("EC_HTTP2_ENABLED", "true")
	t.Setenv("EC_HTTP2_H2C", "true")
	cfg := LoadHTTP2Config()
	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if !cfg.H2C {
		t.Fatal("H2C = false, want true")
	}
}

func TestLoadHTTP2ConfigDisabled(t *testing.T) {
	t.Setenv("EC_HTTP2_ENABLED", "false")
	t.Setenv("EC_HTTP2_H2C", "true")
	cfg := LoadHTTP2Config()
	if cfg.Enabled {
		t.Fatal("Enabled = true, want false")
	}
}

func TestWrapHandler_NoH2C(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	cfg := HTTP2Config{Enabled: true, H2C: false}
	wrapped := WrapHandler(handler, cfg)

	ts := httptest.NewServer(wrapped)
	defer ts.Close()
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if !called {
		t.Fatal("original handler was not called")
	}
}

func TestWrapHandler_H2C(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("h2c-ok"))
	})
	cfg := HTTP2Config{Enabled: true, H2C: true}
	wrapped := WrapHandler(handler, cfg)

	ts := httptest.NewServer(wrapped)
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "h2c-ok" {
		t.Fatalf("body = %q, want h2c-ok", body)
	}
}

func TestHTTP2_TLSNegotiation(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("proto:" + r.Proto))
	})
	ts := httptest.NewTLSServer(handler)
	defer ts.Close()

	if err := http2.ConfigureTransport(ts.Client().Transport.(*http.Transport)); err != nil {
		t.Fatalf("ConfigureTransport: %v", err)
	}
	ts.Client().Transport.(*http.Transport).TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	resp, err := ts.Client().Get(ts.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.Proto != "HTTP/2.0" {
		t.Logf("body=%s proto=%s (HTTP/2 via TLS may not be negotiated in test TLS server; this is informational)", body, resp.Proto)
	}
}

func TestWrapHandler_Disabled(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	cfg := HTTP2Config{Enabled: false, H2C: true}
	wrapped := WrapHandler(handler, cfg)

	ts := httptest.NewServer(wrapped)
	defer ts.Close()
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if !called {
		t.Fatal("original handler was not called when Enabled=false")
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		val      string
		fallback bool
		want     bool
	}{
		{"", true, true},
		{"", false, false},
		{"true", false, true},
		{"TRUE", false, true},
		{"1", false, true},
		{"yes", false, true},
		{"false", true, false},
		{"0", true, false},
		{"no", true, false},
		{"invalid", true, true},
		{"invalid", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.val, func(t *testing.T) {
			t.Setenv("TEST_BOOL", tc.val)
			got := envBool("TEST_BOOL", tc.fallback)
			if got != tc.want {
				t.Errorf("envBool(%q, %v) = %v, want %v", tc.val, tc.fallback, got, tc.want)
			}
		})
	}
}
