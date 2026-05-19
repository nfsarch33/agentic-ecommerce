package compliance

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
)

func TestApplyOverridesDisablesRulesAndOverridesSeverity(t *testing.T) {
	t.Parallel()

	rules := ApplyOverrides(DefaultRules(), []string{"description_length"}, map[string]string{
		"required_title": "warning",
	})
	engine := NewEngine(rules)
	product := testComplianceProduct(t, testComplianceProductInput("SKU-OVERRIDE", " ", "short"))

	got := engine.Evaluate(context.Background(), ProductContent{Product: product})

	if hasRuleID(got.RuleIDs, "description_length") {
		t.Fatalf("disabled rule still evaluated: %+v", got.RuleIDs)
	}
	for _, result := range got.Results {
		if result.ID == "required_title" && result.Severity != SeverityWarning {
			t.Fatalf("required_title severity = %q, want warning", result.Severity)
		}
	}
}

func TestInMemoryCustomRuleStoreCreatesAndListsByTenant(t *testing.T) {
	t.Parallel()

	store := NewInMemoryCustomRuleStore()
	rule := validCustomRule()
	created, err := store.CreateCustomRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("create custom rule: %v", err)
	}
	if created.Version != 1 || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("created rule = %+v", created)
	}
	if _, err := store.CreateCustomRule(context.Background(), rule); err == nil {
		t.Fatal("duplicate custom rule was accepted")
	}
	assertCustomRuleList(t, store, "tenant-a", 1)
	assertCustomRuleList(t, store, "tenant-b", 0)
}

func TestInMemoryCustomRuleStoreUpdatesVersion(t *testing.T) {
	t.Parallel()

	store := NewInMemoryCustomRuleStore()
	rule := validCustomRule()
	if _, err := store.CreateCustomRule(context.Background(), rule); err != nil {
		t.Fatalf("create custom rule: %v", err)
	}
	rule.Name = "No claims v2"
	rule.Severity = SeverityWarning
	updated, err := store.UpdateCustomRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("update custom rule: %v", err)
	}
	if updated.Version != 2 || updated.Severity != SeverityWarning || updated.CreatedAt.IsZero() {
		t.Fatalf("updated rule = %+v", updated)
	}
}

func TestInMemoryCustomRuleStoreDeletesAndRejectsMissingTenant(t *testing.T) {
	t.Parallel()

	store := NewInMemoryCustomRuleStore()
	if _, err := store.CreateCustomRule(context.Background(), validCustomRule()); err != nil {
		t.Fatalf("create custom rule: %v", err)
	}
	if err := store.DeleteCustomRule(context.Background(), "tenant-a", "no-claims"); err != nil {
		t.Fatalf("delete custom rule: %v", err)
	}
	if err := store.DeleteCustomRule(context.Background(), "tenant-a", "no-claims"); !errors.Is(err, ErrCustomRuleNotFound) {
		t.Fatalf("delete missing error = %v, want ErrCustomRuleNotFound", err)
	}
	if _, err := store.ListCustomRules(context.Background(), " "); err == nil {
		t.Fatal("ListCustomRules accepted missing tenant")
	}
}

func validCustomRule() CustomRule {
	return CustomRule{
		TenantID: "tenant-a",
		ID:       "no-claims",
		Name:     "No claims",
		Severity: SeverityError,
		Enabled:  true,
		Definition: CustomRuleDefinition{
			Type:   CustomRuleContainsAny,
			Field:  CustomRuleFieldDescription,
			Values: []string{"miracle"},
		},
	}
}

func assertCustomRuleList(t *testing.T, store *InMemoryCustomRuleStore, tenantID string, want int) {
	t.Helper()
	list, err := store.ListCustomRules(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("list %s rules: %v", tenantID, err)
	}
	if len(list) != want {
		t.Fatalf("%s rules = %+v, want %d", tenantID, list, want)
	}
}

func TestCustomRuleFieldsEvaluateExpectedContent(t *testing.T) {
	t.Parallel()

	product := testComplianceProduct(t, testComplianceProductInput(
		"FIELD-1",
		"Limited title claim",
		"Long enough product description for compliance evaluation with ordinary language.",
	))
	for _, tt := range []struct {
		name  string
		field string
		value string
		input ProductContent
	}{
		{name: "title", field: CustomRuleFieldTitle, value: "limited", input: ProductContent{Product: product}},
		{name: "meta", field: CustomRuleFieldMeta, value: "regulated", input: ProductContent{Product: product, Meta: "regulated claim"}},
		{name: "seo title", field: CustomRuleFieldSEOTitle, value: "exclusive", input: ProductContent{Product: product, SEOTitle: "exclusive SEO title"}},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rule := CustomRule{
				TenantID: "tenant-a",
				ID:       "field-" + tt.name,
				Version:  1,
				Name:     "Field rule",
				Severity: SeverityError,
				Enabled:  true,
				Definition: CustomRuleDefinition{
					Type:   CustomRuleContainsAny,
					Field:  tt.field,
					Values: []string{tt.value},
				},
			}
			if got := NewEngine([]Rule{rule}).Evaluate(context.Background(), tt.input); got.Pass {
				t.Fatalf("rule for field %s passed, want failure", tt.field)
			}
		})
	}
}

func testComplianceProductInput(sku, title, description string) catalog.ProductInput {
	return catalog.ProductInput{SKU: sku, Title: title, Description: description}
}
