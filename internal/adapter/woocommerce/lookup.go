package woocommerce

import (
	"context"

	"fmt"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"net/http"
	"net/url"
	"strconv"
)

// FindProductBySKU returns the store's live product for a SKU, or nil when
// the SKU does not exist. It is the GET-before-write half of the exactly-once
// publish gate: SKU is unique per store, so this is the remote lookup that
// decides update-by-id versus create.
func (c Client) FindProductBySKU(ctx context.Context, sku string) (*Product, error) {
	values := url.Values{}
	values.Set("sku", sku)
	endpoint, err := c.endpoint("/products", values)
	if err != nil {
		return nil, err
	}
	var products []Product
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &products); err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return nil, nil
	}
	return &products[0], nil
}

// UpdateProduct writes the fields of payload onto the existing product with
// the given id and returns the stored product as the store now sees it.
func (c Client) UpdateProduct(ctx context.Context, remoteID string, fields map[string]string) (*Product, error) {
	id, err := strconv.Atoi(remoteID)
	if err != nil {
		return nil, fmt.Errorf("woocommerce update: bad remote id %q: %w", remoteID, err)
	}
	endpoint, err := c.endpoint("/products/"+strconv.Itoa(id), nil)
	if err != nil {
		return nil, err
	}
	var updated Product
	if err := c.doJSON(ctx, http.MethodPut, endpoint, fields, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

// CreateProduct posts a new product and returns the stored product.
func (c Client) CreateProduct(ctx context.Context, fields map[string]string) (*Product, error) {
	endpoint, err := c.endpoint("/products", nil)
	if err != nil {
		return nil, err
	}
	var created Product
	if err := c.doJSON(ctx, http.MethodPost, endpoint, fields, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// FieldsFor converts a domain product into the store write fields, shared by
// the gate's update and create paths so both write the same shape.
func FieldsFor(sku, title, description, priceCents string, stock int, status string) map[string]string {
	return map[string]string{
		"sku":               sku,
		"name":              title,
		"short_description": description,
		"regular_price":     priceCents,
		"stock_quantity":    strconv.Itoa(stock),
		"status":            status,
	}
}

// WCStatus maps a domain product status onto the store's status vocabulary.
func WCStatus(s catalog.ProductStatus) string { return wcStatus(s) }

// Config returns a copy of the client's connection settings so a caller can
// build another client against the same store.
func (c Client) Config() Config {
	return Config{BaseURL: c.baseURL, ConsumerKey: c.consumerKey, ConsumerSecret: c.consumerSecret}
}
