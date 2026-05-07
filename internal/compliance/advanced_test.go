package compliance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

func TestCustomRuleValidationAndEvaluation(t *testing.T) {
	t.Parallel()

	rule := CustomRule{
		TenantID:    "tenant-a",
		ID:          "no-greenwashing",
		Version:     1,
		Name:        "No greenwashing",
		Description: "Reject unsupported sustainability claims.",
		Severity:    SeverityError,
		Enabled:     true,
		Definition: CustomRuleDefinition{
			Type:       CustomRuleContainsAny,
			Field:      CustomRuleFieldDescription,
			Values:     []string{"carbon neutral", "zero emissions"},
			FailReason: "unsupported sustainability claim",
		},
	}
	if err := rule.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	product := testComplianceProduct(t, catalog.ProductInput{
		SKU:         "GREEN-1",
		Title:       "Water Bottle",
		Description: "Reusable water bottle with carbon neutral manufacturing claim.",
		Images:      []catalog.Image{{URL: "https://cdn.example.com/bottle.jpg", Alt: "Reusable water bottle"}},
	})
	got := NewEngine([]Rule{rule}).Evaluate(context.Background(), ProductContent{Product: product})

	if got.Pass {
		t.Fatal("custom rule passed, want fail")
	}
	if !hasRuleID(got.RuleIDs, "no-greenwashing@v1") {
		t.Fatalf("rule IDs = %#v, want versioned custom rule ID", got.RuleIDs)
	}
	if len(got.Reasons) != 1 || got.Reasons[0] != "unsupported sustainability claim" {
		t.Fatalf("reasons = %#v", got.Reasons)
	}
}

func TestDisabledCustomRuleIsNotEvaluated(t *testing.T) {
	t.Parallel()

	rule := CustomRule{
		TenantID: "tenant-a",
		ID:       "disabled-rule",
		Version:  3,
		Name:     "Disabled",
		Severity: SeverityCritical,
		Enabled:  false,
		Definition: CustomRuleDefinition{
			Type:   CustomRuleContainsAny,
			Field:  CustomRuleFieldTitle,
			Values: []string{"blocked"},
		},
	}
	if got := NewEngine([]Rule{rule}).Evaluate(context.Background(), ProductContent{Product: testComplianceProduct(t, catalog.ProductInput{
		SKU:         "SKU-1",
		Title:       "Blocked product",
		Description: "Useful description long enough for the product validation rules.",
	})}); !got.Pass || len(got.Results) != 0 {
		t.Fatalf("disabled custom rule result = %+v, want pass with no evaluated rule results", got)
	}
}

func TestCustomRuleValidationRejectsInvalidDefinition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rule CustomRule
		want string
	}{
		{
			name: "missing tenant",
			rule: CustomRule{ID: "rule", Version: 1, Name: "Rule", Severity: SeverityError, Definition: CustomRuleDefinition{
				Type: CustomRuleContainsAny, Field: CustomRuleFieldTitle, Values: []string{"x"},
			}},
			want: "tenant",
		},
		{
			name: "missing values",
			rule: CustomRule{TenantID: "tenant-a", ID: "rule", Version: 1, Name: "Rule", Severity: SeverityError, Definition: CustomRuleDefinition{
				Type: CustomRuleContainsAny, Field: CustomRuleFieldDescription,
			}},
			want: "values",
		},
		{
			name: "bad severity",
			rule: CustomRule{TenantID: "tenant-a", ID: "rule", Version: 1, Name: "Rule", Severity: "bad", Definition: CustomRuleDefinition{
				Type: CustomRuleContainsAny, Field: CustomRuleFieldDescription, Values: []string{"x"},
			}},
			want: "severity",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.rule.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestHistoryStoreAggregatesByTenantRuleProductAndTrend(t *testing.T) {
	t.Parallel()

	store := seedHistoryStore(t)
	summary, err := store.Summary(context.Background(), SummaryFilter{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.TenantID != "tenant-a" || summary.TotalChecks != 2 || summary.PassedChecks != 1 || summary.FailedChecks != 1 {
		t.Fatalf("summary totals = %+v", summary)
	}
	if summary.RuleStats["seo"].Passed != 1 || summary.RuleStats["seo"].Failed != 1 {
		t.Fatalf("seo rule stats = %+v", summary.RuleStats["seo"])
	}
	if summary.ProductStats["product-1"].Total != 2 {
		t.Fatalf("product stats = %+v", summary.ProductStats)
	}
	if len(summary.Trends) != 2 {
		t.Fatalf("trends = %+v, want two tenant-a days", summary.Trends)
	}
}

func seedHistoryStore(t *testing.T) *InMemoryHistoryStore {
	t.Helper()
	store := NewInMemoryHistoryStore()
	now := time.Date(2026, 5, 8, 2, 0, 0, 0, time.UTC)
	records := []EvaluationRecord{
		{TenantID: "tenant-a", ProductID: "product-1", CheckedAt: now.Add(-24 * time.Hour), Result: Result{Pass: false, Results: []RuleResult{{ID: "seo", Pass: false}, {ID: "legal", Pass: true}}}},
		{TenantID: "tenant-a", ProductID: "product-1", CheckedAt: now, Result: Result{Pass: true, Results: []RuleResult{{ID: "seo", Pass: true}, {ID: "legal", Pass: true}}}},
		{TenantID: "tenant-b", ProductID: "product-2", CheckedAt: now, Result: Result{Pass: false, Results: []RuleResult{{ID: "seo", Pass: false}}}},
	}
	for _, record := range records {
		if err := store.RecordEvaluation(context.Background(), record); err != nil {
			t.Fatalf("record evaluation: %v", err)
		}
	}
	return store
}

func TestHistoryStoreExportsJSONAndCSV(t *testing.T) {
	t.Parallel()

	store := NewInMemoryHistoryStore()
	record := EvaluationRecord{
		TenantID:  "tenant-a",
		ProductID: "product-1",
		CheckedAt: time.Date(2026, 5, 8, 2, 0, 0, 0, time.UTC),
		Result:    Result{Pass: false, Score: 50, Severity: SeverityError, RuleIDs: []string{"seo"}, Results: []RuleResult{{ID: "seo", Pass: false}}},
	}
	if err := store.RecordEvaluation(context.Background(), record); err != nil {
		t.Fatalf("record evaluation: %v", err)
	}

	jsonPayload, contentType, err := ExportHistory(context.Background(), store, SummaryFilter{TenantID: "tenant-a"}, ExportFormatJSON)
	if err != nil {
		t.Fatalf("export json: %v", err)
	}
	if contentType != "application/json" || !strings.Contains(string(jsonPayload), `"tenant_id":"tenant-a"`) {
		t.Fatalf("json export contentType=%q payload=%s", contentType, jsonPayload)
	}

	csvPayload, contentType, err := ExportHistory(context.Background(), store, SummaryFilter{TenantID: "tenant-a"}, ExportFormatCSV)
	if err != nil {
		t.Fatalf("export csv: %v", err)
	}
	if contentType != "text/csv" || !strings.Contains(string(csvPayload), "tenant_id,product_id,checked_at,pass,score,severity,failed_rule_ids") {
		t.Fatalf("csv export contentType=%q payload=%s", contentType, csvPayload)
	}
}
