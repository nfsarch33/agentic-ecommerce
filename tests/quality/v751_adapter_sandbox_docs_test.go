package quality_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV751AdapterSandboxReadinessDocsCoverMockLiveBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "payment",
			path: "docs/operations/payment-sandbox-readiness.md",
			want: []string{
				"# Payment Sandbox Readiness",
				"v7.5.1 QA refresh",
				"## Mock / Live Boundary Matrix",
				"Stripe",
				"PayPal",
				"Alipay",
				"WeChat",
				"`internal/adapter/payment/v530_table_test.go`",
				"`live_payment_sandbox`",
				"operator-gated",
			},
		},
		{
			name: "carrier",
			path: "docs/operations/carrier-sandbox-readiness.md",
			want: []string{
				"# Carrier Sandbox Readiness",
				"v7.5.1 QA refresh",
				"## Mock / Live Boundary Matrix",
				"EC_AUSPOST_SANDBOX",
				"EC_DHL_SANDBOX",
				"`internal/adapter/carrier/v530_table_test.go`",
				"`docs/operations/v750-adapter-hardening.md`",
				"`live_carrier_sandbox`",
				"operator-gated",
			},
		},
		{
			name: "social",
			path: "docs/operations/social-sandbox-readiness.md",
			want: []string{
				"# Social Sandbox Readiness",
				"v7.5.1 QA refresh",
				"## Mock / Live Boundary Matrix",
				"`internal/adapter/social/adapter_factory_test.go`",
				"httptest.Server",
				"cassette",
				"BaseURL",
				"tiktok",
				"facebook",
				"rednote",
				"woocommerce",
				"instagram",
				"pinterest",
				"operator-gated",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := readRepoTextV751(t, tc.path)
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing %q", tc.path, want)
				}
			}
		})
	}
}

func TestV751InstagramPinterestDocsReflectProductionAdapters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path      string
		want      []string
		mustAvoid []string
	}{
		{
			path: "docs/operations/instagram-shop.md",
			want: []string{
				"production adapter",
				"`internal/adapter/social/instagram_shop.go`",
				"`internal/adapter/social/instagram_shop_test.go`",
				"`httptest.Server`",
				"`DefaultInstagramBaseURL`",
			},
			mustAvoid: []string{
				"MUST NOT issue any HTTP calls",
				"production-ready stub",
			},
		},
		{
			path: "docs/operations/pinterest-shop.md",
			want: []string{
				"production adapter",
				"`internal/adapter/social/pinterest_shop.go`",
				"`internal/adapter/social/pinterest_shop_test.go`",
				"`httptest.Server`",
				"`DefaultPinterestBaseURL`",
			},
			mustAvoid: []string{
				"MUST NOT issue any HTTP calls",
				"production-ready stub",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			body := readRepoTextV751(t, tc.path)
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing %q", tc.path, want)
				}
			}
			for _, avoid := range tc.mustAvoid {
				if strings.Contains(body, avoid) {
					t.Fatalf("%s still contains stale wording %q", tc.path, avoid)
				}
			}
		})
	}
}

func TestV751AdapterSandboxQADocumentsAllFamilies(t *testing.T) {
	t.Parallel()

	body := readRepoTextV751(t, "docs/operations/v751-adapter-sandbox-boundaries-qa.md")
	for _, want := range []string{
		"# v7.5.1 Adapter Sandbox Boundaries QA",
		"`docs/operations/payment-sandbox-readiness.md`",
		"`docs/operations/carrier-sandbox-readiness.md`",
		"`docs/operations/social-sandbox-readiness.md`",
		"payment",
		"carrier",
		"social",
		"no live sandbox calls",
		"operator-gated",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("v751 QA doc missing %q", want)
		}
	}
}

func readRepoTextV751(t *testing.T, relPath string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", relPath))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	return string(body)
}
