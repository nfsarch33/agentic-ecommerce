//go:build v451_smoke

package v451_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkPolicy_DefaultDenyExists(t *testing.T) {
	content, err := os.ReadFile("../../../deploy/helm/agentic-ecommerce/templates/networkpolicy.yaml")
	require.NoError(t, err)

	raw := string(content)
	assert.Contains(t, raw, "default-deny", "NetworkPolicy with default-deny must exist")
	assert.Contains(t, raw, "- Ingress")
	assert.Contains(t, raw, "- Egress")
	assert.Contains(t, raw, "podSelector: {}")
}

func TestNetworkPolicy_MCApiIngressPort8080(t *testing.T) {
	content, err := os.ReadFile("../../../deploy/helm/agentic-ecommerce/templates/networkpolicy.yaml")
	require.NoError(t, err)

	assert.Contains(t, string(content), "port: 8080")
	assert.Contains(t, string(content), "mc-api")
}

func TestNetworkPolicy_WorkersAccessTemporal(t *testing.T) {
	content, err := os.ReadFile("../../../deploy/helm/agentic-ecommerce/templates/networkpolicy.yaml")
	require.NoError(t, err)

	assert.Contains(t, string(content), "port: 7233")
	assert.Contains(t, string(content), "temporal")
}

func TestNetworkPolicy_AllPodsAccessOTelCollector(t *testing.T) {
	content, err := os.ReadFile("../../../deploy/helm/agentic-ecommerce/templates/networkpolicy.yaml")
	require.NoError(t, err)

	assert.Contains(t, string(content), "port: 4317")
	assert.Contains(t, string(content), "otel-collector")
}

func TestPodSecurity_RestrictedProfile(t *testing.T) {
	content, err := os.ReadFile("../../../deploy/helm/agentic-ecommerce/templates/podsecurity.yaml")
	require.NoError(t, err)

	assert.Contains(t, string(content), "pod-security.kubernetes.io/enforce: restricted")
	assert.Contains(t, string(content), "pod-security.kubernetes.io/audit: restricted")
}
