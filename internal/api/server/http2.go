// Package server provides HTTP/2 configuration for mc-api.
//
// Go's net/http automatically supports HTTP/2 when TLS is enabled
// (e.g. behind a load balancer that terminates TLS). For local
// development without TLS certificates, h2c (HTTP/2 Cleartext)
// enables HTTP/2 multiplexing over plain TCP.
//
// Production deployment note: when mc-api runs behind a TLS-
// terminating load balancer (AWS ALB, GCP HTTPS LB, nginx), HTTP/2
// is negotiated automatically via ALPN. The h2c path is only needed
// for local dev and integration tests that want HTTP/2 without
// certificates.
//
// Config env vars:
//   - EC_HTTP2_ENABLED (bool, default true)
//   - EC_HTTP2_H2C (bool, default false; set true for local dev)
package server

import (
	"net/http"
	"os"
	"strings"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// HTTP2Config holds HTTP/2 server configuration.
type HTTP2Config struct {
	Enabled bool
	H2C     bool
}

// LoadHTTP2Config reads HTTP/2 settings from environment variables.
func LoadHTTP2Config() HTTP2Config {
	return HTTP2Config{
		Enabled: envBool("EC_HTTP2_ENABLED", true),
		H2C:     envBool("EC_HTTP2_H2C", false),
	}
}

// WrapHandler returns a handler that supports HTTP/2 cleartext (h2c)
// when cfg.H2C is true. When cfg.Enabled is false or cfg.H2C is
// false, the original handler is returned unchanged (HTTP/2 over TLS
// is automatic in Go's net/http).
func WrapHandler(handler http.Handler, cfg HTTP2Config) http.Handler {
	if !cfg.Enabled || !cfg.H2C {
		return handler
	}
	h2s := &http2.Server{}
	return h2c.NewHandler(handler, h2s)
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return fallback
}
