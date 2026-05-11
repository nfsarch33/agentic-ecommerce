package k6_test

import (
	"os"
	"strings"
	"testing"
)

func TestV490ComprehensiveUsesMountedLoadMatrixRoutes(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("v490_comprehensive.js")
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	script := string(raw)

	for _, stale := range []string{
		"/api/v1/payments/charge",
		"/api/v1/payments/webhook",
		"/api/v1/admin/mobile",
		"/api/v1/coaching/tip",
		"/api/v1/marketplace/commissions/report",
		"/api/v1/analytics/gmv/daily",
	} {
		if strings.Contains(script, stale) {
			t.Fatalf("script still contains stale route %q", stale)
		}
	}

	for _, mounted := range []string{
		"/api/v1/payments?",
		"/api/v1/webhooks",
		"/api/v1/admin/summary",
		"/api/v1/admin/orders",
		"/api/v1/marketplace/plugins",
		"/api/v1/tenants/${TENANT_ID}/dashboard",
		"/api/v1/analytics/gmv?",
		"EC_K6_SCENARIO_DURATION",
		"Default rates target 100 HTTP requests/s",
	} {
		if !strings.Contains(script, mounted) {
			t.Fatalf("script missing mounted route or config token %q", mounted)
		}
	}

	for _, rate := range []string{
		"rate: scaledRate(10)",
		"rate: scaledRate(15)",
		"rate: scaledRate(5)",
		"rate: scaledRate(20)",
	} {
		if !strings.Contains(script, rate) {
			t.Fatalf("script missing default 100-RPS matrix rate token %q", rate)
		}
	}
}
