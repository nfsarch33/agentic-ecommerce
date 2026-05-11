// File scope: v6.1.0 coverage backfill -- the 5 canonical uiauto-vs-
// Playwright scenarios were declared in scenarios.go but never
// exercised by any unit test. The original v4.14.0 harness only
// reached them via mocked-mode integration tests that ship under
// `internal/uiauto/compare/`. Adding lightweight build-shape tests
// here lifts the package coverage from 0% and pins the contract.
package compare_scenarios

import (
	"strings"
	"testing"
)

const testBaseURL = "https://example.test"

func TestProductListingScenarioShape(t *testing.T) {
	t.Parallel()
	s := ProductListing(testBaseURL)
	if s.Name != "product-listing" {
		t.Fatalf("Name = %q", s.Name)
	}
	if !strings.HasPrefix(s.URL, testBaseURL) {
		t.Fatalf("URL = %q, want prefix %q", s.URL, testBaseURL)
	}
	if len(s.Actions) == 0 {
		t.Fatal("Actions empty")
	}
	if len(s.Assertions) == 0 {
		t.Fatal("Assertions empty")
	}
}

func TestOnboardingWizardScenarioShape(t *testing.T) {
	t.Parallel()
	s := OnboardingWizard(testBaseURL)
	if s.Name != "onboarding-wizard" {
		t.Fatalf("Name = %q", s.Name)
	}
	if len(s.Actions) < 3 {
		t.Fatalf("Actions len = %d, want >=3 (navigate+type+click)", len(s.Actions))
	}
	if len(s.Assertions) < 2 {
		t.Fatalf("Assertions len = %d, want >=2", len(s.Assertions))
	}
}

func TestPaymentDashboardScenarioShape(t *testing.T) {
	t.Parallel()
	s := PaymentDashboard(testBaseURL)
	if s.Name != "payment-dashboard" {
		t.Fatalf("Name = %q", s.Name)
	}
	if !strings.Contains(s.URL, "/admin/payments") {
		t.Fatalf("URL = %q, want admin/payments path", s.URL)
	}
}

func TestAgentActivityFeedScenarioShape(t *testing.T) {
	t.Parallel()
	s := AgentActivityFeed(testBaseURL)
	if s.Name != "agent-activity-feed" {
		t.Fatalf("Name = %q", s.Name)
	}
	if !strings.Contains(s.URL, "/admin/activity") {
		t.Fatalf("URL = %q, want admin/activity path", s.URL)
	}
}

func TestOperatorAlertsScenarioShape(t *testing.T) {
	t.Parallel()
	s := OperatorAlerts(testBaseURL)
	if s.Name != "operator-alerts" {
		t.Fatalf("Name = %q", s.Name)
	}
	if !strings.Contains(s.URL, "/admin/alerts") {
		t.Fatalf("URL = %q, want admin/alerts path", s.URL)
	}
}

func TestAllScenariosReturnsCanonicalFive(t *testing.T) {
	t.Parallel()
	scenarios := AllScenarios(testBaseURL)
	if len(scenarios) != 5 {
		t.Fatalf("len(AllScenarios) = %d, want 5", len(scenarios))
	}
	want := map[string]bool{
		"product-listing":     true,
		"onboarding-wizard":   true,
		"payment-dashboard":   true,
		"agent-activity-feed": true,
		"operator-alerts":     true,
	}
	for _, s := range scenarios {
		if !want[s.Name] {
			t.Errorf("unexpected scenario %q", s.Name)
		}
		delete(want, s.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing scenarios: %v", want)
	}
}
