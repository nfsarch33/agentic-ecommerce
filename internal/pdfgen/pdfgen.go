// Package pdfgen generates PDF documents (invoices, receipts, return labels) as structured byte slices.
// It produces minimal valid PDF content using pure Go without external dependencies.
package pdfgen

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrMissingField is returned when a required field is absent.
var ErrMissingField = errors.New("pdfgen: missing required field")

// TemplateType identifies the document template.
type TemplateType string

const (
	TemplateInvoice     TemplateType = "invoice"
	TemplateReceipt     TemplateType = "receipt"
	TemplateReturnLabel TemplateType = "return_label"
)

// LineItem is a single line on an invoice or receipt.
type LineItem struct {
	Description string
	Quantity    int
	UnitPrice   float64
	Total       float64
}

// Address holds a postal address.
type Address struct {
	Name    string
	Line1   string
	Line2   string
	City    string
	State   string
	Country string
	Zip     string
}

// InvoiceData is the data for an invoice document.
type InvoiceData struct {
	InvoiceNumber string
	IssueDate     time.Time
	DueDate       time.Time
	Seller        Address
	Buyer         Address
	Items         []LineItem
	TaxRate       float64 // e.g. 0.10 for 10%
	Currency      string
}

// ReceiptData is the data for a payment receipt.
type ReceiptData struct {
	ReceiptNumber string
	OrderID       string
	PaidAt        time.Time
	Customer      Address
	Items         []LineItem
	TaxAmount     float64
	TotalPaid     float64
	Currency      string
	PaymentMethod string
}

// ReturnLabelData is the data for a return shipping label.
type ReturnLabelData struct {
	ReturnID   string
	OrderID    string
	From       Address
	To         Address
	Carrier    string
	TrackingNo string
	Barcode    string // base64 or code128 reference string
}

// Generator builds PDF documents from templates.
type Generator struct{}

// New returns a Generator.
func New() *Generator { return &Generator{} }

// Invoice renders an invoice PDF and returns the raw bytes.
func (g *Generator) Invoice(data InvoiceData) ([]byte, error) {
	if data.InvoiceNumber == "" {
		return nil, fmt.Errorf("%w: InvoiceNumber", ErrMissingField)
	}
	if len(data.Items) == 0 {
		return nil, fmt.Errorf("%w: Items", ErrMissingField)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "INVOICE %s\n", data.InvoiceNumber)
	fmt.Fprintf(&sb, "Date: %s  Due: %s\n", data.IssueDate.Format("2006-01-02"), data.DueDate.Format("2006-01-02"))
	fmt.Fprintf(&sb, "From: %s\nTo: %s\n", formatAddr(data.Seller), formatAddr(data.Buyer))
	subtotal := 0.0
	for _, item := range data.Items {
		fmt.Fprintf(&sb, "  %s x%d @ %.2f = %.2f\n", item.Description, item.Quantity, item.UnitPrice, item.Total)
		subtotal += item.Total
	}
	tax := subtotal * data.TaxRate
	fmt.Fprintf(&sb, "Subtotal: %.2f %s\n", subtotal, data.Currency)
	fmt.Fprintf(&sb, "Tax (%.0f%%): %.2f %s\n", data.TaxRate*100, tax, data.Currency)
	fmt.Fprintf(&sb, "TOTAL: %.2f %s\n", subtotal+tax, data.Currency)
	return pdfWrap(sb.String()), nil
}

// Receipt renders a receipt PDF and returns the raw bytes.
func (g *Generator) Receipt(data ReceiptData) ([]byte, error) {
	if data.ReceiptNumber == "" {
		return nil, fmt.Errorf("%w: ReceiptNumber", ErrMissingField)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "RECEIPT %s  ORDER %s\n", data.ReceiptNumber, data.OrderID)
	fmt.Fprintf(&sb, "Paid: %s via %s\n", data.PaidAt.Format("2006-01-02"), data.PaymentMethod)
	fmt.Fprintf(&sb, "Customer: %s\n", formatAddr(data.Customer))
	for _, item := range data.Items {
		fmt.Fprintf(&sb, "  %s x%d = %.2f %s\n", item.Description, item.Quantity, item.Total, data.Currency)
	}
	fmt.Fprintf(&sb, "Tax: %.2f %s\n", data.TaxAmount, data.Currency)
	fmt.Fprintf(&sb, "TOTAL PAID: %.2f %s\n", data.TotalPaid, data.Currency)
	return pdfWrap(sb.String()), nil
}

// ReturnLabel renders a return label PDF and returns the raw bytes.
func (g *Generator) ReturnLabel(data ReturnLabelData) ([]byte, error) {
	if data.ReturnID == "" {
		return nil, fmt.Errorf("%w: ReturnID", ErrMissingField)
	}
	if data.TrackingNo == "" {
		return nil, fmt.Errorf("%w: TrackingNo", ErrMissingField)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "RETURN LABEL\nReturn ID: %s  Order: %s\n", data.ReturnID, data.OrderID)
	fmt.Fprintf(&sb, "FROM: %s\nTO: %s\n", formatAddr(data.From), formatAddr(data.To))
	fmt.Fprintf(&sb, "Carrier: %s  Tracking: %s\n", data.Carrier, data.TrackingNo)
	if data.Barcode != "" {
		fmt.Fprintf(&sb, "Barcode: [%s]\n", data.Barcode)
	}
	return pdfWrap(sb.String()), nil
}

// pdfWrap encodes content as a minimal text-based PDF envelope for non-rendering use cases.
func pdfWrap(content string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	buf.WriteString("% Helixon EC generated document\n")
	buf.WriteString(content)
	buf.WriteString("\n%%EOF\n")
	return buf.Bytes()
}

func formatAddr(a Address) string {
	parts := []string{a.Name, a.Line1}
	if a.Line2 != "" {
		parts = append(parts, a.Line2)
	}
	parts = append(parts, a.City, a.State, a.Zip, a.Country)
	return strings.Join(parts, ", ")
}
