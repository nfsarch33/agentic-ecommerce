package bulk_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/admin/bulk"
)

var validCSV = `sku,title,quantity,price
SKU-001,Widget A,10,1999
SKU-002,Widget B,5,2999
`

func TestImport_ValidCSV(t *testing.T) {
	t.Parallel()
	proc := bulk.NewBulkProcessor()
	result, err := proc.Import(strings.NewReader(validCSV), "csv")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Imported != 2 {
		t.Fatalf("expected 2 imported, got %d", result.Imported)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected 0 errors, got %v", result.Errors)
	}
}

func TestImport_PartialSuccess(t *testing.T) {
	t.Parallel()
	badCSV := `sku,title,quantity,price
SKU-001,Good Widget,10,1999
,Missing SKU,5,2999
SKU-003,Another Good,3,999
`
	proc := bulk.NewBulkProcessor()
	result, err := proc.Import(strings.NewReader(badCSV), "csv")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Imported != 2 {
		t.Fatalf("expected 2 imported, got %d", result.Imported)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
}

func TestImport_InvalidFormat(t *testing.T) {
	t.Parallel()
	proc := bulk.NewBulkProcessor()
	_, err := proc.Import(strings.NewReader("{}"), "xml")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestExportRoundtrip_CSV(t *testing.T) {
	t.Parallel()
	proc := bulk.NewBulkProcessor()

	_, err := proc.Import(strings.NewReader(validCSV), "csv")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	var buf bytes.Buffer
	if err := proc.Export(bulk.ExportFilter{}, &buf); err != nil {
		t.Fatalf("Export: %v", err)
	}

	exported := buf.String()
	if !strings.Contains(exported, "SKU-001") || !strings.Contains(exported, "SKU-002") {
		t.Fatalf("exported CSV missing items: %s", exported)
	}
}

func TestImport_EmptyCSV(t *testing.T) {
	t.Parallel()
	proc := bulk.NewBulkProcessor()
	result, err := proc.Import(strings.NewReader("sku,title,quantity,price\n"), "csv")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Imported != 0 {
		t.Fatalf("expected 0 imported for empty CSV, got %d", result.Imported)
	}
}

func TestExport_FilterBySKUPrefix(t *testing.T) {
	t.Parallel()
	proc := bulk.NewBulkProcessor()
	proc.Import(strings.NewReader(validCSV), "csv")

	var buf bytes.Buffer
	proc.Export(bulk.ExportFilter{SKUPrefix: "SKU-001"}, &buf)
	out := buf.String()
	if strings.Contains(out, "SKU-002") {
		t.Fatal("export filter should have excluded SKU-002")
	}
}
