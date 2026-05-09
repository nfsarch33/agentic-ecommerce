// File scope: v3.2.1 QA Task 4 -- WooCommerce idempotent re-run
// produces zero duplicates (per ADR-028 EC-2-3 acceptance:
// "idempotent re-run produces zero duplicates").
//
// The v3.2.0 RED test (TestSEO_InjectsLongTailKeywordsFromTrendData)
// already exercises a 2-call idempotent path. The v3.2.1 acceptance
// criterion in the parent plan widens this to a 3-run sweep against
// a richer in-memory WC store stub that records every upsert with
// the full payload so the test can assert:
//
//   - Exactly 1 unique SKU stored after 3 sequential Inject runs.
//   - Importer.Calls == 3 (the operator-facing call count is
//     unchanged on idempotent re-import).
//   - Importer.NewSKUs == 1 (only the first run created a row).
//   - Each post-first-run upsert overwrites the existing row in
//     place, leaving zero duplicate WC products.
//   - The trend-keyword tags + meta description survive every
//     re-run so the keyword-density signal stays stable.
//
// Cite skill: go-clean-architecture (the test exercises the
// CatalogueImporter port contract; production wiring uses
// internal/adapter/woocommerce.Client).
package seo

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestProductSEO_WCSyncIdempotent_ZeroDuplicates is the v3.2.1
// QA-4 acceptance test.
func TestProductSEO_WCSyncIdempotent_ZeroDuplicates(t *testing.T) {
	t.Parallel()

	trends := &fakeTrendKeywordSource{
		responses: map[string][]string{
			"earbuds:cylrl": {"wireless earbuds 2026", "noise cancelling earbuds", "long battery earbuds"},
		},
	}
	store := &recordingWCStore{}
	injector, err := NewProductSEO(nil, ProductSEOConfig{
		Trends:   trends,
		Importer: store,
		TenantID: "cylrl",
		Now:      func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewProductSEO: %v", err)
	}
	t.Cleanup(func() { _ = injector.Close(context.Background()) })

	req := SEOInjectRequest{
		Product: SEOProduct{
			ID:          "earbuds-001",
			Title:       "Premium Wireless Earbuds",
			Description: "Crisp sound and 36-hour battery life.",
			Topic:       "earbuds",
			Categories:  []string{"electronics"},
			PriceCents:  4500,
			Stock:       42,
		},
	}

	// Run the inject path 3 times against the same product
	// fixture. The plan calls out "3 times" verbatim.
	const runs = 3
	var lastResult SEOInjectResult
	for i := 0; i < runs; i++ {
		res, err := injector.Inject(context.Background(), req)
		if err != nil {
			t.Fatalf("Inject(run %d): %v", i+1, err)
		}
		lastResult = res
	}

	// Assertion 1: exactly 3 calls landed at the importer.
	if got := store.Calls(); got != runs {
		t.Fatalf("importer Calls = %d, want %d (call cardinal stays stable on idempotent re-import)", got, runs)
	}

	// Assertion 2: exactly 1 unique SKU stored. Zero duplicates.
	uniqueSKUs := store.UniqueSKUs()
	if len(uniqueSKUs) != 1 {
		t.Fatalf("unique SKUs = %v, want exactly 1 (idempotent re-run produces zero duplicates per EC-2-3)", uniqueSKUs)
	}
	if uniqueSKUs[0] != "earbuds-001" {
		t.Fatalf("stored SKU = %q, want earbuds-001", uniqueSKUs[0])
	}

	// Assertion 3: only the first call created a new SKU; the
	// remaining (runs - 1) calls hit the existing row.
	if got := store.NewSKUs(); got != 1 {
		t.Fatalf("NewSKUs = %d, want 1 (idempotent re-run must not create duplicates)", got)
	}

	// Assertion 4: the latest stored payload still carries the
	// tenant scope + the trend-keyword tags. If a future regression
	// reset Tags on overwrite, this guard catches it.
	stored, ok := store.Get("earbuds-001")
	if !ok {
		t.Fatal("stored SKU lookup failed")
	}
	if stored.TenantID != "cylrl" {
		t.Fatalf("stored TenantID = %q, want cylrl", stored.TenantID)
	}
	if len(stored.Tags) == 0 {
		t.Fatal("stored Tags empty; expected trend-keyword tags to survive idempotent re-runs")
	}

	// Assertion 5: the operator-facing result still reports the
	// trend store was used on the third run (no silent regression
	// where idempotent re-runs skip the trend lookup).
	if !lastResult.UsedTrendData {
		t.Fatal("UsedTrendData = false on final run; expected true on every idempotent re-run")
	}

	// Assertion 6: every WC payload across the 3 runs is byte-
	// identical. Guards against sneaky non-determinism (e.g. map
	// iteration leaking into the keyword tag order).
	if !store.PayloadsIdentical() {
		t.Fatalf("WC upsert payloads diverged across idempotent re-runs:\n%s", store.PayloadDiff())
	}
}

// recordingWCStore is the in-test WooCommerce stub. It satisfies
// the CatalogueImporter port and snapshots every Upsert payload so
// the test can verify zero duplicate row creates and identical
// payloads across the three runs.
type recordingWCStore struct {
	mu       sync.Mutex
	rows     map[string]CatalogueUpsertRequest
	calls    int
	newSKUs  int
	payloads []CatalogueUpsertRequest
}

func (s *recordingWCStore) Upsert(_ context.Context, req CatalogueUpsertRequest) (CatalogueUpsertResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = make(map[string]CatalogueUpsertRequest)
	}
	s.calls++
	s.payloads = append(s.payloads, req)
	created := false
	if _, ok := s.rows[req.SKU]; !ok {
		s.newSKUs++
		created = true
	}
	s.rows[req.SKU] = req
	return CatalogueUpsertResult{SKU: req.SKU, Created: created}, nil
}

func (s *recordingWCStore) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *recordingWCStore) NewSKUs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newSKUs
}

func (s *recordingWCStore) UniqueSKUs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.rows))
	for k := range s.rows {
		out = append(out, k)
	}
	return out
}

func (s *recordingWCStore) Get(sku string) (CatalogueUpsertRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[sku]
	return r, ok
}

// PayloadsIdentical returns true when every captured Upsert payload
// is byte-identical (ignoring slice header pointers; the comparison
// uses element-by-element equality on the public fields).
func (s *recordingWCStore) PayloadsIdentical() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.payloads) <= 1 {
		return true
	}
	first := s.payloads[0]
	for _, p := range s.payloads[1:] {
		if !payloadsEqual(first, p) {
			return false
		}
	}
	return true
}

// PayloadDiff returns a small textual diff of the first two
// divergent payloads. Used by the test assertion error message so
// future regressions surface the exact field that drifted.
func (s *recordingWCStore) PayloadDiff() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.payloads) < 2 {
		return "<single payload>"
	}
	return diffPayloads(s.payloads[0], s.payloads[len(s.payloads)-1])
}

func payloadsEqual(a, b CatalogueUpsertRequest) bool {
	if a.TenantID != b.TenantID || a.SKU != b.SKU || a.Title != b.Title || a.Description != b.Description {
		return false
	}
	if a.MetaTitle != b.MetaTitle || a.MetaDesc != b.MetaDesc {
		return false
	}
	if a.PriceCents != b.PriceCents || a.Stock != b.Stock {
		return false
	}
	if !stringSlicesEqual(a.Tags, b.Tags) {
		return false
	}
	if !stringSlicesEqual(a.Categories, b.Categories) {
		return false
	}
	return true
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func diffPayloads(a, b CatalogueUpsertRequest) string {
	mismatches := []string{}
	if a.Title != b.Title {
		mismatches = append(mismatches, "Title: "+a.Title+" vs "+b.Title)
	}
	if a.MetaTitle != b.MetaTitle {
		mismatches = append(mismatches, "MetaTitle: "+a.MetaTitle+" vs "+b.MetaTitle)
	}
	if a.MetaDesc != b.MetaDesc {
		mismatches = append(mismatches, "MetaDesc: "+a.MetaDesc+" vs "+b.MetaDesc)
	}
	if !stringSlicesEqual(a.Tags, b.Tags) {
		mismatches = append(mismatches, "Tags: differing")
	}
	if len(mismatches) == 0 {
		return "<no field-level diff detected>"
	}
	out := ""
	for _, m := range mismatches {
		out += " - " + m + "\n"
	}
	return out
}
