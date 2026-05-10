//go:build v491_smoke

package v491

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/compliance"
)

// inMemRepo is a full in-memory ComplianceRepository for E2E testing.
type inMemRepo struct {
	customers        map[string]map[string]any
	orders           map[string][]map[string]any
	coachingSessions map[string][]map[string]any
	faqInteractions  map[string][]map[string]any
	alerts           map[string]int64
	auditEntries     []compliance.AuditEntry
	consents         []compliance.ConsentRecord
}

func newInMemRepo() *inMemRepo {
	return &inMemRepo{
		customers:        make(map[string]map[string]any),
		orders:           make(map[string][]map[string]any),
		coachingSessions: make(map[string][]map[string]any),
		faqInteractions:  make(map[string][]map[string]any),
		alerts:           make(map[string]int64),
	}
}

func repoKey(tenantID, subjectID string) string {
	return tenantID + "/" + subjectID
}

func (r *inMemRepo) FindSubjectTables(_ context.Context, tenantID, subjectID string) ([]string, error) {
	key := repoKey(tenantID, subjectID)
	var tables []string
	if _, ok := r.customers[key]; ok {
		tables = append(tables, "customers")
	}
	if _, ok := r.orders[key]; ok {
		tables = append(tables, "orders")
	}
	if _, ok := r.coachingSessions[key]; ok {
		tables = append(tables, "coaching_sessions")
	}
	if _, ok := r.faqInteractions[key]; ok {
		tables = append(tables, "faq_interactions")
	}
	return tables, nil
}

func (r *inMemRepo) AnonymizeOrders(_ context.Context, tenantID, subjectID, placeholder string) (int64, error) {
	key := repoKey(tenantID, subjectID)
	orders := r.orders[key]
	for i := range orders {
		orders[i]["customer_name"] = placeholder
		orders[i]["customer_email"] = placeholder
	}
	return int64(len(orders)), nil
}

func (r *inMemRepo) DeleteCustomerPII(_ context.Context, tenantID, subjectID string) error {
	delete(r.customers, repoKey(tenantID, subjectID))
	return nil
}

func (r *inMemRepo) DeleteCoachingSessions(_ context.Context, tenantID, subjectID string) (int64, error) {
	key := repoKey(tenantID, subjectID)
	n := int64(len(r.coachingSessions[key]))
	delete(r.coachingSessions, key)
	return n, nil
}

func (r *inMemRepo) DeleteFAQInteractions(_ context.Context, tenantID, subjectID string) (int64, error) {
	key := repoKey(tenantID, subjectID)
	n := int64(len(r.faqInteractions[key]))
	delete(r.faqInteractions, key)
	return n, nil
}

func (r *inMemRepo) DeleteOperatorAlerts(_ context.Context, tenantID, subjectID string) (int64, error) {
	key := repoKey(tenantID, subjectID)
	n := r.alerts[key]
	delete(r.alerts, key)
	return n, nil
}

func (r *inMemRepo) InsertAuditEntry(_ context.Context, entry compliance.AuditEntry) error {
	r.auditEntries = append(r.auditEntries, entry)
	return nil
}

func (r *inMemRepo) GetCustomerData(_ context.Context, tenantID, subjectID string) (map[string]any, error) {
	return r.customers[repoKey(tenantID, subjectID)], nil
}

func (r *inMemRepo) GetSubjectOrders(_ context.Context, tenantID, subjectID string) ([]map[string]any, error) {
	return r.orders[repoKey(tenantID, subjectID)], nil
}

func (r *inMemRepo) GetSubjectCoachingSessions(_ context.Context, tenantID, subjectID string) ([]map[string]any, error) {
	return r.coachingSessions[repoKey(tenantID, subjectID)], nil
}

func (r *inMemRepo) GetSubjectFAQInteractions(_ context.Context, tenantID, subjectID string) ([]map[string]any, error) {
	return r.faqInteractions[repoKey(tenantID, subjectID)], nil
}

func (r *inMemRepo) GetConsentRecords(_ context.Context, _, _ string) ([]compliance.ConsentRecord, error) {
	return r.consents, nil
}

func (r *inMemRepo) InsertConsent(_ context.Context, record compliance.ConsentRecord) error {
	r.consents = append(r.consents, record)
	return nil
}

func (r *inMemRepo) RevokeConsent(_ context.Context, _, _, _ string, revokedAt time.Time) error {
	for i := range r.consents {
		r.consents[i].RevokedAt = &revokedAt
	}
	return nil
}

// seedTestData populates an in-memory repo with a complete data subject.
func seedTestData(repo *inMemRepo, tenantID, subjectID string) {
	key := repoKey(tenantID, subjectID)
	repo.customers[key] = map[string]any{
		"name": "Jane Doe", "email": "jane@example.com",
		"phone": "+61400000000",
	}
	repo.orders[key] = []map[string]any{
		{"order_id": "o1", "customer_name": "Jane Doe", "customer_email": "jane@example.com", "total_cents": 5000},
		{"order_id": "o2", "customer_name": "Jane Doe", "customer_email": "jane@example.com", "total_cents": 12000},
	}
	repo.coachingSessions[key] = []map[string]any{
		{"session_id": "cs1", "topic": "upselling"},
	}
	repo.faqInteractions[key] = []map[string]any{
		{"faq_id": "f1", "question": "Where is my order?"},
	}
	repo.alerts[key] = 2
}

var fixedNow = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

// E2E Scenario: full lifecycle -- create → delete → verify → audit.
func TestCompliance_FullDeleteLifecycle(t *testing.T) {
	t.Parallel()
	repo := newInMemRepo()
	seedTestData(repo, "t1", "subj-1")
	svc := compliance.NewService(repo, nil, func() time.Time { return fixedNow })

	if err := svc.RightToDelete(context.Background(), "t1", "subj-1"); err != nil {
		t.Fatalf("RightToDelete: %v", err)
	}

	key := repoKey("t1", "subj-1")

	if _, ok := repo.customers[key]; ok {
		t.Fatal("customer PII should be deleted")
	}

	if _, ok := repo.coachingSessions[key]; ok {
		t.Fatal("coaching sessions should be deleted")
	}

	if _, ok := repo.faqInteractions[key]; ok {
		t.Fatal("FAQ interactions should be deleted")
	}

	orders := repo.orders[key]
	for _, o := range orders {
		name, _ := o["customer_name"].(string)
		if name == "Jane Doe" {
			t.Fatal("order customer_name should be anonymized")
		}
		if _, ok := o["total_cents"]; !ok {
			t.Fatal("order total_cents (non-PII) should be preserved")
		}
	}

	if len(repo.auditEntries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(repo.auditEntries))
	}
	if repo.auditEntries[0].Action != "right_to_delete" {
		t.Fatalf("audit action = %s, want right_to_delete", repo.auditEntries[0].Action)
	}
}

// E2E Scenario: export contains all subject data.
func TestCompliance_FullExportLifecycle(t *testing.T) {
	t.Parallel()
	repo := newInMemRepo()
	seedTestData(repo, "t1", "subj-1")
	svc := compliance.NewService(repo, nil, func() time.Time { return fixedNow })

	bundle, err := svc.DataExport(context.Background(), "t1", "subj-1")
	if err != nil {
		t.Fatalf("DataExport: %v", err)
	}
	if bundle.CustomerData["name"] != "Jane Doe" {
		t.Fatal("export missing customer name")
	}
	if len(bundle.Orders) != 2 {
		t.Fatalf("export orders = %d, want 2", len(bundle.Orders))
	}
	if len(bundle.CoachingSessions) != 1 {
		t.Fatal("export missing coaching sessions")
	}
	if len(bundle.FAQInteractions) != 1 {
		t.Fatal("export missing FAQ interactions")
	}
	if len(repo.auditEntries) != 1 || repo.auditEntries[0].Action != "data_export" {
		t.Fatal("export audit log not persisted")
	}
}

// E2E Scenario: consent grant → verify → revoke → verify.
func TestCompliance_ConsentLifecycle(t *testing.T) {
	t.Parallel()
	repo := newInMemRepo()
	svc := compliance.NewService(repo, nil, func() time.Time { return fixedNow })

	if err := svc.GrantConsent(context.Background(), "t1", "subj-1", "marketing"); err != nil {
		t.Fatalf("GrantConsent: %v", err)
	}
	if len(repo.consents) != 1 {
		t.Fatal("consent not recorded")
	}
	if repo.consents[0].RevokedAt != nil {
		t.Fatal("new consent should not be revoked")
	}

	if err := svc.RevokeConsent(context.Background(), "t1", "subj-1", "marketing"); err != nil {
		t.Fatalf("RevokeConsent: %v", err)
	}
	if repo.consents[0].RevokedAt == nil {
		t.Fatal("consent should be revoked")
	}
}

// E2E Scenario: tenant isolation — delete in t1 doesn't affect t2.
func TestCompliance_TenantIsolation(t *testing.T) {
	t.Parallel()
	repo := newInMemRepo()
	seedTestData(repo, "t1", "subj-1")
	seedTestData(repo, "t2", "subj-1")
	svc := compliance.NewService(repo, nil, func() time.Time { return fixedNow })

	if err := svc.RightToDelete(context.Background(), "t1", "subj-1"); err != nil {
		t.Fatalf("RightToDelete t1: %v", err)
	}

	if _, ok := repo.customers[repoKey("t2", "subj-1")]; !ok {
		t.Fatal("t2 customer data should be unaffected")
	}
}

// E2E Scenario: delete non-existent subject returns error.
func TestCompliance_DeleteNonExistent(t *testing.T) {
	t.Parallel()
	repo := newInMemRepo()
	svc := compliance.NewService(repo, nil, func() time.Time { return fixedNow })

	err := svc.RightToDelete(context.Background(), "t1", "ghost")
	if !errors.Is(err, compliance.ErrSubjectNotFound) {
		t.Fatalf("expected ErrSubjectNotFound, got: %v", err)
	}
}
