// Package grafana contains the v2.7.0 per-tenant Grafana dashboard
// provisioning helper. The helper reads
// monitoring/grafana/dashboards/tenant-template.json, substitutes
// __TENANT_ID__ for each tenant ID, and either writes the result to
// disk or returns the rendered JSON. Callers can pipe the rendered
// JSON into the Grafana provisioning API at deploy time, or persist
// it under monitoring/grafana/dashboards/tenants/<id>.json for the
// file-based provider.
package grafana

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// tenantIDPattern enforces the same shape used elsewhere in the
// stack (marketplace, RLS GUC, secret-store). Permitting only safe
// characters means the substituted JSON cannot accidentally break
// the surrounding string literals.
var tenantIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ErrInvalidTenantID is returned when RenderTenantDashboard is
// called with a tenantID that does not match tenantIDPattern.
var ErrInvalidTenantID = errors.New("grafana: invalid tenant id for dashboard rendering")

// RenderTenantDashboard substitutes __TENANT_ID__ in template with
// tenantID and returns the rendered JSON. Callers are expected to
// pre-load the template body once at boot.
func RenderTenantDashboard(template, tenantID string) (string, error) {
	if tenantID == "" || !tenantIDPattern.MatchString(tenantID) {
		return "", fmt.Errorf("%w: tenantID=%q", ErrInvalidTenantID, tenantID)
	}
	if template == "" {
		return "", errors.New("grafana: template is empty")
	}
	out := strings.ReplaceAll(template, "__TENANT_ID__", tenantID)
	return out, nil
}

// RenderTenantDashboards renders dashboards for every tenant in the
// given list. The returned map is keyed by tenantID. The function
// short-circuits on the first error so partial output stays
// detectable.
func RenderTenantDashboards(template string, tenantIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(tenantIDs))
	for _, id := range tenantIDs {
		rendered, err := RenderTenantDashboard(template, id)
		if err != nil {
			return out, err
		}
		out[id] = rendered
	}
	return out, nil
}
