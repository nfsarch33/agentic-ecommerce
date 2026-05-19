package bulk

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// Item is a product record used in bulk operations.
type Item struct {
	SKU      string
	Title    string
	Quantity int
	Price    int
}

// ImportError captures a row-level validation failure.
type ImportError struct {
	Row     int
	Message string
}

func (e ImportError) Error() string { return fmt.Sprintf("row %d: %s", e.Row, e.Message) }

// ImportResult summarises the outcome of a bulk import.
type ImportResult struct {
	Imported int
	Errors   []ImportError
}

// ExportFilter constrains which records are exported.
type ExportFilter struct {
	SKUPrefix string
}

// BulkProcessor handles bulk import and export of inventory records.
type BulkProcessor struct {
	mu    sync.RWMutex
	items []Item
}

func NewBulkProcessor() *BulkProcessor { return &BulkProcessor{} }

// Import reads records from r in the given format ("csv") and stores valid rows.
func (p *BulkProcessor) Import(r io.Reader, format string) (ImportResult, error) {
	if format != "csv" {
		return ImportResult{}, errors.New("unsupported format: " + format)
	}
	return p.importCSV(r)
}

func (p *BulkProcessor) importCSV(r io.Reader) (ImportResult, error) {
	reader := csv.NewReader(r)
	header, err := reader.Read()
	if err != nil {
		return ImportResult{}, fmt.Errorf("read header: %w", err)
	}
	col := headerIndex(header)
	var result ImportResult
	row := 1
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			result.Errors = append(result.Errors, ImportError{Row: row, Message: err.Error()})
			row++
			continue
		}
		item, err := parseCSVRow(record, col)
		if err != nil {
			result.Errors = append(result.Errors, ImportError{Row: row, Message: err.Error()})
		} else {
			p.mu.Lock()
			p.items = append(p.items, item)
			p.mu.Unlock()
			result.Imported++
		}
		row++
	}
	return result, nil
}

// Export writes records matching filter to w in CSV format.
func (p *BulkProcessor) Export(f ExportFilter, w io.Writer) error {
	p.mu.RLock()
	items := append([]Item(nil), p.items...)
	p.mu.RUnlock()

	cw := csv.NewWriter(w)
	cw.Write([]string{"sku", "title", "quantity", "price"})
	for _, item := range items {
		if f.SKUPrefix != "" && !strings.HasPrefix(item.SKU, f.SKUPrefix) {
			continue
		}
		cw.Write([]string{
			item.SKU,
			item.Title,
			strconv.Itoa(item.Quantity),
			strconv.Itoa(item.Price),
		})
	}
	cw.Flush()
	return cw.Error()
}

func headerIndex(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[strings.TrimSpace(h)] = i
	}
	return m
}

func parseCSVRow(record []string, col map[string]int) (Item, error) {
	get := func(name string) string {
		i, ok := col[name]
		if !ok || i >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[i])
	}
	sku := get("sku")
	if sku == "" {
		return Item{}, errors.New("missing sku")
	}
	qty, err := strconv.Atoi(get("quantity"))
	if err != nil {
		return Item{}, fmt.Errorf("invalid quantity: %w", err)
	}
	price, err := strconv.Atoi(get("price"))
	if err != nil {
		return Item{}, fmt.Errorf("invalid price: %w", err)
	}
	return Item{SKU: sku, Title: get("title"), Quantity: qty, Price: price}, nil
}
