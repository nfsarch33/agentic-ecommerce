package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync"
)

const (
	defaultAPIVersion        = "2026-04"
	defaultCustomIDNamespace = "agentic_ec"
	defaultCustomIDKey       = "entity_id"
)

var (
	ErrInvalidConfig      = errors.New("shopify: invalid config")
	ErrUnsupportedEvent   = errors.New("shopify: unsupported marketplace event")
	ErrInvalidProductData = errors.New("shopify: invalid product data")
)

type Config struct {
	BaseURL           string
	StoreName         string
	AccessToken       string
	APIVersion        string
	CustomIDNamespace string
	CustomIDKey       string
}

type Client struct {
	baseURL           string
	accessToken       string
	apiVersion        string
	customIDNamespace string
	customIDKey       string
	httpClient        *http.Client
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data struct {
		ProductSet struct {
			Product *struct {
				ID     string `json:"id"`
				Handle string `json:"handle"`
			} `json:"product"`
			UserErrors []userError `json:"userErrors"`
		} `json:"productSet"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type userError struct {
	Field   []string `json:"field"`
	Message string   `json:"message"`
}

var _ marketplacesync.Connector = (*Client)(nil)

func NewClient(cfg Config, httpClient *http.Client) (*Client, error) {
	if strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, fmt.Errorf("%w: access token required", ErrInvalidConfig)
	}
	baseURL, err := baseURL(cfg)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	apiVersion := strings.TrimSpace(cfg.APIVersion)
	if apiVersion == "" {
		apiVersion = defaultAPIVersion
	}
	namespace := strings.TrimSpace(cfg.CustomIDNamespace)
	if namespace == "" {
		namespace = defaultCustomIDNamespace
	}
	key := strings.TrimSpace(cfg.CustomIDKey)
	if key == "" {
		key = defaultCustomIDKey
	}
	return &Client{
		baseURL:           baseURL,
		accessToken:       strings.TrimSpace(cfg.AccessToken),
		apiVersion:        apiVersion,
		customIDNamespace: namespace,
		customIDKey:       key,
		httpClient:        httpClient,
	}, nil
}

func (c *Client) Apply(ctx context.Context, event marketplacesync.ProductEvent) (marketplacesync.ApplyResult, error) {
	if event.Provider != "shopify" {
		return marketplacesync.ApplyResult{}, fmt.Errorf("%w: provider %q", ErrUnsupportedEvent, event.Provider)
	}
	if event.EntityType != marketplacesync.EntityProduct {
		return marketplacesync.ApplyResult{}, fmt.Errorf("%w: entity type %q", ErrUnsupportedEvent, event.EntityType)
	}
	if event.Operation != marketplacesync.OperationUpsert {
		return marketplacesync.ApplyResult{}, fmt.Errorf("%w: unsupported operation %q", ErrUnsupportedEvent, event.Operation)
	}

	input, err := c.productInput(event)
	if err != nil {
		return marketplacesync.ApplyResult{}, err
	}
	request := graphQLRequest{
		Query: productSetMutation,
		Variables: map[string]any{
			"input":       input,
			"identifier":  c.identifier(event),
			"synchronous": true,
		},
	}
	var response graphQLResponse
	if err := c.doGraphQL(ctx, request, &response); err != nil {
		return marketplacesync.ApplyResult{}, err
	}
	if len(response.Errors) > 0 {
		return marketplacesync.ApplyResult{}, fmt.Errorf("shopify graphql error: %s", joinGraphQLErrors(response.Errors))
	}
	if len(response.Data.ProductSet.UserErrors) > 0 {
		return marketplacesync.ApplyResult{}, fmt.Errorf("shopify user error: %s", joinUserErrors(response.Data.ProductSet.UserErrors))
	}
	if response.Data.ProductSet.Product == nil || strings.TrimSpace(response.Data.ProductSet.Product.ID) == "" {
		return marketplacesync.ApplyResult{}, errors.New("shopify productSet returned no product id")
	}
	return marketplacesync.ApplyResult{
		RemoteID: response.Data.ProductSet.Product.ID,
		Version:  event.Version,
	}, nil
}

func (c *Client) doGraphQL(ctx context.Context, request graphQLRequest, dest any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal shopify graphql request: %w", err)
	}
	endpoint, err := c.endpoint()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build shopify graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Shopify-Access-Token", c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send shopify graphql request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("shopify graphql status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode shopify graphql response: %w", err)
	}
	return nil
}

func (c *Client) endpoint() (string, error) {
	endpoint, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("%w: base url: %w", ErrInvalidConfig, err)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/admin/api/" + c.apiVersion + "/graphql.json"
	return endpoint.String(), nil
}

func (c *Client) identifier(event marketplacesync.ProductEvent) map[string]any {
	if strings.HasPrefix(event.ExternalID, "gid://shopify/Product/") {
		return map[string]any{"id": event.ExternalID}
	}
	return map[string]any{
		"customId": map[string]any{
			"namespace": c.customIDNamespace,
			"key":       c.customIDKey,
			"value":     event.EntityID,
		},
	}
}

func (c *Client) productInput(event marketplacesync.ProductEvent) (map[string]any, error) {
	title := payloadString(event.Payload, "title")
	if title == "" {
		return nil, fmt.Errorf("%w: title required", ErrInvalidProductData)
	}
	input := map[string]any{"title": title}
	addString(input, "handle", payloadString(event.Payload, "handle"))
	addString(input, "descriptionHtml", payloadString(event.Payload, "description_html", "descriptionHtml"))
	addString(input, "productType", payloadString(event.Payload, "product_type", "productType"))
	addString(input, "vendor", payloadString(event.Payload, "vendor"))
	addString(input, "status", payloadString(event.Payload, "status"))
	if tags := payloadTags(event.Payload["tags"]); len(tags) > 0 {
		input["tags"] = tags
	}
	return input, nil
}

func baseURL(cfg Config) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		store := strings.TrimSpace(cfg.StoreName)
		if store == "" {
			return "", fmt.Errorf("%w: base url or store name required", ErrInvalidConfig)
		}
		store = strings.TrimSuffix(strings.TrimPrefix(store, "https://"), "/")
		if !strings.Contains(store, ".") {
			store += ".myshopify.com"
		}
		base = "https://" + store
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("%w: base url: %w", ErrInvalidConfig, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: base url must include scheme and host", ErrInvalidConfig)
	}
	return base, nil
}

func payloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func payloadTags(value any) []string {
	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, tag := range v {
			if trimmed := strings.TrimSpace(tag); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, tag := range v {
			if s, ok := tag.(string); ok {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					out = append(out, trimmed)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func addString(target map[string]any, key, value string) {
	if value != "" {
		target[key] = value
	}
}

func joinGraphQLErrors(errors []graphQLError) string {
	messages := make([]string, 0, len(errors))
	for _, err := range errors {
		if strings.TrimSpace(err.Message) != "" {
			messages = append(messages, strings.TrimSpace(err.Message))
		}
	}
	return strings.Join(messages, "; ")
}

func joinUserErrors(errors []userError) string {
	messages := make([]string, 0, len(errors))
	for _, err := range errors {
		message := strings.TrimSpace(err.Message)
		if message == "" {
			continue
		}
		if len(err.Field) > 0 {
			message = strings.Join(err.Field, ".") + ": " + message
		}
		messages = append(messages, message)
	}
	return strings.Join(messages, "; ")
}

const productSetMutation = `
mutation ECProductSet($input: ProductSetInput!, $identifier: ProductSetIdentifiers, $synchronous: Boolean!) {
  productSet(input: $input, identifier: $identifier, synchronous: $synchronous) {
    product {
      id
      handle
    }
    userErrors {
      field
      message
    }
  }
}`
