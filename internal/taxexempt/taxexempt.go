// Package taxexempt provides tax exemption certificate validation and
// jurisdiction rule management.
package taxexempt

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Certificate represents a tax exemption certificate issued to a customer.
type Certificate struct {
	ID             string
	CustomerID     string
	IssuedBy       string
	JurisdictionCode string
	ExemptType     string
	ValidFrom      time.Time
	ValidTo        time.Time
	Verified       bool
}

// ---------------------------------------------------------------------------
// CertificateStore
// ---------------------------------------------------------------------------

// ErrCertNotFound is returned when a certificate ID is not present in the store.
var ErrCertNotFound = errors.New("taxexempt: certificate not found")

// CertificateStore is a thread-safe in-memory store for Certificate values.
type CertificateStore struct {
	mu    sync.RWMutex
	certs map[string]Certificate // keyed by ID
}

// NewCertificateStore returns an initialised CertificateStore.
func NewCertificateStore() *CertificateStore {
	return &CertificateStore{certs: make(map[string]Certificate)}
}

// Add inserts or replaces a certificate by ID.
func (s *CertificateStore) Add(cert Certificate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certs[cert.ID] = cert
}

// Get returns a pointer to the stored certificate.  It returns ErrCertNotFound
// when the ID is not present.
func (s *CertificateStore) Get(id string) (*Certificate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.certs[id]
	if !ok {
		return nil, ErrCertNotFound
	}
	cp := c
	return &cp, nil
}

// ByCustomer returns all certificates that belong to the given customerID in
// unspecified order.
func (s *CertificateStore) ByCustomer(customerID string) []Certificate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Certificate
	for _, c := range s.certs {
		if c.CustomerID == customerID {
			cp := c
			out = append(out, cp)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// JurisdictionRule and RuleEngine
// ---------------------------------------------------------------------------

// JurisdictionRule defines the tax configuration for a jurisdiction.
type JurisdictionRule struct {
	Code        string
	Name        string
	TaxRate     float64
	ExemptTypes []string
}

// ErrRuleNotFound is returned when a jurisdiction code has no registered rule.
var ErrRuleNotFound = errors.New("taxexempt: jurisdiction rule not found")

// RuleEngine is a thread-safe registry of JurisdictionRule entries.
type RuleEngine struct {
	mu    sync.RWMutex
	rules map[string]JurisdictionRule
}

// NewRuleEngine returns an initialised RuleEngine.
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{rules: make(map[string]JurisdictionRule)}
}

// AddRule inserts or replaces the rule for the given jurisdiction code.
func (e *RuleEngine) AddRule(rule JurisdictionRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules[rule.Code] = rule
}

// GetRule returns the rule for code.  It returns ErrRuleNotFound when absent.
func (e *RuleEngine) GetRule(code string) (*JurisdictionRule, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r, ok := e.rules[code]
	if !ok {
		return nil, ErrRuleNotFound
	}
	cp := r
	return &cp, nil
}

// IsExempt reports whether exemptType is permitted by the rule for
// jurisdictionCode.  Returns false for unknown jurisdictions or unknown types.
func (e *RuleEngine) IsExempt(jurisdictionCode, exemptType string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r, ok := e.rules[jurisdictionCode]
	if !ok {
		return false
	}
	for _, t := range r.ExemptTypes {
		if t == exemptType {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Validator
// ---------------------------------------------------------------------------

// Validator validates a Certificate against a RuleEngine at a specific moment.
type Validator struct {
	Engine *RuleEngine
}

// Validate returns nil when cert is valid at now, otherwise a descriptive error.
//
// Checks performed:
//  1. cert is not expired (ValidFrom <= now < ValidTo).
//  2. A rule exists for cert.JurisdictionCode.
//  3. cert.ExemptType is permitted by that rule.
func (v *Validator) Validate(cert Certificate, now time.Time) error {
	if now.Before(cert.ValidFrom) || !now.Before(cert.ValidTo) {
		return fmt.Errorf("taxexempt: certificate %q is expired or not yet valid at %v", cert.ID, now)
	}
	rule, err := v.Engine.GetRule(cert.JurisdictionCode)
	if err != nil {
		return fmt.Errorf("taxexempt: no rule for jurisdiction %q: %w", cert.JurisdictionCode, err)
	}
	for _, t := range rule.ExemptTypes {
		if t == cert.ExemptType {
			return nil
		}
	}
	return fmt.Errorf("taxexempt: exempt type %q is not permitted in jurisdiction %q", cert.ExemptType, cert.JurisdictionCode)
}
