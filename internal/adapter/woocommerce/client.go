package woocommerce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

	endpoint, err := url.Parse(c.baseURL + "/wp-json/wc/v3/products")
	if err != nil {
		return fmt.Errorf("woocommerce endpoint: %w", err)
	}
	query := endpoint.Query()
	if c.consumerKey != "" {
		query.Set("consumer_key", c.consumerKey)
	}
	if c.consumerSecret != "" {
		query.Set("consumer_secret", c.consumerSecret)
	}
	endpoint.RawQuery = query.Encode()

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
