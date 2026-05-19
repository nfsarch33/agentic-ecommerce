// Package compare_scenarios defines the 5 canonical comparison
// scenarios used by the v4.14.0 uiauto-vs-Playwright harness.
// Each scenario exercises a distinct page and assertion pattern
// from the agentic-ecommerce-web frontend.
package compare_scenarios

import "github.com/nfsarch33/helixon-ec/internal/uiauto/compare"

// ProductListing verifies the product listing page renders
// correctly: navigate → verify product count → verify first title.
func ProductListing(baseURL string) compare.TestScenario {
	return compare.TestScenario{
		Name: "product-listing",
		URL:  baseURL + "/products",
		Actions: []compare.Action{
			{Type: compare.ActionNavigate, URL: baseURL + "/products"},
			{Type: compare.ActionWait, WaitMs: 500},
		},
		Assertions: []compare.Assertion{
			{Type: compare.ActionAssertElement, Selector: "[data-testid='product-card']"},
			{Type: compare.ActionAssertText, Selector: "h1", Expected: "Products"},
			{Type: compare.ActionAssertElement, Selector: "[data-testid='product-card']:first-child .product-title"},
		},
	}
}

// OnboardingWizard verifies the multi-step onboarding wizard:
// navigate → fill step 1 → advance → verify step 2 displayed.
func OnboardingWizard(baseURL string) compare.TestScenario {
	return compare.TestScenario{
		Name: "onboarding-wizard",
		URL:  baseURL + "/onboarding",
		Actions: []compare.Action{
			{Type: compare.ActionNavigate, URL: baseURL + "/onboarding"},
			{Type: compare.ActionType_, Selector: "[data-testid='store-name-input']", Value: "Test Store"},
			{Type: compare.ActionClick, Selector: "[data-testid='next-step-btn']"},
			{Type: compare.ActionWait, WaitMs: 300},
		},
		Assertions: []compare.Assertion{
			{Type: compare.ActionAssertElement, Selector: "[data-testid='step-2-content']"},
			{Type: compare.ActionAssertText, Selector: "[data-testid='step-indicator']", Expected: "Step 2"},
		},
	}
}

// PaymentDashboard verifies the payment provider dashboard:
// navigate → verify table renders → verify provider column.
func PaymentDashboard(baseURL string) compare.TestScenario {
	return compare.TestScenario{
		Name: "payment-dashboard",
		URL:  baseURL + "/admin/payments",
		Actions: []compare.Action{
			{Type: compare.ActionNavigate, URL: baseURL + "/admin/payments"},
			{Type: compare.ActionWait, WaitMs: 500},
		},
		Assertions: []compare.Assertion{
			{Type: compare.ActionAssertElement, Selector: "[data-testid='payments-table']"},
			{Type: compare.ActionAssertElement, Selector: "th[data-testid='provider-column']"},
			{Type: compare.ActionAssertText, Selector: "[data-testid='payments-heading']", Expected: "Payment Transactions"},
		},
	}
}

// AgentActivityFeed verifies the SSE-driven agent activity feed:
// navigate → verify SSE connection → verify event renders.
func AgentActivityFeed(baseURL string) compare.TestScenario {
	return compare.TestScenario{
		Name: "agent-activity-feed",
		URL:  baseURL + "/admin/activity",
		Actions: []compare.Action{
			{Type: compare.ActionNavigate, URL: baseURL + "/admin/activity"},
			{Type: compare.ActionWait, WaitMs: 1000},
		},
		Assertions: []compare.Assertion{
			{Type: compare.ActionAssertElement, Selector: "[data-testid='activity-feed']"},
			{Type: compare.ActionAssertElement, Selector: "[data-testid='sse-connected-indicator']"},
			{Type: compare.ActionAssertElement, Selector: "[data-testid='activity-event']:first-child"},
		},
	}
}

// OperatorAlerts verifies the operator alerts page:
// navigate → verify alert list → verify acknowledge button.
func OperatorAlerts(baseURL string) compare.TestScenario {
	return compare.TestScenario{
		Name: "operator-alerts",
		URL:  baseURL + "/admin/alerts",
		Actions: []compare.Action{
			{Type: compare.ActionNavigate, URL: baseURL + "/admin/alerts"},
			{Type: compare.ActionWait, WaitMs: 500},
		},
		Assertions: []compare.Assertion{
			{Type: compare.ActionAssertElement, Selector: "[data-testid='alerts-list']"},
			{Type: compare.ActionAssertElement, Selector: "[data-testid='alert-item']:first-child"},
			{Type: compare.ActionAssertElement, Selector: "[data-testid='acknowledge-btn']"},
		},
	}
}

// AllScenarios returns the canonical 5 comparison scenarios.
func AllScenarios(baseURL string) []compare.TestScenario {
	return []compare.TestScenario{
		ProductListing(baseURL),
		OnboardingWizard(baseURL),
		PaymentDashboard(baseURL),
		AgentActivityFeed(baseURL),
		OperatorAlerts(baseURL),
	}
}
