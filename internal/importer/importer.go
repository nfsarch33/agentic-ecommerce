package importer

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
)

const defaultBatchSize = 100

var columnAliases = map[string]string{
	"title":         "name",
	"name":          "name",
	"price":         "regular_price",
	"regular_price": "regular_price",
	"sku":           "sku",
	"description":   "description",
	"type":          "type",
	"status":        "status",
}

type WooCommerceBatchClient interface {
	BatchCreateProducts(context.Context, []woocommerce.Product) (*woocommerce.BatchResult, error)
}

type ImportRequest struct {
	Source     string
	SourceType string
	Products   []woocommerce.Product
	BatchSize  int
	DryRun     bool
}

type ImportResult struct {
	Success       bool     `json:"success"`
	ImportedCount int      `json:"imported_count"`
	FailedCount   int      `json:"failed_count"`
	Errors        []string `json:"errors,omitempty"`
	CreatedIDs    []int    `json:"created_ids,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type Importer struct {
	client WooCommerceBatchClient
	logger *slog.Logger
}

func New(client WooCommerceBatchClient, logger *slog.Logger) *Importer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Importer{client: client, logger: logger}
}

func (i *Importer) Run(ctx context.Context, req ImportRequest) ImportResult {
	if req.BatchSize <= 0 {
		req.BatchSize = defaultBatchSize
	}

	products, err := i.resolveProducts(req)
	if err != nil {
		return ImportResult{Success: false, Error: err.Error()}
	}
	if len(products) == 0 {
		return ImportResult{Success: false, Error: "no products to import"}
	}
	if req.DryRun {
		i.logger.Info("dry-run: parsed products", "count", len(products))
		return ImportResult{Success: true, ImportedCount: len(products)}
	}
	return i.executeBatches(ctx, products, req.BatchSize)
}

func (i *Importer) resolveProducts(req ImportRequest) ([]woocommerce.Product, error) {
	if len(req.Products) > 0 {
		return req.Products, nil
	}
	if req.Source == "" {
		return nil, fmt.Errorf("no source provided: set Source path or Products slice")
	}
	data, err := os.ReadFile(req.Source)
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(req.SourceType)) {
	case "csv":
		return parseCSV(data)
	case "json":
		return parseJSON(data)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", req.SourceType)
	}
}

func parseCSV(data []byte) ([]woocommerce.Product, error) {
	reader := csv.NewReader(strings.NewReader(string(data)))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("CSV has no data rows")
	}

	fieldMap := make(map[int]string)
	for idx, header := range records[0] {
		if mapped, ok := columnAliases[strings.ToLower(strings.TrimSpace(header))]; ok {
			fieldMap[idx] = mapped
		}
	}

	products := make([]woocommerce.Product, 0, len(records)-1)
	for _, row := range records[1:] {
		var product woocommerce.Product
		for idx, val := range row {
			field, ok := fieldMap[idx]
			if !ok {
				continue
			}
			val = strings.TrimSpace(val)
			switch field {
			case "name":
				product.Name = val
			case "regular_price":
				product.Regular = val
			case "sku":
				product.SKU = val
			case "description":
				product.Description = val
			case "type":
				product.Type = val
			case "status":
				product.Status = val
			}
		}
		products = append(products, product)
	}
	return products, nil
}

func parseJSON(data []byte) ([]woocommerce.Product, error) {
	var products []woocommerce.Product
	if err := json.Unmarshal(data, &products); err == nil {
		return products, nil
	}
	var wrapped struct {
		Products []woocommerce.Product `json:"products"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return wrapped.Products, nil
}

func (i *Importer) executeBatches(ctx context.Context, products []woocommerce.Product, batchSize int) ImportResult {
	result := ImportResult{Success: true}
	for offset := 0; offset < len(products); offset += batchSize {
		end := offset + batchSize
		if end > len(products) {
			end = len(products)
		}
		chunk := products[offset:end]
		batchResult, err := i.client.BatchCreateProducts(ctx, chunk)
		if err != nil {
			result.FailedCount += len(chunk)
			result.Errors = append(result.Errors, fmt.Sprintf("batch at offset %d: %s", offset, err))
			continue
		}
		result.ImportedCount += len(batchResult.Create)
		for _, product := range batchResult.Create {
			result.CreatedIDs = append(result.CreatedIDs, product.ID)
		}
	}
	return result
}
