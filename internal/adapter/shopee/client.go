package shopee

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/marketplacesync"
)

const (
	defaultHTTPTimeout = 15 * time.Second
	addItemPath        = "/api/v2/product/add_item"
	updateItemPath     = "/api/v2/product/update_item"
)

var (
	ErrUnsupportedEvent   = errors.New("shopee: unsupported marketplace event")
	ErrInvalidProductData = errors.New("shopee: invalid product data")
	ErrLiveCallsDisabled  = errors.New("shopee: live calls disabled")
)

type Config struct {
	BaseURL          string
	PartnerID        int64
	PartnerKey       string
	AccessToken      string
	ShopID           int64
	Now              func() int64
	AllowLiveBaseURL bool
}

type Client struct {
	baseURL     string
	partnerID   int64
	partnerKey  []byte
	accessToken string
	shopID      int64
	now         func() int64
	httpClient  *http.Client
}

type productPayload struct {
	ItemID        *int64  `json:"item_id,omitempty"`
	ItemName      string  `json:"item_name"`
	Description   string  `json:"description"`
	ItemSKU       string  `json:"item_sku,omitempty"`
	ExternalSKU   string  `json:"external_sku,omitempty"`
	OriginalPrice float64 `json:"original_price,omitempty"`
	NormalStock   int     `json:"normal_stock,omitempty"`
}

type productResponse struct {
	Error    string `json:"error"`
	Message  string `json:"message"`
	Response struct {
		ItemID int64 `json:"item_id"`
	} `json:"response"`
}

var _ marketplacesync.Connector = (*Client)(nil)

func NewClient(cfg Config, httpClient *http.Client) (*Client, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	return &Client{
		baseURL:     strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		partnerID:   cfg.PartnerID,
		partnerKey:  []byte(cfg.PartnerKey),
		accessToken: strings.TrimSpace(cfg.AccessToken),
		shopID:      cfg.ShopID,
		now:         defaultClock(cfg.Now),
		httpClient:  boundedHTTPClient(httpClient),
	}, nil
}

func (c *Client) Apply(ctx context.Context, event marketplacesync.ProductEvent) (marketplacesync.ApplyResult, error) {
	if err := validateEvent(event); err != nil {
		return marketplacesync.ApplyResult{}, err
	}
	payload, err := productInput(event)
	if err != nil {
		return marketplacesync.ApplyResult{}, err
	}
	response, err := c.doProductRequest(ctx, endpointPath(payload), payload)
	if err != nil {
		return marketplacesync.ApplyResult{}, err
	}
	return resultFromResponse(event, response)
}

func validateConfig(cfg Config) error {
	if err := validateBaseURLPolicy(cfg); err != nil {
		return err
	}
	switch {
	case strings.TrimSpace(cfg.BaseURL) == "":
		return fmt.Errorf("%w: base url required", ErrInvalidConfig)
	case cfg.PartnerID <= 0:
		return fmt.Errorf("%w: partner id required", ErrInvalidConfig)
	case len(strings.TrimSpace(cfg.PartnerKey)) < minPartnerKeyBytes:
		return fmt.Errorf("%w: partner key too short", ErrInvalidConfig)
	case strings.TrimSpace(cfg.AccessToken) == "":
		return fmt.Errorf("%w: access token required", ErrInvalidConfig)
	case cfg.ShopID <= 0:
		return fmt.Errorf("%w: shop id required", ErrInvalidConfig)
	default:
		return nil
	}
}

func validateBaseURLPolicy(cfg Config) error {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("%w: base url: %w", ErrInvalidConfig, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: base url must include scheme and host", ErrInvalidConfig)
	}
	if isOfficialShopeeHost(parsed.Hostname()) && !cfg.AllowLiveBaseURL {
		return fmt.Errorf("%w: %s", ErrLiveCallsDisabled, parsed.Hostname())
	}
	return nil
}

func isOfficialShopeeHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return hasDomain(host, "shopee.com") || hasDomain(host, "shopeemobile.com")
}

func hasDomain(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func boundedHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

func defaultClock(now func() int64) func() int64 {
	if now != nil {
		return now
	}
	return func() int64 { return time.Now().Unix() }
}

func validateEvent(event marketplacesync.ProductEvent) error {
	if event.Provider != "shopee" {
		return fmt.Errorf("%w: provider %q", ErrUnsupportedEvent, event.Provider)
	}
	if event.EntityType != marketplacesync.EntityProduct {
		return fmt.Errorf("%w: entity type %q", ErrUnsupportedEvent, event.EntityType)
	}
	if event.Operation != marketplacesync.OperationUpsert {
		return fmt.Errorf("%w: unsupported operation %q", ErrUnsupportedEvent, event.Operation)
	}
	return nil
}

func productInput(event marketplacesync.ProductEvent) (productPayload, error) {
	payload := productPayload{
		ItemName:      payloadString(event.Payload, "title", "item_name"),
		Description:   payloadString(event.Payload, "description", "description_html"),
		ItemSKU:       payloadString(event.Payload, "sku", "item_sku"),
		ExternalSKU:   event.EntityID,
		OriginalPrice: payloadFloat(event.Payload, "price", "original_price"),
		NormalStock:   payloadInt(event.Payload, "stock", "normal_stock"),
	}
	if payload.ItemName == "" {
		return productPayload{}, fmt.Errorf("%w: title required", ErrInvalidProductData)
	}
	if payload.Description == "" {
		return productPayload{}, fmt.Errorf("%w: description required", ErrInvalidProductData)
	}
	if itemID, ok := parseItemID(event.ExternalID); ok {
		payload.ItemID = &itemID
	}
	return payload, nil
}

func parseItemID(value string) (int64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(trimmed, 10, 64)
	return id, err == nil && id > 0
}

func endpointPath(payload productPayload) string {
	if payload.ItemID != nil {
		return updateItemPath
	}
	return addItemPath
}

func (c *Client) doProductRequest(ctx context.Context, path string, payload productPayload) (productResponse, error) {
	req, err := c.newProductRequest(ctx, path, payload)
	if err != nil {
		return productResponse{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return productResponse{}, fmt.Errorf("send shopee product request: %w", err)
	}
	return decodeProductResponse(resp)
}

func (c *Client) newProductRequest(ctx context.Context, path string, payload productPayload) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal shopee product request: %w", err)
	}
	endpoint, err := c.signedEndpoint(path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build shopee product request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *Client) signedEndpoint(path string) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("%w: base url: %w", ErrInvalidConfig, err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	timestamp := c.now()
	signature, err := c.signature(path, timestamp)
	if err != nil {
		return "", err
	}
	query := base.Query()
	query.Set("partner_id", strconv.FormatInt(c.partnerID, 10))
	query.Set("timestamp", strconv.FormatInt(timestamp, 10))
	query.Set("access_token", c.accessToken)
	query.Set("shop_id", strconv.FormatInt(c.shopID, 10))
	query.Set("sign", signature)
	base.RawQuery = query.Encode()
	return base.String(), nil
}

func (c *Client) signature(path string, timestamp int64) (string, error) {
	return ComputeSignature(SignRequest{
		PartnerKey:  c.partnerKey,
		PartnerID:   c.partnerID,
		Path:        path,
		Timestamp:   timestamp,
		AccessToken: c.accessToken,
		ShopID:      c.shopID,
	})
}

func decodeProductResponse(resp *http.Response) (productResponse, error) {
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return productResponse{}, fmt.Errorf("shopee product status %d", resp.StatusCode)
	}
	var out productResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return productResponse{}, fmt.Errorf("decode shopee product response: %w", err)
	}
	return out, nil
}

func resultFromResponse(event marketplacesync.ProductEvent, response productResponse) (marketplacesync.ApplyResult, error) {
	if strings.TrimSpace(response.Error) != "" {
		return marketplacesync.ApplyResult{}, fmt.Errorf("shopee api error %s: %s", response.Error, response.Message)
	}
	if response.Response.ItemID <= 0 {
		return marketplacesync.ApplyResult{}, errors.New("shopee product response returned no item id")
	}
	return marketplacesync.ApplyResult{
		RemoteID: strconv.FormatInt(response.Response.ItemID, 10),
		Version:  event.Version,
	}, nil
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

func payloadFloat(payload map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := numberAsFloat(payload[key]); ok {
			return value
		}
	}
	return 0
}

func payloadInt(payload map[string]any, keys ...string) int {
	for _, key := range keys {
		if value, ok := numberAsInt(payload[key]); ok {
			return value
		}
	}
	return 0
}

func numberAsFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

func numberAsInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	default:
		return 0, false
	}
}
