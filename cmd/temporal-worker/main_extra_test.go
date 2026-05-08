package main

import (
	"testing"
	"time"
)

// File scope: targeted coverage for previously-uncovered branches in
// the temporal-worker env helpers (parseDuration seconds-fallback,
// parsePositiveInt edge cases, parseBool unknown values).

func TestParseDurationAcceptsBareSecondsInteger(t *testing.T) {
	t.Parallel()

	got, err := parseDuration("45", 5*time.Second)
	if err != nil {
		t.Fatalf("parseDuration: %v", err)
	}
	if got != 45*time.Second {
		t.Fatalf("parseDuration = %s, want 45s", got)
	}
}

func TestParseDurationRejectsNonNumericFallback(t *testing.T) {
	t.Parallel()

	if _, err := parseDuration("not-a-number", 5*time.Second); err == nil {
		t.Fatal("parseDuration accepted non-numeric input")
	}
}

func TestParseDurationRejectsZeroParsedDuration(t *testing.T) {
	t.Parallel()

	if _, err := parseDuration("0s", 5*time.Second); err == nil {
		t.Fatal("parseDuration accepted zero duration")
	}
}

func TestParseDurationRejectsNegativeSecondsInteger(t *testing.T) {
	t.Parallel()

	if _, err := parseDuration("-30", 5*time.Second); err == nil {
		t.Fatal("parseDuration accepted negative seconds")
	}
}

func TestParseDurationReturnsFallbackForBlank(t *testing.T) {
	t.Parallel()

	got, err := parseDuration("   ", 7*time.Minute)
	if err != nil {
		t.Fatalf("parseDuration: %v", err)
	}
	if got != 7*time.Minute {
		t.Fatalf("parseDuration = %s, want 7m fallback", got)
	}
}

func TestParseBoolRejectsUnknownLiteral(t *testing.T) {
	t.Parallel()

	if _, err := parseBool("definitely", false); err == nil {
		t.Fatal("parseBool accepted unknown literal")
	}
}

func TestParseBoolUsesFallbackForBlank(t *testing.T) {
	t.Parallel()

	got, err := parseBool("   ", true)
	if err != nil {
		t.Fatalf("parseBool: %v", err)
	}
	if !got {
		t.Fatalf("parseBool = %v, want fallback true", got)
	}
}

func TestParsePositiveIntReturnsFallbackForBlank(t *testing.T) {
	t.Parallel()

	got, err := parsePositiveInt("", 4)
	if err != nil {
		t.Fatalf("parsePositiveInt: %v", err)
	}
	if got != 4 {
		t.Fatalf("parsePositiveInt = %d, want fallback 4", got)
	}
}

func TestParsePositiveIntRejectsNegativeAndNonNumeric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, input string
	}{
		{name: "negative", input: "-1"},
		{name: "zero", input: "0"},
		{name: "non-numeric", input: "foo"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parsePositiveInt(tc.input, 5); err == nil {
				t.Fatalf("parsePositiveInt(%q) accepted invalid", tc.input)
			}
		})
	}
}

func TestNewProductRepositoryFromEnvCleanupIsSafeWhenInMemoryFallback(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_URL", "")

	repo, cleanup, err := newProductRepositoryFromEnv(t.Context(), nil)
	if err != nil || repo == nil {
		t.Fatalf("expected in-memory fallback, got repo=%v err=%v", repo, err)
	}
	cleanup()
	cleanup() // double-call should not panic; in-memory cleanup is a no-op.
}

func TestFirstNonEmptyReturnsLastFallbackWhenAllBlank(t *testing.T) {
	t.Parallel()

	if got := firstNonEmpty("", "  ", "fallback"); got != "fallback" {
		t.Fatalf("firstNonEmpty = %q, want fallback", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Fatalf("firstNonEmpty empty = %q, want empty", got)
	}
}
