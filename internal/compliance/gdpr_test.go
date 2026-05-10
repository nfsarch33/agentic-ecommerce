package compliance

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubRepo struct {
	tables           []string
	anonymizedCount  int64
	deletedCustomer  bool
	deletedCoaching  int64
	deletedFAQ       int64
	deletedAlerts    int64
	auditEntries     []AuditEntry
	customerData     map[string]any
	orders           []map[string]any
	coachingSessions []map[string]any
	faqInteractions  []map[string]any
	consents         []ConsentRecord
}

func (r *stubRepo) FindSubjectTables(_ context.Context, _, _ string) ([]string, error) {
	return r.tables, nil
}
func (r *stubRepo) AnonymizeOrders(_ context.Context, _, _, _ string) (int64, error) {
	return r.anonymizedCount, nil
}
func (r *stubRepo) DeleteCustomerPII(_ context.Context, _, _ string) error {
	r.deletedCustomer = true
	return nil
}
func (r *stubRepo) DeleteCoachingSessions(_ context.Context, _, _ string) (int64, error) {
	r.deletedCoaching++
	return r.deletedCoaching, nil
}
func (r *stubRepo) DeleteFAQInteractions(_ context.Context, _, _ string) (int64, error) {
	r.deletedFAQ++
	return r.deletedFAQ, nil
}
func (r *stubRepo) DeleteOperatorAlerts(_ context.Context, _, _ string) (int64, error) {
	r.deletedAlerts++
	return r.deletedAlerts, nil
}
func (r *stubRepo) InsertAuditEntry(_ context.Context, entry AuditEntry) error {
	r.auditEntries = append(r.auditEntries, entry)
	return nil
}
func (r *stubRepo) GetCustomerData(_ context.Context, _, _ string) (map[string]any, error) {
	return r.customerData, nil
}
func (r *stubRepo) GetSubjectOrders(_ context.Context, _, _ string) ([]map[string]any, error) {
	return r.orders, nil
}
func (r *stubRepo) GetSubjectCoachingSessions(_ context.Context, _, _ string) ([]map[string]any, error) {
	return r.coachingSessions, nil
}
func (r *stubRepo) GetSubjectFAQInteractions(_ context.Context, _, _ string) ([]map[string]any, error) {
	return r.faqInteractions, nil
}
func (r *stubRepo) GetConsentRecords(_ context.Context, _, _ string) ([]ConsentRecord, error) {
	return r.consents, nil
}
func (r *stubRepo) InsertConsent(_ context.Context, record ConsentRecord) error {
	r.consents = append(r.consents, record)
	return nil
}
func (r *stubRepo) RevokeConsent(_ context.Context, _, _, _ string, revokedAt time.Time) error {
	for i := range r.consents {
		r.consents[i].RevokedAt = &revokedAt
	}
	return nil
}

type stubMetrics struct {
	deletions int
	exports   int
}

func (m *stubMetrics) IncDeletions(_ string) { m.deletions++ }
func (m *stubMetrics) IncExports(_ string)   { m.exports++ }

var fixedNow = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

func newTestService(repo *stubRepo) *Service {
	return NewService(repo, &stubMetrics{}, func() time.Time { return fixedNow })
}

// RED Scenario 1: right-to-delete removes all PII.
func TestRightToDelete_RemovesAllPII(t *testing.T) {
	t.Parallel()
	repo := &stubRepo{
		tables:          []string{"customers", "orders", "coaching_sessions"},
		anonymizedCount: 3,
	}
	svc := newTestService(repo)
	err := svc.RightToDelete(context.Background(), "t1", "subj-1")
	if err != nil {
		t.Fatalf("RightToDelete: %v", err)
	}
	if !repo.deletedCustomer {
		t.Fatal("customer PII not deleted")
	}
	if repo.deletedCoaching != 1 {
		t.Fatal("coaching sessions not deleted")
	}
	if repo.deletedFAQ != 1 {
		t.Fatal("FAQ interactions not deleted")
	}
}

// RED Scenario 2: export contains all subject data.
func TestDataExport_ContainsAllData(t *testing.T) {
	t.Parallel()
	repo := &stubRepo{
		customerData:     map[string]any{"name": "Jane", "email": "j@x.com"},
		orders:           []map[string]any{{"order_id": "o1", "total_cents": 5000}},
		coachingSessions: []map[string]any{{"session_id": "s1"}},
		faqInteractions:  []map[string]any{{"faq_id": "f1"}},
		consents:         []ConsentRecord{{SubjectID: "subj-1", ConsentType: "marketing"}},
	}
	svc := newTestService(repo)
	bundle, err := svc.DataExport(context.Background(), "t1", "subj-1")
	if err != nil {
		t.Fatalf("DataExport: %v", err)
	}
	if bundle.CustomerData["name"] != "Jane" {
		t.Fatal("customer name missing from export")
	}
	if len(bundle.Orders) != 1 {
		t.Fatal("orders missing from export")
	}
	if len(bundle.CoachingSessions) != 1 {
		t.Fatal("coaching sessions missing from export")
	}
	if len(bundle.FAQInteractions) != 1 {
		t.Fatal("FAQ interactions missing from export")
	}
	if len(bundle.ConsentRecords) != 1 {
		t.Fatal("consent records missing from export")
	}
}

// RED Scenario 3: consent record tracks grant/revoke.
func TestConsent_GrantAndRevoke(t *testing.T) {
	t.Parallel()
	repo := &stubRepo{}
	svc := newTestService(repo)
	err := svc.GrantConsent(context.Background(), "t1", "subj-1", "marketing")
	if err != nil {
		t.Fatalf("GrantConsent: %v", err)
	}
	if len(repo.consents) != 1 {
		t.Fatalf("expected 1 consent, got %d", len(repo.consents))
	}
	if repo.consents[0].RevokedAt != nil {
		t.Fatal("new consent should not be revoked")
	}
	err = svc.RevokeConsent(context.Background(), "t1", "subj-1", "marketing")
	if err != nil {
		t.Fatalf("RevokeConsent: %v", err)
	}
	if repo.consents[0].RevokedAt == nil {
		t.Fatal("consent should be revoked after RevokeConsent")
	}
}

// RED Scenario 4: audit log persisted for delete.
func TestRightToDelete_AuditLogPersisted(t *testing.T) {
	t.Parallel()
	repo := &stubRepo{tables: []string{"customers"}}
	svc := newTestService(repo)
	_ = svc.RightToDelete(context.Background(), "t1", "subj-1")
	if len(repo.auditEntries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(repo.auditEntries))
	}
	if repo.auditEntries[0].Action != AuditActionDelete {
		t.Fatalf("audit action = %s, want right_to_delete", repo.auditEntries[0].Action)
	}
}

// RED Scenario 5: tenant isolation — subject not found in other tenant.
func TestRightToDelete_SubjectNotFound(t *testing.T) {
	t.Parallel()
	repo := &stubRepo{tables: nil}
	svc := newTestService(repo)
	err := svc.RightToDelete(context.Background(), "t2", "subj-unknown")
	if !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("expected ErrSubjectNotFound, got: %v", err)
	}
}

// RED Scenario 6: anonymization preserves analytics (orders have placeholder, not deleted).
func TestAnonymization_PreservesAnalytics(t *testing.T) {
	t.Parallel()
	placeholder := deletionPlaceholder("subj-1")
	if placeholder == "" {
		t.Fatal("placeholder should not be empty")
	}
	if placeholder == "subj-1" {
		t.Fatal("placeholder should differ from original subject ID")
	}
	if len(placeholder) < 20 {
		t.Fatalf("placeholder too short: %s", placeholder)
	}
}

func TestDataExport_AuditLogPersisted(t *testing.T) {
	t.Parallel()
	repo := &stubRepo{customerData: map[string]any{"name": "Test"}}
	svc := newTestService(repo)
	_, err := svc.DataExport(context.Background(), "t1", "subj-1")
	if err != nil {
		t.Fatalf("DataExport: %v", err)
	}
	if len(repo.auditEntries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(repo.auditEntries))
	}
	if repo.auditEntries[0].Action != AuditActionExport {
		t.Fatalf("audit action = %s, want data_export", repo.auditEntries[0].Action)
	}
}
