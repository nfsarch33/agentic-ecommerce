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
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync"
)

const (
	defaultAPIVersion        = "2026-04"
	defaultCustomIDNamespace = "agentic_ec"
	defaultCustomIDKey       = "entity_id"
	defaultHTTPTimeout       = 15 * time.Second
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
	accessToken, err := requiredAccessToken(cfg.AccessToken)
	if err != nil {
		return nil, err
	}
	baseURL, err := baseURL(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL:           baseURL,
		accessToken:       accessToken,
		apiVersion:        defaultString(cfg.APIVersion, defaultAPIVersion),
		customIDNamespace: defaultString(cfg.CustomIDNamespace, defaultCustomIDNamespace),
		customIDKey:       defaultString(cfg.CustomIDKey, defaultCustomIDKey),
		httpClient:        boundedHTTPClient(httpClient),
	}, nil
}

func (c *Client) Apply(ctx context.Context, event marketplacesync.ProductEvent) (marketplacesync.ApplyResult, error) {
	if err := validateEvent(event); err != nil {
		return marketplacesync.ApplyResult{}, err
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
	return resultFromResponse(event, response)
}

func requiredAccessToken(value string) (string, error) {
	token := strings.TrimSpace(value)
	if token == "" {
		return "", fmt.Errorf("%w: access token required", ErrInvalidConfig)
	}
	return token, nil
}

func boundedHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

func defaultString(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func validateEvent(event marketplacesync.ProductEvent) error {
	if event.Provider != "shopify" {
		return fmt.Errorf("%w: provider %q", ErrUnsupportedEvent, event.Provider)
	}
	return validateProductEvent(event)
}

func validateProductEvent(event marketplacesync.ProductEvent) error {
	if event.EntityType != marketplacesync.EntityProduct {
		return fmt.Errorf("%w: entity type %q", ErrUnsupportedEvent, event.EntityType)
	}
	if event.Operation != marketplacesync.OperationUpsert {
		return fmt.Errorf("%w: unsupported operation %q", ErrUnsupportedEvent, event.Operation)
	}
	return nil
}

func resultFromResponse(event marketplacesync.ProductEvent, response graphQLResponse) (marketplacesync.ApplyResult, error) {
	if len(response.Errors) > 0 {
		return marketplacesync.ApplyResult{}, fmt.Errorf("shopify graphql error: %s", joinGraphQLErrors(response.Errors))
	}
	if len(response.Data.ProductSet.UserErrors) > 0 {
		return marketplacesync.ApplyResult{}, fmt.Errorf("shopify user error: %s", joinUserErrors(response.Data.ProductSet.UserErrors))
	}
	return resultFromProduct(event, response.Data.ProductSet.Product)
}

func resultFromProduct(event marketplacesync.ProductEvent, product *struct {
	ID     string `json:"id"`
	Handle string `json:"handle"`
}) (marketplacesync.ApplyResult, error) {
	if product == nil || strings.TrimSpace(product.ID) == "" {
		return marketplacesync.ApplyResult{}, errors.New("shopify productSet returned no product id")
	}
	return marketplacesync.ApplyResult{
		RemoteID: product.ID,
		Version:  event.Version,
	}, nil
}

func (c *Client) doGraphQL(ctx context.Context, request graphQLRequest, dest any) error {
	req, err := c.newGraphQLRequest(ctx, request)
	if err != nil {
		return err
	}
	return c.doGraphQLRequest(req, dest)
}

func (c *Client) newGraphQLRequest(ctx context.Context, request graphQLRequest) (*http.Request, error) {
	body, err := marshalGraphQLRequest(request)
	if err != nil {
		return nil, err
	}
	endpoint, err := c.endpoint()
	if err != nil {
		return nil, err
	}
	return c.newHTTPRequest(ctx, endpoint, body)
}

func marshalGraphQLRequest(request graphQLRequest) ([]byte, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal shopify graphql request: %w", err)
	}
	return body, nil
}

func (c *Client) newHTTPRequest(ctx context.Context, endpoint string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build shopify graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Shopify-Access-Token", c.accessToken)
	return req, nil
}

func (c *Client) doGraphQLRequest(req *http.Request, dest any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send shopify graphql request: %w", err)
	}
	return decodeGraphQLResponse(resp, dest)
}

func decodeGraphQLResponse(resp *http.Response, dest any) error {
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
		return storeBaseURL(cfg.StoreName)
	}
	return validateBaseURL(base)
}

func storeBaseURL(storeName string) (string, error) {
	store := normalizedStoreName(storeName)
	if store == "" {
		return "", fmt.Errorf("%w: base url or store name required", ErrInvalidConfig)
	}
	return validateBaseURL("https://" + store)
}

func normalizedStoreName(storeName string) string {
	store := strings.TrimSpace(storeName)
	store = strings.TrimSuffix(strings.TrimPrefix(store, "https://"), "/")
	if store != "" && !strings.Contains(store, ".") {
		store += ".myshopify.com"
	}
	return store
}

func validateBaseURL(base string) (string, error) {
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
		if s := cleanPayloadString(payload[key]); s != "" {
			return s
		}
	}
	return ""
}

func cleanPayloadString(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func payloadTags(value any) []string {
	switch v := value.(type) {
	case []string:
		return cleanStringTags(v)
	case []any:
		return cleanAnyTags(v)
	default:
		return nil
	}
}

func cleanStringTags(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = appendCleanTag(out, value)
	}
	return out
}

func cleanAnyTags(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		s, ok := value.(string)
		if !ok {
			continue
		}
		out = appendCleanTag(out, s)
	}
	return out
}

func appendCleanTag(out []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return out
	}
	return append(out, trimmed)
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
	return strings.Join(userErrorMessages(errors), "; ")
}

func userErrorMessages(errors []userError) []string {
	messages := make([]string, 0, len(errors))
	for _, err := range errors {
		if message := userErrorMessage(err); message != "" {
			messages = append(messages, message)
		}
	}
	return messages
}

func userErrorMessage(err userError) string {
	message := strings.TrimSpace(err.Message)
	if message == "" {
		return ""
	}
	return withUserErrorField(err.Field, message)
}

func withUserErrorField(field []string, message string) string {
	if len(field) == 0 {
		return message
	}
	return strings.Join(field, ".") + ": " + message
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
