// Package invoicegen provides sequential invoice numbering, tax breakdown
// calculation, and minimal text-based PDF generation without external
// dependencies.
package invoicegen

import (
	"bytes"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// InvoiceNumber
// ---------------------------------------------------------------------------

// InvoiceNumber represents a human-readable sequential invoice identifier.
type InvoiceNumber struct {
	Prefix string
	Seq    int
}

// String returns the formatted invoice number, e.g. "INV-000042".
func (n InvoiceNumber) String() string {
	return fmt.Sprintf("%s-%06d", n.Prefix, n.Seq)
}

// ---------------------------------------------------------------------------
// Sequencer
// ---------------------------------------------------------------------------

// Sequencer generates monotonically increasing InvoiceNumber values per prefix.
// It is safe for concurrent use.
type Sequencer struct {
	mu      sync.Mutex
	counter map[string]int
}

// NewSequencer returns an initialised Sequencer.
func NewSequencer() *Sequencer {
	return &Sequencer{counter: make(map[string]int)}
}

// Next atomically increments the counter for prefix and returns the next
// InvoiceNumber.
func (s *Sequencer) Next(prefix string) InvoiceNumber {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter[prefix]++
	return InvoiceNumber{Prefix: prefix, Seq: s.counter[prefix]}
}

// ---------------------------------------------------------------------------
// Address
// ---------------------------------------------------------------------------

// Address holds a postal address used on invoices.
type Address struct {
	Name    string
	Line1   string
	City    string
	State   string
	Country string
	Zip     string
}

// ---------------------------------------------------------------------------
// LineItem
// ---------------------------------------------------------------------------

// LineItem represents a single product or service on an invoice.
type LineItem struct {
	Description string
	Quantity    int
	UnitPrice   float64
	Total       float64
}

// ---------------------------------------------------------------------------
// TaxLine
// ---------------------------------------------------------------------------

// TaxLine represents a tax charge applied to the invoice subtotal.
type TaxLine struct {
	Description string
	Rate        float64
	Base        float64
	Amount      float64
}

// ---------------------------------------------------------------------------
// Invoice
// ---------------------------------------------------------------------------

// Invoice is the fully assembled invoice document.
type Invoice struct {
	Number     InvoiceNumber
	Seller     Address
	Buyer      Address
	Lines      []LineItem
	TaxLines   []TaxLine
	Subtotal   float64
	TaxTotal   float64
	GrandTotal float64
	IssuedAt   time.Time
}

// ---------------------------------------------------------------------------
// InvoiceBuilder
// ---------------------------------------------------------------------------

// InvoiceBuilder assembles an Invoice using the builder pattern.
type InvoiceBuilder struct {
	number   InvoiceNumber
	seller   Address
	buyer    Address
	lines    []LineItem
	taxRates []struct {
		rate float64
		desc string
	}
	issuedAt time.Time
}

// NewInvoiceBuilder returns an InvoiceBuilder with the given pre-assigned
// number and issue timestamp.
func NewInvoiceBuilder(number InvoiceNumber, issuedAt time.Time) *InvoiceBuilder {
	return &InvoiceBuilder{number: number, issuedAt: issuedAt}
}

// SetSeller sets the seller address.
func (b *InvoiceBuilder) SetSeller(addr Address) *InvoiceBuilder {
	b.seller = addr
	return b
}

// SetBuyer sets the buyer address.
func (b *InvoiceBuilder) SetBuyer(addr Address) *InvoiceBuilder {
	b.buyer = addr
	return b
}

// AddLineItem appends a line item, computing its Total as qty * unit.
func (b *InvoiceBuilder) AddLineItem(desc string, qty int, unit float64) *InvoiceBuilder {
	b.lines = append(b.lines, LineItem{
		Description: desc,
		Quantity:    qty,
		UnitPrice:   unit,
		Total:       float64(qty) * unit,
	})
	return b
}

// AddTax registers a tax with a fractional rate (e.g. 0.10 for 10%) and a
// human-readable description.  The tax is applied to the subtotal at Build time.
func (b *InvoiceBuilder) AddTax(rate float64, desc string) *InvoiceBuilder {
	b.taxRates = append(b.taxRates, struct {
		rate float64
		desc string
	}{rate, desc})
	return b
}

// Build computes totals and returns the assembled Invoice.
func (b *InvoiceBuilder) Build() Invoice {
	var subtotal float64
	for _, li := range b.lines {
		subtotal += li.Total
	}

	var taxLines []TaxLine
	var taxTotal float64
	for _, tr := range b.taxRates {
		amount := subtotal * tr.rate
		taxLines = append(taxLines, TaxLine{
			Description: tr.desc,
			Rate:        tr.rate,
			Base:        subtotal,
			Amount:      amount,
		})
		taxTotal += amount
	}

	// Defensive copies of slices.
	linesCopy := make([]LineItem, len(b.lines))
	copy(linesCopy, b.lines)

	return Invoice{
		Number:     b.number,
		Seller:     b.seller,
		Buyer:      b.buyer,
		Lines:      linesCopy,
		TaxLines:   taxLines,
		Subtotal:   subtotal,
		TaxTotal:   taxTotal,
		GrandTotal: subtotal + taxTotal,
		IssuedAt:   b.issuedAt,
	}
}

// ---------------------------------------------------------------------------
// PDFBytes -- minimal text-based PDF (no external dependencies)
// ---------------------------------------------------------------------------

// PDFBytes serialises inv into a minimal, standards-conformant PDF containing
// the invoice number, party names, line items, and totals.  No images or
// embedded fonts are used.
func PDFBytes(inv Invoice) []byte {
	// Build stream content first so we know its length.
	var stream bytes.Buffer
	w := func(format string, args ...interface{}) {
		fmt.Fprintf(&stream, format, args...)
	}

	w("BT\n")
	w("/F1 14 Tf\n")
	w("50 780 Td\n")
	w("(INVOICE: %s) Tj\n", inv.Number.String())
	w("0 -20 Td\n")
	w("/F1 10 Tf\n")
	w("(Issued: %s) Tj\n", inv.IssuedAt.Format("2006-01-02"))
	w("0 -20 Td\n")
	w("(Seller: %s) Tj\n", inv.Seller.Name)
	w("0 -15 Td\n")
	w("(Buyer: %s) Tj\n", inv.Buyer.Name)
	w("0 -20 Td\n")
	for _, li := range inv.Lines {
		w("(%s  x%d @ %.2f = %.2f) Tj\n", li.Description, li.Quantity, li.UnitPrice, li.Total)
		w("0 -15 Td\n")
	}
	w("0 -10 Td\n")
	w("(Subtotal: %.2f) Tj\n", inv.Subtotal)
	w("0 -15 Td\n")
	for _, tl := range inv.TaxLines {
		w("(%s %.0f%%: %.2f) Tj\n", tl.Description, tl.Rate*100, tl.Amount)
		w("0 -15 Td\n")
	}
	w("(TOTAL: %.2f) Tj\n", inv.GrandTotal)
	w("ET\n")

	streamBytes := stream.Bytes()
	streamLen := len(streamBytes)

	// Assemble minimal PDF structure.
	var pdf bytes.Buffer
	p := func(format string, args ...interface{}) {
		fmt.Fprintf(&pdf, format, args...)
	}

	p("%%PDF-1.4\n")

	// Object 1: catalog
	off1 := pdf.Len()
	p("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	// Object 2: pages
	off2 := pdf.Len()
	p("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	// Object 3: page
	off3 := pdf.Len()
	p("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]")
	p(" /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")

	// Object 4: content stream
	off4 := pdf.Len()
	p("4 0 obj\n<< /Length %d >>\nstream\n", streamLen)
	pdf.Write(streamBytes)
	p("\nendstream\nendobj\n")

	// Object 5: font
	off5 := pdf.Len()
	p("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	// Cross-reference table
	xrefOffset := pdf.Len()
	p("xref\n0 6\n")
	p("0000000000 65535 f \n")
	p("%010d 00000 n \n", off1)
	p("%010d 00000 n \n", off2)
	p("%010d 00000 n \n", off3)
	p("%010d 00000 n \n", off4)
	p("%010d 00000 n \n", off5)
	p("trailer\n<< /Size 6 /Root 1 0 R >>\n")
	p("startxref\n%d\n", xrefOffset)
	p("%%%%EOF\n")

	return pdf.Bytes()
}
