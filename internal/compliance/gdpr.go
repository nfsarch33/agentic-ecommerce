// Package compliance implements GDPR/CCPA compliance operations.
// v4.9.0 Story 2: right-to-delete, data export, consent tracking.
//
// Decomposition (HARD GATE: complex_fn=4):
//   - RightToDelete     -> orchestrate (cyclomatic 4)
//   - findAffectedTables-> discover (cyclomatic 2)
//   - anonymizeOrders   -> transform (cyclomatic 2)
//   - deleteDirectPII   -> bulk delete (cyclomatic 3)
//   - logAudit          -> persist (cyclomatic 1)
//   - DataExport        -> orchestrate (cyclomatic 3)
//   - collectFromTables -> gather (cyclomatic 2)
//   - buildBundle       -> assemble (cyclomatic 1)
//   - GrantConsent      -> persist (cyclomatic 2)
//   - RevokeConsent     -> persist (cyclomatic 2)
package compliance

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

var (
	ErrSubjectNotFound = errors.New("compliance: subject not found")
	ErrTenantMismatch  = errors.New("compliance: tenant mismatch")
	ErrAuditLogFailed  = errors.New("compliance: audit log write failed")
	ErrAlreadyDeleted  = errors.New("compliance: subject already deleted")
	ErrConsentNotFound = errors.New("compliance: consent record not found")
)

// AuditAction tracks what compliance operation was performed.
type AuditAction string

const (
	AuditActionDelete AuditAction = "right_to_delete"
	AuditActionExport AuditAction = "data_export"
)

// AuditEntry is one row in compliance_audit_log.
type AuditEntry struct {
	ID        string      `json:"id"`
	TenantID  string      `json:"tenant_id"`
	SubjectID string      `json:"subject_id"`
	Action    AuditAction `json:"action"`
	Details   string      `json:"details"`
	CreatedAt time.Time   `json:"created_at"`
}

// ConsentRecord tracks a data subject's consent grants/revocations.
type ConsentRecord struct {
	SubjectID   string     `json:"subject_id"`
	TenantID    string     `json:"tenant_id"`
	ConsentType string     `json:"consent_type"`
	GrantedAt   time.Time  `json:"granted_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

// ExportBundle is the GDPR Article 15 data export envelope.
type ExportBundle struct {
	SubjectID        string           `json:"subject_id"`
	TenantID         string           `json:"tenant_id"`
	CustomerData     map[string]any   `json:"customer_data,omitempty"`
	Orders           []map[string]any `json:"orders,omitempty"`
	CoachingSessions []map[string]any `json:"coaching_sessions,omitempty"`
	FAQInteractions  []map[string]any `json:"faq_interactions,omitempty"`
	ConsentRecords   []ConsentRecord  `json:"consent_records,omitempty"`
	ExportedAt       time.Time        `json:"exported_at"`
}

// ComplianceRepository is the port for all compliance DB operations.
type ComplianceRepository interface {
	FindSubjectTables(ctx context.Context, tenantID, subjectID string) ([]string, error)
	AnonymizeOrders(ctx context.Context, tenantID, subjectID, placeholder string) (int64, error)
	DeleteCustomerPII(ctx context.Context, tenantID, subjectID string) error
	DeleteCoachingSessions(ctx context.Context, tenantID, subjectID string) (int64, error)
	DeleteFAQInteractions(ctx context.Context, tenantID, subjectID string) (int64, error)
	DeleteOperatorAlerts(ctx context.Context, tenantID, subjectID string) (int64, error)
	InsertAuditEntry(ctx context.Context, entry AuditEntry) error
	GetCustomerData(ctx context.Context, tenantID, subjectID string) (map[string]any, error)
	GetSubjectOrders(ctx context.Context, tenantID, subjectID string) ([]map[string]any, error)
	GetSubjectCoachingSessions(ctx context.Context, tenantID, subjectID string) ([]map[string]any, error)
	GetSubjectFAQInteractions(ctx context.Context, tenantID, subjectID string) ([]map[string]any, error)
	GetConsentRecords(ctx context.Context, tenantID, subjectID string) ([]ConsentRecord, error)
	InsertConsent(ctx context.Context, record ConsentRecord) error
	RevokeConsent(ctx context.Context, tenantID, subjectID, consentType string, revokedAt time.Time) error
}

// ComplianceMetrics is the metrics port.
type ComplianceMetrics interface {
	IncDeletions(tenantID string)
	IncExports(tenantID string)
}

// Service orchestrates GDPR/CCPA operations.
type Service struct {
	repo    ComplianceRepository
	metrics ComplianceMetrics
	now     func() time.Time
}

// NewService constructs the compliance service.
func NewService(repo ComplianceRepository, metrics ComplianceMetrics, now func() time.Time) *Service {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{repo: repo, metrics: metrics, now: now}
}

// RightToDelete removes all PII for a data subject. Orchestrator.
func (s *Service) RightToDelete(ctx context.Context, tenantID, subjectID string) error {
	tables, err := s.findAffectedTables(ctx, tenantID, subjectID)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return fmt.Errorf("%w: %s in tenant %s", ErrSubjectNotFound, subjectID, tenantID)
	}
	placeholder := deletionPlaceholder(subjectID)
	if err := s.anonymizeOrders(ctx, tenantID, subjectID, placeholder); err != nil {
		return err
	}
	if err := s.deleteDirectPII(ctx, tenantID, subjectID); err != nil {
		return err
	}
	if err := s.logAudit(ctx, tenantID, subjectID, AuditActionDelete, fmt.Sprintf("tables=%v", tables)); err != nil {
		return err
	}
	if s.metrics != nil {
		s.metrics.IncDeletions(tenantID)
	}
	return nil
}

func (s *Service) findAffectedTables(ctx context.Context, tenantID, subjectID string) ([]string, error) {
	tables, err := s.repo.FindSubjectTables(ctx, tenantID, subjectID)
	if err != nil {
		return nil, fmt.Errorf("compliance: find tables: %w", err)
	}
	return tables, nil
}

func (s *Service) anonymizeOrders(ctx context.Context, tenantID, subjectID, placeholder string) error {
	_, err := s.repo.AnonymizeOrders(ctx, tenantID, subjectID, placeholder)
	if err != nil {
		return fmt.Errorf("compliance: anonymize orders: %w", err)
	}
	return nil
}

func (s *Service) deleteDirectPII(ctx context.Context, tenantID, subjectID string) error {
	if err := s.repo.DeleteCustomerPII(ctx, tenantID, subjectID); err != nil {
		return fmt.Errorf("compliance: delete customer PII: %w", err)
	}
	if _, err := s.repo.DeleteCoachingSessions(ctx, tenantID, subjectID); err != nil {
		return fmt.Errorf("compliance: delete coaching: %w", err)
	}
	if _, err := s.repo.DeleteFAQInteractions(ctx, tenantID, subjectID); err != nil {
		return fmt.Errorf("compliance: delete FAQ: %w", err)
	}
	if _, err := s.repo.DeleteOperatorAlerts(ctx, tenantID, subjectID); err != nil {
		return fmt.Errorf("compliance: delete alerts: %w", err)
	}
	return nil
}

func (s *Service) logAudit(ctx context.Context, tenantID, subjectID string, action AuditAction, details string) error {
	entry := AuditEntry{
		TenantID:  tenantID,
		SubjectID: subjectID,
		Action:    action,
		Details:   details,
		CreatedAt: s.now(),
	}
	if err := s.repo.InsertAuditEntry(ctx, entry); err != nil {
		return fmt.Errorf("%w: %v", ErrAuditLogFailed, err)
	}
	return nil
}

// DataExport returns a GDPR Article 15 data bundle.
func (s *Service) DataExport(ctx context.Context, tenantID, subjectID string) (ExportBundle, error) {
	data, err := s.collectFromTables(ctx, tenantID, subjectID)
	if err != nil {
		return ExportBundle{}, err
	}
	bundle := s.buildBundle(tenantID, subjectID, data)
	if err := s.logAudit(ctx, tenantID, subjectID, AuditActionExport, "full_export"); err != nil {
		return ExportBundle{}, err
	}
	if s.metrics != nil {
		s.metrics.IncExports(tenantID)
	}
	return bundle, nil
}

type collectedData struct {
	customer         map[string]any
	orders           []map[string]any
	coachingSessions []map[string]any
	faqInteractions  []map[string]any
	consents         []ConsentRecord
}

func (s *Service) collectFromTables(ctx context.Context, tenantID, subjectID string) (collectedData, error) {
	cust, err := s.repo.GetCustomerData(ctx, tenantID, subjectID)
	if err != nil {
		return collectedData{}, fmt.Errorf("compliance: collect customer: %w", err)
	}
	orders, err := s.repo.GetSubjectOrders(ctx, tenantID, subjectID)
	if err != nil {
		return collectedData{}, fmt.Errorf("compliance: collect orders: %w", err)
	}
	sessions, err := s.repo.GetSubjectCoachingSessions(ctx, tenantID, subjectID)
	if err != nil {
		return collectedData{}, fmt.Errorf("compliance: collect coaching: %w", err)
	}
	faq, err := s.repo.GetSubjectFAQInteractions(ctx, tenantID, subjectID)
	if err != nil {
		return collectedData{}, fmt.Errorf("compliance: collect FAQ: %w", err)
	}
	consents, err := s.repo.GetConsentRecords(ctx, tenantID, subjectID)
	if err != nil {
		return collectedData{}, fmt.Errorf("compliance: collect consents: %w", err)
	}
	return collectedData{
		customer: cust, orders: orders,
		coachingSessions: sessions, faqInteractions: faq,
		consents: consents,
	}, nil
}

func (s *Service) buildBundle(tenantID, subjectID string, data collectedData) ExportBundle {
	return ExportBundle{
		SubjectID:        subjectID,
		TenantID:         tenantID,
		CustomerData:     data.customer,
		Orders:           data.orders,
		CoachingSessions: data.coachingSessions,
		FAQInteractions:  data.faqInteractions,
		ConsentRecords:   data.consents,
		ExportedAt:       s.now(),
	}
}

// GrantConsent records a consent grant for a data subject.
func (s *Service) GrantConsent(ctx context.Context, tenantID, subjectID, consentType string) error {
	record := ConsentRecord{
		SubjectID:   subjectID,
		TenantID:    tenantID,
		ConsentType: consentType,
		GrantedAt:   s.now(),
	}
	return s.repo.InsertConsent(ctx, record)
}

// RevokeConsent marks a consent as revoked.
func (s *Service) RevokeConsent(ctx context.Context, tenantID, subjectID, consentType string) error {
	return s.repo.RevokeConsent(ctx, tenantID, subjectID, consentType, s.now())
}

// deletionPlaceholder generates the [DELETED-<hash>] replacement.
func deletionPlaceholder(subjectID string) string {
	h := sha256.Sum256([]byte(subjectID))
	return fmt.Sprintf("[DELETED-%x]", h[:8])
}
