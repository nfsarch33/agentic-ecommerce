package grafana_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/monitoring/grafana"
)

func loadTemplate(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, "dashboards", "tenant-template.json"))
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	return string(body)
}

func TestRenderTenantDashboard(t *testing.T) {
	t.Parallel()
	template := loadTemplate(t)
	out, err := grafana.RenderTenantDashboard(template, "acme")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Contains(out, "__TENANT_ID__") {
		t.Fatalf("template still contains __TENANT_ID__ placeholder")
	}
	if !strings.Contains(out, `tenant_id=\"acme\"`) {
		t.Fatalf("expected tenant_id=\\\"acme\\\" in rendered queries")
	}
	if !strings.Contains(out, `"uid": "ec-tenant-acme"`) {
		t.Fatalf("expected uid to include tenant id")
	}
}

func TestRenderTenantDashboard_RejectsInvalid(t *testing.T) {
	t.Parallel()
	template := loadTemplate(t)
	for _, tc := range []string{"", " ", "ACME", "acme!", "acme/something"} {
		if _, err := grafana.RenderTenantDashboard(template, tc); !errors.Is(err, grafana.ErrInvalidTenantID) {
			t.Fatalf("tenant=%q err=%v want ErrInvalidTenantID", tc, err)
		}
	}
}

func TestRenderTenantDashboard_EmptyTemplate(t *testing.T) {
	t.Parallel()
	if _, err := grafana.RenderTenantDashboard("", "acme"); err == nil {
		t.Fatalf("expected error for empty template")
	}
}

func TestRenderTenantDashboards(t *testing.T) {
	t.Parallel()
	template := loadTemplate(t)
	out, err := grafana.RenderTenantDashboards(template, []string{"acme", "umbrella", "tenant-c"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d dashboards, want 3", len(out))
	}
	for id, body := range out {
		if strings.Contains(body, "__TENANT_ID__") {
			t.Fatalf("dashboard for %q still contains placeholder", id)
		}
	}
}

func TestRenderTenantDashboards_StopsOnInvalid(t *testing.T) {
	t.Parallel()
	template := loadTemplate(t)
	out, err := grafana.RenderTenantDashboards(template, []string{"acme", "BAD"})
	if !errors.Is(err, grafana.ErrInvalidTenantID) {
		t.Fatalf("err=%v want ErrInvalidTenantID", err)
	}
	if _, ok := out["acme"]; !ok {
		t.Fatalf("partial output should include valid tenant rendered before error")
	}
}
