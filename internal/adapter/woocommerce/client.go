package woocommerce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

type Config struct {
	BaseURL        string
	ConsumerKey    string
	ConsumerSecret string
}

type Client struct {
	baseURL        string
	consumerKey    string
	consumerSecret string
	httpClient     *http.Client
}

func NewClient(config Config, httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return Client{
		baseURL:        strings.TrimRight(config.BaseURL, "/"),
		consumerKey:    config.ConsumerKey,
		consumerSecret: config.ConsumerSecret,
		httpClient:     httpClient,
	}
}

func (c Client) UpsertProduct(ctx context.Context, product catalog.Product) error {
	payload := map[string]any{
		"sku":               product.SKU(),
		"name":              product.Title(),
		"short_description": product.Description(),
		"regular_price":     fmt.Sprintf("%.2f", float64(product.Price().Amount())/100),
		"stock_quantity":    product.Stock(),
		"status":            wcStatus(product.Status()),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal product %s: %w", product.SKU(), err)
	}

	endpoint, err := c.endpoint("/products", nil)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build product request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send product %s: %w", product.SKU(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("woocommerce product %s: status %d", product.SKU(), resp.StatusCode)
	}

	return nil
}

func (c Client) ListProducts(ctx context.Context, opts ListOptions) ([]Product, error) {
	values := url.Values{}
	addListOptions(values, opts)
	endpoint, err := c.endpoint("/products", values)
	if err != nil {
		return nil, err
	}
	var products []Product
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &products); err != nil {
		return nil, err
	}
	return products, nil
}

func (c Client) BatchCreateProducts(ctx context.Context, products []Product) (*BatchResult, error) {
	endpoint, err := c.endpoint("/products/batch", nil)
	if err != nil {
		return nil, err
	}
	body := map[string][]Product{"create": products}
	var result BatchResult
	if err := c.doJSON(ctx, http.MethodPost, endpoint, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c Client) ListOrders(ctx context.Context, opts ListOptions) ([]Order, error) {
	values := url.Values{}
	addListOptions(values, opts)
	endpoint, err := c.endpoint("/orders", values)
	if err != nil {
		return nil, err
	}
	var orders []Order
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &orders); err != nil {
		return nil, err
	}
	return orders, nil
}

func (c Client) doJSON(ctx context.Context, method string, endpoint *url.URL, payload any, dest any) error {
	var body *bytes.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal woocommerce request: %w", err)
		}
		body = bytes.NewReader(data)
	} else {
		body = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return fmt.Errorf("build woocommerce request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send woocommerce request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("woocommerce status %d", resp.StatusCode)
	}
	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return fmt.Errorf("decode woocommerce response: %w", err)
		}
	}
	return nil
}

func (c Client) endpoint(path string, values url.Values) (*url.URL, error) {
	endpoint, err := url.Parse(c.baseURL + "/wp-json/wc/v3" + path)
	if err != nil {
		return nil, fmt.Errorf("woocommerce endpoint: %w", err)
	}
	query := endpoint.Query()
	for key, vals := range values {
		for _, value := range vals {
			query.Add(key, value)
		}
	}
	if c.consumerKey != "" {
		query.Set("consumer_key", c.consumerKey)
	}
	if c.consumerSecret != "" {
		query.Set("consumer_secret", c.consumerSecret)
	}
	endpoint.RawQuery = query.Encode()
	return endpoint, nil
}

func addListOptions(values url.Values, opts ListOptions) {
	if opts.PerPage > 0 {
		values.Set("per_page", strconv.Itoa(opts.PerPage))
	}
	if opts.Page > 0 {
		values.Set("page", strconv.Itoa(opts.Page))
	}
	if opts.Status != "" {
		values.Set("status", opts.Status)
	}
	if opts.After != "" {
		values.Set("after", opts.After)
	}
	if opts.SKU != "" {
		values.Set("sku", opts.SKU)
	}
}

func wcStatus(s catalog.ProductStatus) string {
	switch s {
	case catalog.StatusActive:
		return "publish"
	case catalog.StatusArchived:
		return "private"
	default:
		return "draft"
	}
}
