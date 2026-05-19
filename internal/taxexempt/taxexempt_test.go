package taxexempt_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/taxexempt"
)

// ---------------------------------------------------------------------------
// CertificateStore tests
// ---------------------------------------------------------------------------

func TestCertStoreAddAndGet(t *testing.T) {
	t.Parallel()
	store := taxexempt.NewCertificateStore()

	cert := taxexempt.Certificate{
		ID:               "CERT-1",
		CustomerID:       "CUST-A",
		IssuedBy:         "ATO",
		JurisdictionCode: "AU-NSW",
		ExemptType:       "government",
		ValidFrom:        time.Now().Add(-24 * time.Hour),
		ValidTo:          time.Now().Add(365 * 24 * time.Hour),
		Verified:         true,
	}
	store.Add(cert)

	got, err := store.Get("CERT-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CustomerID != "CUST-A" {
		t.Fatalf("want CustomerID CUST-A, got %q", got.CustomerID)
	}
}

func TestCertStoreGetNotFound(t *testing.T) {
	t.Parallel()
	store := taxexempt.NewCertificateStore()
	_, err := store.Get("GHOST")
	if err == nil {
		t.Fatal("want error for missing cert, got nil")
	}
}

func TestCertStoreByCustomer(t *testing.T) {
	t.Parallel()
	store := taxexempt.NewCertificateStore()

	for i, id := range []string{"C1", "C2", "C3"} {
		c := taxexempt.Certificate{
			ID:         id,
			CustomerID: "CUST-B",
			ValidFrom:  time.Now().Add(-time.Hour),
			ValidTo:    time.Now().Add(time.Hour),
		}
		_ = i
		store.Add(c)
	}
	// Different customer
	store.Add(taxexempt.Certificate{ID: "OTHER", CustomerID: "CUST-X"})

	certs := store.ByCustomer("CUST-B")
	if len(certs) != 3 {
		t.Fatalf("want 3 certs for CUST-B, got %d", len(certs))
	}
}

// ---------------------------------------------------------------------------
// RuleEngine tests
// ---------------------------------------------------------------------------

func TestRuleEngineIsExempt(t *testing.T) {
	t.Parallel()
	engine := taxexempt.NewRuleEngine()
	engine.AddRule(taxexempt.JurisdictionRule{
		Code:        "AU-NSW",
		Name:        "New South Wales",
		TaxRate:     0.10,
		ExemptTypes: []string{"government", "charity"},
	})

	if !engine.IsExempt("AU-NSW", "government") {
		t.Error("expected government to be exempt in AU-NSW")
	}
	if !engine.IsExempt("AU-NSW", "charity") {
		t.Error("expected charity to be exempt in AU-NSW")
	}
	if engine.IsExempt("AU-NSW", "retail") {
		t.Error("expected retail NOT to be exempt in AU-NSW")
	}
	if engine.IsExempt("UNKNOWN", "government") {
		t.Error("expected false for unknown jurisdiction")
	}
}

func TestRuleEngineGetRule(t *testing.T) {
	t.Parallel()
	engine := taxexempt.NewRuleEngine()
	engine.AddRule(taxexempt.JurisdictionRule{Code: "US-CA", Name: "California", TaxRate: 0.0725})

	rule, err := engine.GetRule("US-CA")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rule.TaxRate != 0.0725 {
		t.Fatalf("unexpected tax rate: %f", rule.TaxRate)
	}
}

func TestRuleEngineGetRuleNotFound(t *testing.T) {
	t.Parallel()
	engine := taxexempt.NewRuleEngine()
	_, err := engine.GetRule("ZZ-NOWHERE")
	if err == nil {
		t.Fatal("want error for missing rule, got nil")
	}
}

// ---------------------------------------------------------------------------
// Validator tests
// ---------------------------------------------------------------------------

func newEngine(t *testing.T) *taxexempt.RuleEngine {
	t.Helper()
	engine := taxexempt.NewRuleEngine()
	engine.AddRule(taxexempt.JurisdictionRule{
		Code:        "AU-NSW",
		Name:        "New South Wales",
		TaxRate:     0.10,
		ExemptTypes: []string{"government", "charity"},
	})
	return engine
}

func TestValidatorValidCert(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)
	v := &taxexempt.Validator{Engine: engine}

	now := time.Now()
	cert := taxexempt.Certificate{
		ID:               "V1",
		CustomerID:       "CUST-V",
		JurisdictionCode: "AU-NSW",
		ExemptType:       "charity",
		ValidFrom:        now.Add(-time.Hour),
		ValidTo:          now.Add(time.Hour),
		Verified:         true,
	}
	if err := v.Validate(cert, now); err != nil {
		t.Fatalf("expected no error for valid cert, got: %v", err)
	}
}

func TestValidatorExpiredCert(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)
	v := &taxexempt.Validator{Engine: engine}

	now := time.Now()
	cert := taxexempt.Certificate{
		ID:               "V2",
		CustomerID:       "CUST-V",
		JurisdictionCode: "AU-NSW",
		ExemptType:       "charity",
		ValidFrom:        now.Add(-2 * time.Hour),
		ValidTo:          now.Add(-time.Hour), // already expired
	}
	if err := v.Validate(cert, now); err == nil {
		t.Fatal("expected error for expired cert, got nil")
	}
}

func TestValidatorInvalidExemptType(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)
	v := &taxexempt.Validator{Engine: engine}

	now := time.Now()
	cert := taxexempt.Certificate{
		ID:               "V3",
		CustomerID:       "CUST-V",
		JurisdictionCode: "AU-NSW",
		ExemptType:       "retail", // not in rule
		ValidFrom:        now.Add(-time.Hour),
		ValidTo:          now.Add(time.Hour),
	}
	if err := v.Validate(cert, now); err == nil {
		t.Fatal("expected error for invalid exempt type, got nil")
	}
}
