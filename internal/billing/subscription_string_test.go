// File scope: v6.1.0 coverage backfill -- InvoiceStatus.String was
// uncovered. The Stringer is referenced by adapters that persist the
// canonical string; pinning the round-trip protects against silent
// breakage.
package billing

import "testing"

func TestInvoiceStatusStringMatchesUnderlyingLiteral(t *testing.T) {
	t.Parallel()
	for _, s := range []InvoiceStatus{InvoiceOpen, InvoicePaid, InvoiceVoid, InvoiceUncollectible} {
		if got := s.String(); got != string(s) {
			t.Errorf("InvoiceStatus(%s).String() = %q, want %q", s, got, string(s))
		}
	}
}
