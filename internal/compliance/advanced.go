package compliance

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CustomRuleType string

const (
	CustomRuleContainsAny CustomRuleType = "contains_any"
)

const (
	CustomRuleFieldTitle       = "title"
	CustomRuleFieldDescription = "description"
	CustomRuleFieldMeta        = "meta_description"
	CustomRuleFieldSEOTitle    = "seo_title"
)

type CustomRuleDefinition struct {
	Type       CustomRuleType `json:"type"`
	Field      string         `json:"field"`
	Values     []string       `json:"values"`
	FailReason string         `json:"fail_reason,omitempty"`
}

type CustomRule struct {
	TenantID    string               `json:"tenant_id"`
	ID          string               `json:"id"`
	Version     int                  `json:"version"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Severity    Severity             `json:"severity"`
	Enabled     bool                 `json:"enabled"`
	Definition  CustomRuleDefinition `json:"definition"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

func (r CustomRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID:          r.versionedID(),
		Description: firstNonEmpty(r.Description, r.Name),
		Severity:    r.Severity,
	}
}

func (r CustomRule) IsEnabled() bool {
	return r.Enabled
}

func (r CustomRule) Evaluate(_ context.Context, content ProductContent) RuleResult {
	desc := r.Descriptor()
	text := strings.ToLower(r.fieldValue(content))
	for _, value := range r.Definition.Values {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(value))) {
			reason := strings.TrimSpace(r.Definition.FailReason)
			if reason == "" {
				reason = "custom compliance rule failed: " + r.ID
			}
			return fail(desc, reason)
		}
	}
	return pass(desc)
}

func (r CustomRule) Validate() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return errors.New("tenant id required")
	}
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("rule id required")
	}
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("rule name required")
	}
	if r.Version < 0 {
		return errors.New("rule version must be non-negative")
	}
	if err := validateCustomRuleSeverity(r.Severity); err != nil {
		return err
	}
	return r.Definition.Validate()
}

func validateCustomRuleSeverity(severity Severity) error {
	switch severity {
	case SeverityInfo, SeverityWarning, SeverityError, SeverityCritical:
		return nil
	default:
		return fmt.Errorf("invalid severity %q", severity)
	}
}

func (d CustomRuleDefinition) Validate() error {
	if d.Type != CustomRuleContainsAny {
		return fmt.Errorf("unsupported custom rule type %q", d.Type)
	}
	if err := validateCustomRuleField(d.Field); err != nil {
		return err
	}
	if !hasCustomRuleValues(d.Values) {
		return errors.New("custom rule values required")
	}
	return nil
}

func validateCustomRuleField(field string) error {
	switch field {
	case CustomRuleFieldTitle, CustomRuleFieldDescription, CustomRuleFieldMeta, CustomRuleFieldSEOTitle:
		return nil
	default:
		return fmt.Errorf("unsupported custom rule field %q", field)
	}
}

func hasCustomRuleValues(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func (r CustomRule) versionedID() string {
	version := r.Version
	if version <= 0 {
		version = 1
	}
	return fmt.Sprintf("%s@v%d", r.ID, version)
}

func (r CustomRule) fieldValue(content ProductContent) string {
	switch r.Definition.Field {
	case CustomRuleFieldTitle:
		return content.Product.Title()
	case CustomRuleFieldMeta:
		return content.Meta
	case CustomRuleFieldSEOTitle:
		return content.SEOTitle
	default:
		return content.Product.Description()
	}
}

type CustomRuleStore interface {
	ListCustomRules(ctx context.Context, tenantID string) ([]CustomRule, error)
	CreateCustomRule(ctx context.Context, rule CustomRule) (CustomRule, error)
	UpdateCustomRule(ctx context.Context, rule CustomRule) (CustomRule, error)
	DeleteCustomRule(ctx context.Context, tenantID, id string) error
}

func ApplyOverrides(rules []Rule, disabledRuleIDs []string, severityOverride map[string]string) []Rule {
	if len(disabledRuleIDs) == 0 && len(severityOverride) == 0 {
		return append([]Rule(nil), rules...)
	}
	disabled := map[string]struct{}{}
	for _, id := range disabledRuleIDs {
		if id = strings.TrimSpace(id); id != "" {
			disabled[id] = struct{}{}
		}
	}
	out := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		desc := rule.Descriptor()
		if _, ok := disabled[desc.ID]; ok {
			continue
		}
		override, ok := severityOverride[desc.ID]
		if !ok {
			out = append(out, rule)
			continue
		}
		severity := Severity(strings.TrimSpace(override))
		switch severity {
		case SeverityInfo, SeverityWarning, SeverityError, SeverityCritical:
			out = append(out, severityOverrideRule{rule: rule, severity: severity})
		default:
			out = append(out, rule)
		}
	}
	return out
}

type severityOverrideRule struct {
	rule     Rule
	severity Severity
}

func (r severityOverrideRule) Descriptor() RuleDescriptor {
	desc := r.rule.Descriptor()
	desc.Severity = r.severity
	return desc
}

func (r severityOverrideRule) Evaluate(ctx context.Context, content ProductContent) RuleResult {
	result := r.rule.Evaluate(ctx, content)
	result.Severity = r.severity
	return result
}

type InMemoryCustomRuleStore struct {
	mu    sync.RWMutex
	rules map[string]map[string]CustomRule
	now   func() time.Time
}

func NewInMemoryCustomRuleStore() *InMemoryCustomRuleStore {
	return &InMemoryCustomRuleStore{
		rules: map[string]map[string]CustomRule{},
		now:   time.Now,
	}
}

func (s *InMemoryCustomRuleStore) ListCustomRules(_ context.Context, tenantID string) ([]CustomRule, error) {
	tenantID, err := requireTenant(tenantID)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenantRules := s.rules[tenantID]
	out := make([]CustomRule, 0, len(tenantRules))
	for _, rule := range tenantRules {
		out = append(out, cloneCustomRule(rule))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *InMemoryCustomRuleStore) CreateCustomRule(_ context.Context, rule CustomRule) (CustomRule, error) {
	rule.TenantID = strings.TrimSpace(rule.TenantID)
	rule.ID = strings.TrimSpace(rule.ID)
	if rule.Version == 0 {
		rule.Version = 1
	}
	if err := rule.Validate(); err != nil {
		return CustomRule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rules[rule.TenantID] == nil {
		s.rules[rule.TenantID] = map[string]CustomRule{}
	}
	if _, exists := s.rules[rule.TenantID][rule.ID]; exists {
		return CustomRule{}, fmt.Errorf("custom rule %s already exists", rule.ID)
	}
	now := s.now().UTC()
	rule.CreatedAt = now
	rule.UpdatedAt = now
	s.rules[rule.TenantID][rule.ID] = cloneCustomRule(rule)
	return cloneCustomRule(rule), nil
}

func (s *InMemoryCustomRuleStore) UpdateCustomRule(_ context.Context, rule CustomRule) (CustomRule, error) {
	rule.TenantID = strings.TrimSpace(rule.TenantID)
	rule.ID = strings.TrimSpace(rule.ID)
	if err := rule.Validate(); err != nil {
		return CustomRule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.rules[rule.TenantID][rule.ID]
	if !ok {
		return CustomRule{}, ErrCustomRuleNotFound
	}
	rule.Version = existing.Version + 1
	rule.CreatedAt = existing.CreatedAt
	rule.UpdatedAt = s.now().UTC()
	s.rules[rule.TenantID][rule.ID] = cloneCustomRule(rule)
	return cloneCustomRule(rule), nil
}

func (s *InMemoryCustomRuleStore) DeleteCustomRule(_ context.Context, tenantID, id string) error {
	tenantID, err := requireTenant(tenantID)
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("rule id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rules[tenantID][id]; !ok {
		return ErrCustomRuleNotFound
	}
	delete(s.rules[tenantID], id)
	return nil
}

var ErrCustomRuleNotFound = errors.New("custom rule not found")

func cloneCustomRule(rule CustomRule) CustomRule {
	if rule.Definition.Values != nil {
		rule.Definition.Values = append([]string(nil), rule.Definition.Values...)
	}
	return rule
}

type EvaluationRecord struct {
	TenantID  string    `json:"tenant_id"`
	ProductID string    `json:"product_id"`
	CheckedAt time.Time `json:"checked_at"`
	Result    Result    `json:"result"`
}

type SummaryFilter struct {
	TenantID string
	From     time.Time
	To       time.Time
}

type RuleStat struct {
	RuleID string `json:"rule_id"`
	Passed int    `json:"passed"`
	Failed int    `json:"failed"`
	Total  int    `json:"total"`
}

type ProductStat struct {
	ProductID string `json:"product_id"`
	Passed    int    `json:"passed"`
	Failed    int    `json:"failed"`
	Total     int    `json:"total"`
}

type TrendPoint struct {
	Date   string `json:"date"`
	Passed int    `json:"passed"`
	Failed int    `json:"failed"`
	Total  int    `json:"total"`
}

type Summary struct {
	TenantID     string                 `json:"tenant_id"`
	TotalChecks  int                    `json:"total_checks"`
	PassedChecks int                    `json:"passed_checks"`
	FailedChecks int                    `json:"failed_checks"`
	PassRate     float64                `json:"pass_rate"`
	RuleStats    map[string]RuleStat    `json:"rule_stats"`
	ProductStats map[string]ProductStat `json:"product_stats"`
	Trends       []TrendPoint           `json:"trends"`
}

type HistoryStore interface {
	RecordEvaluation(ctx context.Context, record EvaluationRecord) error
	ListEvaluations(ctx context.Context, filter SummaryFilter) ([]EvaluationRecord, error)
	Summary(ctx context.Context, filter SummaryFilter) (Summary, error)
}

type InMemoryHistoryStore struct {
	mu      sync.RWMutex
	records []EvaluationRecord
}

func NewInMemoryHistoryStore() *InMemoryHistoryStore {
	return &InMemoryHistoryStore{}
}

func (s *InMemoryHistoryStore) RecordEvaluation(_ context.Context, record EvaluationRecord) error {
	tenantID, err := requireTenant(record.TenantID)
	if err != nil {
		return err
	}
	record.TenantID = tenantID
	if strings.TrimSpace(record.ProductID) == "" {
		return errors.New("product id required")
	}
	if record.CheckedAt.IsZero() {
		record.CheckedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, cloneEvaluationRecord(record))
	return nil
}

func (s *InMemoryHistoryStore) ListEvaluations(_ context.Context, filter SummaryFilter) ([]EvaluationRecord, error) {
	tenantID, err := requireTenant(filter.TenantID)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EvaluationRecord, 0)
	for _, record := range s.records {
		if record.TenantID != tenantID {
			continue
		}
		if !filter.From.IsZero() && record.CheckedAt.Before(filter.From) {
			continue
		}
		if !filter.To.IsZero() && record.CheckedAt.After(filter.To) {
			continue
		}
		out = append(out, cloneEvaluationRecord(record))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CheckedAt.Before(out[j].CheckedAt)
	})
	return out, nil
}

func (s *InMemoryHistoryStore) Summary(ctx context.Context, filter SummaryFilter) (Summary, error) {
	records, err := s.ListEvaluations(ctx, filter)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{
		TenantID:     strings.TrimSpace(filter.TenantID),
		RuleStats:    map[string]RuleStat{},
		ProductStats: map[string]ProductStat{},
		Trends:       []TrendPoint{},
	}
	trends := map[string]TrendPoint{}
	for _, record := range records {
		summary.TotalChecks++
		productStat := summary.ProductStats[record.ProductID]
		productStat.ProductID = record.ProductID
		productStat.Total++
		day := record.CheckedAt.UTC().Format("2006-01-02")
		trend := trends[day]
		trend.Date = day
		trend.Total++
		if record.Result.Pass {
			summary.PassedChecks++
			productStat.Passed++
			trend.Passed++
		} else {
			summary.FailedChecks++
			productStat.Failed++
			trend.Failed++
		}
		summary.ProductStats[record.ProductID] = productStat

		for _, result := range record.Result.Results {
			stat := summary.RuleStats[result.ID]
			stat.RuleID = result.ID
			stat.Total++
			if result.Pass {
				stat.Passed++
			} else {
				stat.Failed++
			}
			summary.RuleStats[result.ID] = stat
		}
		trends[day] = trend
	}
	if summary.TotalChecks > 0 {
		summary.PassRate = float64(summary.PassedChecks) / float64(summary.TotalChecks)
	}
	days := make([]string, 0, len(trends))
	for day := range trends {
		days = append(days, day)
	}
	sort.Strings(days)
	for _, day := range days {
		summary.Trends = append(summary.Trends, trends[day])
	}
	return summary, nil
}

type ExportFormat string

const (
	ExportFormatJSON ExportFormat = "json"
	ExportFormatCSV  ExportFormat = "csv"
)

func ExportHistory(ctx context.Context, store HistoryStore, filter SummaryFilter, format ExportFormat) ([]byte, string, error) {
	records, err := store.ListEvaluations(ctx, filter)
	if err != nil {
		return nil, "", err
	}
	switch format {
	case "", ExportFormatJSON:
		payload, err := json.Marshal(records)
		if err != nil {
			return nil, "", err
		}
		return payload, "application/json", nil
	case ExportFormatCSV:
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)
		if err := writer.Write([]string{"tenant_id", "product_id", "checked_at", "pass", "score", "severity", "failed_rule_ids"}); err != nil {
			return nil, "", err
		}
		for _, record := range records {
			if err := writer.Write([]string{
				record.TenantID,
				record.ProductID,
				record.CheckedAt.UTC().Format(time.RFC3339),
				strconv.FormatBool(record.Result.Pass),
				strconv.Itoa(record.Result.Score),
				string(record.Result.Severity),
				strings.Join(record.Result.RuleIDs, "|"),
			}); err != nil {
				return nil, "", err
			}
		}
		writer.Flush()
		return buf.Bytes(), "text/csv", writer.Error()
	default:
		return nil, "", fmt.Errorf("unsupported export format %q", format)
	}
}

func cloneEvaluationRecord(record EvaluationRecord) EvaluationRecord {
	record.Result.Reasons = append([]string(nil), record.Result.Reasons...)
	record.Result.RuleIDs = append([]string(nil), record.Result.RuleIDs...)
	if record.Result.Results != nil {
		record.Result.Results = append([]RuleResult(nil), record.Result.Results...)
		for i := range record.Result.Results {
			record.Result.Results[i].Reasons = append([]string(nil), record.Result.Results[i].Reasons...)
		}
	}
	return record
}

func requireTenant(tenantID string) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "", errors.New("tenant id required")
	}
	return tenantID, nil
}
