package invoicegen_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/invoicegen"
)

// ---------------------------------------------------------------------------
// InvoiceNumber tests
// ---------------------------------------------------------------------------

func TestInvoiceNumberString(t *testing.T) {
	t.Parallel()
	n := invoicegen.InvoiceNumber{Prefix: "INV", Seq: 42}
	if got := n.String(); got != "INV-000042" {
		t.Fatalf("want INV-000042, got %q", got)
	}
}

func TestInvoiceNumberLeadingZeroes(t *testing.T) {
	t.Parallel()
	n := invoicegen.InvoiceNumber{Prefix: "RCP", Seq: 1}
	if got := n.String(); got != "RCP-000001" {
		t.Fatalf("want RCP-000001, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Sequencer tests
// ---------------------------------------------------------------------------

func TestSequencerSequential(t *testing.T) {
	t.Parallel()
	s := invoicegen.NewSequencer()
	n1 := s.Next("INV")
	n2 := s.Next("INV")
	n3 := s.Next("INV")

	if n1.Seq != 1 || n2.Seq != 2 || n3.Seq != 3 {
		t.Fatalf("want seqs 1,2,3 got %d,%d,%d", n1.Seq, n2.Seq, n3.Seq)
	}
}

func TestSequencerSeparatePrefixes(t *testing.T) {
	t.Parallel()
	s := invoicegen.NewSequencer()
	a := s.Next("AAA")
	b := s.Next("BBB")
	a2 := s.Next("AAA")

	if a.Seq != 1 || b.Seq != 1 || a2.Seq != 2 {
		t.Fatalf("prefix counters not isolated: AAA1=%d BBB1=%d AAA2=%d", a.Seq, b.Seq, a2.Seq)
	}
}

func TestSequencerConcurrencySafe(t *testing.T) {
	t.Parallel()
	s := invoicegen.NewSequencer()
	const goroutines = 50

	results := make([]invoicegen.InvoiceNumber, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		idx := i
		go func() {
			defer wg.Done()
			results[idx] = s.Next("RACE")
		}()
	}
	wg.Wait()

	// All sequences 1..50 must appear exactly once.
	seen := make(map[int]bool, goroutines)
	for _, n := range results {
		if seen[n.Seq] {
			t.Fatalf("duplicate sequence number: %d", n.Seq)
		}
		seen[n.Seq] = true
	}
	for i := 1; i <= goroutines; i++ {
		if !seen[i] {
			t.Fatalf("missing sequence number: %d", i)
		}
	}
}

// ---------------------------------------------------------------------------
// InvoiceBuilder tests
// ---------------------------------------------------------------------------

func buildSampleInvoice(t *testing.T) invoicegen.Invoice {
	t.Helper()
	s := invoicegen.NewSequencer()
	num := s.Next("TEST")
	now := time.Now()

	inv := invoicegen.NewInvoiceBuilder(num, now).
		SetSeller(invoicegen.Address{Name: "Acme Corp", Line1: "1 Main St", City: "Sydney", Country: "AU"}).
		SetBuyer(invoicegen.Address{Name: "Client Ltd", Line1: "2 Bay St", City: "Melbourne", Country: "AU"}).
		AddLineItem("Widget A", 3, 10.00).
		AddLineItem("Widget B", 2, 25.00).
		AddTax(0.10, "GST").
		Build()
	return inv
}

func TestInvoiceSubtotal(t *testing.T) {
	t.Parallel()
	inv := buildSampleInvoice(t)
	// 3*10 + 2*25 = 80
	if inv.Subtotal != 80.0 {
		t.Fatalf("want subtotal 80.00, got %.2f", inv.Subtotal)
	}
}

func TestInvoiceTaxTotal(t *testing.T) {
	t.Parallel()
	inv := buildSampleInvoice(t)
	// 10% of 80 = 8
	if inv.TaxTotal != 8.0 {
		t.Fatalf("want tax total 8.00, got %.2f", inv.TaxTotal)
	}
}

func TestInvoiceGrandTotal(t *testing.T) {
	t.Parallel()
	inv := buildSampleInvoice(t)
	// 80 + 8 = 88
	if inv.GrandTotal != 88.0 {
		t.Fatalf("want grand total 88.00, got %.2f", inv.GrandTotal)
	}
}

func TestInvoiceBuilderPattern(t *testing.T) {
	t.Parallel()
	s := invoicegen.NewSequencer()
	num := s.Next("ORD")
	now := time.Now()
	inv := invoicegen.NewInvoiceBuilder(num, now).
		SetSeller(invoicegen.Address{Name: "Seller"}).
		SetBuyer(invoicegen.Address{Name: "Buyer"}).
		AddLineItem("Item", 1, 100.00).
		Build()

	if inv.Seller.Name != "Seller" {
		t.Fatalf("unexpected seller: %q", inv.Seller.Name)
	}
	if inv.Buyer.Name != "Buyer" {
		t.Fatalf("unexpected buyer: %q", inv.Buyer.Name)
	}
	if len(inv.Lines) != 1 {
		t.Fatalf("want 1 line item, got %d", len(inv.Lines))
	}
}

// ---------------------------------------------------------------------------
// PDFBytes tests
// ---------------------------------------------------------------------------

func TestPDFBytesContainsInvoiceNumber(t *testing.T) {
	t.Parallel()
	inv := buildSampleInvoice(t)
	pdf := invoicegen.PDFBytes(inv)

	if len(pdf) == 0 {
		t.Fatal("PDFBytes returned empty slice")
	}
	content := string(pdf)
	if !strings.Contains(content, "%PDF") {
		t.Error("PDF output missing %%PDF header")
	}
	if !strings.Contains(content, inv.Number.String()) {
		t.Errorf("PDF output missing invoice number %q", inv.Number.String())
	}
}

func TestPDFBytesContainsTotals(t *testing.T) {
	t.Parallel()
	inv := buildSampleInvoice(t)
	pdf := invoicegen.PDFBytes(inv)
	content := string(pdf)

	if !strings.Contains(content, "88.00") {
		t.Errorf("PDF output missing grand total; content snippet: %q", content[:min(200, len(content))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
