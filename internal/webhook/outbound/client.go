package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type ClientConfig struct {
	HTTPClient  HTTPDoer
	MaxAttempts int
	Timeout     time.Duration
	Backoff     func(attempt int) time.Duration
	Now         func() time.Time
}

type Client struct {
	httpClient  HTTPDoer
	maxAttempts int
	timeout     time.Duration
	backoff     func(attempt int) time.Duration
	now         func() time.Time
}

type DeliveryRequest struct {
	Registration Registration
	Secret       string
	Event        eventbus.Event
}

func NewClient(cfg ClientConfig) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	backoff := cfg.Backoff
	if backoff == nil {
		backoff = func(attempt int) time.Duration {
			return time.Duration(attempt) * 100 * time.Millisecond
		}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		httpClient:  httpClient,
		maxAttempts: maxAttempts,
		timeout:     timeout,
		backoff:     backoff,
		now:         now,
	}
}

func (c *Client) Deliver(ctx context.Context, req DeliveryRequest) DeliveryResult {
	event := req.Event
	result := DeliveryResult{
		WebhookID: req.Registration.ID,
		EventID:   event.ID,
		EventType: event.Type,
	}
	body, err := json.Marshal(EventPayload{
		ID:        event.ID,
		Type:      event.Type,
		TenantID:  event.TenantID,
		Payload:   event.Payload,
		Timestamp: event.Timestamp,
		Source:    event.Source,
	})
	if err != nil {
		result.Attempts = 0
		result.Error = "payload_marshal_failed"
		return result
	}

	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		result.Attempts = attempt
		status, deliveryErr := c.deliverOnce(ctx, req, body)
		result.Status = status
		if deliveryErr == nil && status >= 200 && status < 300 {
			result.Success = true
			result.Error = ""
			return result
		}
		result.Error = deliveryErrorCode(status, deliveryErr)
		if !shouldRetry(status, deliveryErr) || attempt == c.maxAttempts {
			return result
		}
		if delay := c.backoff(attempt); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				result.Error = "context_cancelled"
				return result
			case <-timer.C:
			}
		}
	}
	return result
}

func (c *Client) deliverOnce(ctx context.Context, req DeliveryRequest, body []byte) (int, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, req.Registration.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "agentic-ecommerce-webhook-bridge/1.5")
	for key, values := range NewSigner(req.Secret).Sign(req.Registration.ID, c.now().UTC(), body) {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func shouldRetry(status int, err error) bool {
	if err != nil {
		return true
	}
	return status == http.StatusTooManyRequests || status >= 500
}

func deliveryErrorCode(status int, err error) string {
	if err != nil {
		return "request_failed"
	}
	if status == 0 {
		return "unknown_status"
	}
	return fmt.Sprintf("http_%d", status)
}
