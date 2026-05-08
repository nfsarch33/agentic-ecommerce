package main

import (
	"testing"
)

// File scope: cover the `getenv` shim that wraps `os.Getenv` and which
// previously sat at 0%. This is a tiny but mandatory surface because
// every helper that reads env vars in main.go ultimately calls it; we
// want a regression alarm if it stops trimming or honouring fallbacks.

func TestGetenvHonoursFallbackWhenEnvUnset(t *testing.T) {
	t.Setenv("ECOMMERCE_AGENT_WORKER_TEST_VAR_X", "")
	if got := getenv("ECOMMERCE_AGENT_WORKER_TEST_VAR_X", "fallback"); got != "fallback" {
		t.Fatalf("getenv = %q, want fallback", got)
	}
}

func TestGetenvTrimsWhitespaceAndReturnsConfiguredValue(t *testing.T) {
	t.Setenv("ECOMMERCE_AGENT_WORKER_TEST_VAR_Y", "  configured  ")
	if got := getenv("ECOMMERCE_AGENT_WORKER_TEST_VAR_Y", "fallback"); got != "configured" {
		t.Fatalf("getenv = %q, want trimmed configured", got)
	}
}

func TestGetenvDefaultPrefersFallbackForBlankValue(t *testing.T) {
	if got := getenvDefault(func(string) string { return "  \t  " }, "ANY", "fallback"); got != "fallback" {
		t.Fatalf("getenvDefault = %q, want fallback for whitespace value", got)
	}
}

func TestFirstConfiguredFallsThroughToFinalLiteral(t *testing.T) {
	got := firstConfigured(func(string) string { return "" }, "ECOMMERCE_AGENT_WORKER_TEST_VAR_NONE", "literal-fallback")
	if got != "literal-fallback" {
		t.Fatalf("firstConfigured = %q, want literal-fallback", got)
	}
}

func TestFirstConfiguredReturnsFirstNonEmpty(t *testing.T) {
	getter := func(key string) string {
		switch key {
		case "ECOMMERCE_AGENT_WORKER_TEST_VAR_A":
			return ""
		case "ECOMMERCE_AGENT_WORKER_TEST_VAR_B":
			return "found-it"
		default:
			return ""
		}
	}
	got := firstConfigured(getter, "ECOMMERCE_AGENT_WORKER_TEST_VAR_A", "ECOMMERCE_AGENT_WORKER_TEST_VAR_B", "literal-fallback")
	if got != "found-it" {
		t.Fatalf("firstConfigured = %q, want found-it", got)
	}
}
