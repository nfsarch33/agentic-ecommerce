package importer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/adapter/woocommerce"
)

type mockWCClient struct {
	calls    int
	batches  [][]woocommerce.Product
	failOn   int
	failWith error
}

func (m *mockWCClient) BatchCreateProducts(_ context.Context, products []woocommerce.Product) (*woocommerce.BatchResult, error) {
	m.calls++
	m.batches = append(m.batches, products)
	if m.failOn > 0 && m.calls == m.failOn {
		return nil, m.failWith
	}
	created := make([]woocommerce.Product, len(products))
	for i, p := range products {
		p.ID = m.calls*100 + i
		created[i] = p
	}
	return &woocommerce.BatchResult{Create: created}, nil
}

func TestImportCSVColumnAliasesAndBatches(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "products.csv", "title,price,sku\nYoga Mat,29.99,YM-1\nBottle,12.50,BT-1\n")
	client := &mockWCClient{}
	result := New(client, testLogger()).Run(context.Background(), ImportRequest{
		Source:     path,
		SourceType: "csv",
		BatchSize:  1,
	})

	if !result.Success {
		t.Fatalf("result = %+v", result)
	}
	if result.ImportedCount != 2 || client.calls != 2 {
		t.Fatalf("result = %+v calls=%d", result, client.calls)
	}
	if got := client.batches[0][0].Name; got != "Yoga Mat" {
		t.Fatalf("name = %q, want Yoga Mat", got)
	}
	if got := client.batches[0][0].Regular; got != "29.99" {
		t.Fatalf("regular = %q, want 29.99", got)
	}
}

func TestImportJSONWrappedDryRunDoesNotCallWooCommerce(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "products.json", `{"products":[{"name":"Widget","regular_price":"9.99","sku":"W-1"}]}`)
	client := &mockWCClient{}
	result := New(client, testLogger()).Run(context.Background(), ImportRequest{
		Source:     path,
		SourceType: "json",
		DryRun:     true,
	})

	if !result.Success || result.ImportedCount != 1 {
		t.Fatalf("result = %+v", result)
	}
	if client.calls != 0 {
		t.Fatalf("calls = %d, want 0", client.calls)
	}
}

func TestImportPartialFailureKeepsSuccessfulBatches(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "products.csv", "name,regular_price\nA,1.00\nB,2.00\nC,3.00\n")
	client := &mockWCClient{failOn: 2, failWith: errors.New("bad gateway")}
	result := New(client, testLogger()).Run(context.Background(), ImportRequest{
		Source:     path,
		SourceType: "csv",
		BatchSize:  1,
	})

	if !result.Success {
		t.Fatalf("partial failures should keep success true: %+v", result)
	}
	if result.ImportedCount != 2 || result.FailedCount != 1 || len(result.Errors) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestImportRejectsMissingSource(t *testing.T) {
	t.Parallel()

	result := New(&mockWCClient{}, testLogger()).Run(context.Background(), ImportRequest{SourceType: "csv"})
	if result.Success || result.Error == "" {
		t.Fatalf("result = %+v, want missing source failure", result)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
