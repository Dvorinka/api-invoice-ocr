package ocr

import (
	"context"
	"testing"
)

func TestExtractFromText(t *testing.T) {
	service := NewService(10 << 20)

	text := `
ACME Corporation
Invoice Number: INV-2026-001
Date: 2026-02-20
VAT: CZ12345678
Consulting services 2 x 150.00 = 300.00
Subtotal: USD 300.00
Tax: USD 63.00
Total Due: USD 363.00
`

	result, err := service.ExtractFromText(text)
	if err != nil {
		t.Fatalf("extract from text: %v", err)
	}

	if result.InvoiceNumber != "INV-2026-001" {
		t.Fatalf("unexpected invoice number: %q", result.InvoiceNumber)
	}
	if result.SupplierName != "ACME Corporation" {
		t.Fatalf("unexpected supplier: %q", result.SupplierName)
	}
	if result.VATNumber != "CZ12345678" {
		t.Fatalf("unexpected vat number: %q", result.VATNumber)
	}
	if result.Currency != "USD" {
		t.Fatalf("unexpected currency: %q", result.Currency)
	}
	if result.TotalAmount != 363 {
		t.Fatalf("unexpected total amount: %f", result.TotalAmount)
	}
	if len(result.LineItems) != 1 {
		t.Fatalf("expected one line item, got %d", len(result.LineItems))
	}
}

func TestExtractFromFileTextPlain(t *testing.T) {
	service := NewService(1024)
	data := []byte("Invoice # A-77\nTotal: EUR 99.90\n")

	result, err := service.ExtractFromFile(context.Background(), data, "invoice.txt", "text/plain")
	if err != nil {
		t.Fatalf("extract from file: %v", err)
	}

	if result.UsedEngine != "plain_text" {
		t.Fatalf("expected plain_text engine, got %q", result.UsedEngine)
	}
	if result.InvoiceNumber != "A-77" {
		t.Fatalf("unexpected invoice number: %q", result.InvoiceNumber)
	}
	if result.TotalAmount != 99.9 {
		t.Fatalf("unexpected total amount: %f", result.TotalAmount)
	}
}

func TestExtractFromFileRejectsOversize(t *testing.T) {
	service := NewService(10)
	data := []byte("01234567890")

	_, err := service.ExtractFromFile(context.Background(), data, "invoice.txt", "text/plain")
	if err == nil {
		t.Fatalf("expected oversize error")
	}
}
