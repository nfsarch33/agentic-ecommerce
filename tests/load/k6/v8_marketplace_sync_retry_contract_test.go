package k6_test

import (
	"os"
	"strings"
	"testing"
)

func TestV8MarketplaceSyncRetryMatrixContract(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("v8_marketplace_sync_retry.js")
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	script := string(raw)

	for _, want := range []string{
		"marketplace_sync_retry_matrix",
		"marketplace_sync_failures",
		"marketplace_sync_duration",
		"provider",
		"entity_type",
		"retry_class",
		"EC_K6_MARKETPLACE_SYNC_RATE",
		"EC_K6_SCENARIO_DURATION",
		"/healthz",
		"/metrics",
		"rate<0.02",
		"p(95)<250",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing contract token %q", want)
		}
	}

	for _, forbidden := range []string{
		"K6_DURATION",
		"K6_RATE",
		"Authorization",
		"Bearer ",
		"tenant_id",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("script contains forbidden token %q", forbidden)
		}
	}
}
