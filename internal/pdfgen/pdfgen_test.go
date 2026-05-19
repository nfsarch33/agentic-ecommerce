package pdfgen_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/pdfgen"
)

var testAddr = pdfgen.Address{Name: "Acme Corp", Line1: "1 Main St", City: "Sydney", State: "NSW", Country: "AU", Zip: "2000"}
var testBuyer = pdfgen.Address{Name: "Jane Doe", Line1: "5 Oak Ave", City: "Melbourne", State: "VIC", Country: "AU", Zip: "3000"}

func items() []pdfgen.LineItem {
	return []pdfgen.LineItem{
		{Description: "Widget", Quantity: 2, UnitPrice: 25.00, Total: 50.00},
		{Description: "Gadget", Quantity: 1, UnitPrice: 99.00, Total: 99.00},
	}
}

// --- Invoice ---

func TestGenerator_Invoice_Success(t *testing.T) {
	t.Parallel()
	g := pdfgen.New()
	data := pdfgen.InvoiceData{
		InvoiceNumber: "INV-001",
		IssueDate:     time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		DueDate:       time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
		Seller:        testAddr,
		Buyer:         testBuyer,
		Items:         items(),
		TaxRate:       0.10,
		Currency:      "AUD",
	}
	pdf, err := g.Invoice(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(pdf, []byte("INV-001")) {
		t.Fatal("PDF must contain invoice number")
	}
	if !bytes.Contains(pdf, []byte("%PDF-1.4")) {
		t.Fatal("PDF must start with PDF header")
	}
}

func TestGenerator_Invoice_MissingNumber(t *testing.T) {
	t.Parallel()
	g := pdfgen.New()
	_, err := g.Invoice(pdfgen.InvoiceData{Items: items()})
	if !errors.Is(err, pdfgen.ErrMissingField) {
		t.Fatalf("want ErrMissingField, got %v", err)
	}
}

func TestGenerator_Invoice_EmptyItems(t *testing.T) {
	t.Parallel()
	g := pdfgen.New()
	_, err := g.Invoice(pdfgen.InvoiceData{InvoiceNumber: "INV-002"})
	if !errors.Is(err, pdfgen.ErrMissingField) {
		t.Fatalf("want ErrMissingField for empty items, got %v", err)
	}
}

// --- Receipt ---

func TestGenerator_Receipt_Success(t *testing.T) {
	t.Parallel()
	g := pdfgen.New()
	data := pdfgen.ReceiptData{
		ReceiptNumber: "RCP-100",
		OrderID:       "ORD-500",
		PaidAt:        time.Now(),
		Customer:      testBuyer,
		Items:         items(),
		TaxAmount:     14.90,
		TotalPaid:     163.90,
		Currency:      "AUD",
		PaymentMethod: "card",
	}
	pdf, err := g.Receipt(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(pdf, []byte("RCP-100")) {
		t.Fatal("PDF must contain receipt number")
	}
}

func TestGenerator_Receipt_MissingNumber(t *testing.T) {
	t.Parallel()
	g := pdfgen.New()
	_, err := g.Receipt(pdfgen.ReceiptData{})
	if !errors.Is(err, pdfgen.ErrMissingField) {
		t.Fatalf("want ErrMissingField, got %v", err)
	}
}

// --- ReturnLabel ---

func TestGenerator_ReturnLabel_Success(t *testing.T) {
	t.Parallel()
	g := pdfgen.New()
	data := pdfgen.ReturnLabelData{
		ReturnID:   "RTN-001",
		OrderID:    "ORD-500",
		From:       testBuyer,
		To:         testAddr,
		Carrier:    "AusPost",
		TrackingNo: "AP123456789AU",
		Barcode:    "AP123456789AU",
	}
	pdf, err := g.ReturnLabel(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(pdf, []byte("AP123456789AU")) {
		t.Fatal("PDF must contain tracking number")
	}
}

func TestGenerator_ReturnLabel_MissingReturnID(t *testing.T) {
	t.Parallel()
	g := pdfgen.New()
	_, err := g.ReturnLabel(pdfgen.ReturnLabelData{TrackingNo: "X"})
	if !errors.Is(err, pdfgen.ErrMissingField) {
		t.Fatalf("want ErrMissingField, got %v", err)
	}
}

func TestGenerator_ReturnLabel_MissingTracking(t *testing.T) {
	t.Parallel()
	g := pdfgen.New()
	_, err := g.ReturnLabel(pdfgen.ReturnLabelData{ReturnID: "RTN-002"})
	if !errors.Is(err, pdfgen.ErrMissingField) {
		t.Fatalf("want ErrMissingField, got %v", err)
	}
}
