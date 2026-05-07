package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

const (
	defaultModel       = "minimax-text-01"
	defaultHTTPTimeout = 30 * time.Second
	maxResponseBytes   = 5 * 1024 * 1024
)

var (
	ErrMissingBridgeURL = errors.New("missing minimax bridge url")
	ErrDirectMiniMaxURL = errors.New("direct MiniMax URLs are not allowed")
	ErrLocalBridgeURL   = errors.New("localhost bridge URL requires test mode")
)

type Config struct {
	BridgeURL          string
	Model              string
	AllowTestLocalhost bool
}

type Client struct {
	bridgeURL string
	model     string
	http      HTTPDoer
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func NewClient(cfg Config, doer HTTPDoer) (*Client, error) {
	bridgeURL, err := validateBridgeURL(cfg.BridgeURL, cfg.AllowTestLocalhost)
	if err != nil {
		return nil, err
	}
	if doer == nil {
		doer = &http.Client{Timeout: defaultHTTPTimeout}
	}
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	return &Client{bridgeURL: bridgeURL, model: model, http: doer}, nil
}

func (c *Client) Complete(ctx context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	if len(req.Messages) == 0 {
		return port.AICompletionResponse{}, errors.New("messages must not be empty")
	}
	model := c.model
	if req.Model != "" {
		model = req.Model
	}
	body, err := json.Marshal(chatCompletionRequest{
		Model:       model,
		Messages:    toBridgeMessages(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	})
	if err != nil {
		return port.AICompletionResponse{}, fmt.Errorf("marshal minimax bridge request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatCompletionsURL(), bytes.NewReader(body))
	if err != nil {
		return port.AICompletionResponse{}, fmt.Errorf("build minimax bridge request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return port.AICompletionResponse{}, fmt.Errorf("call minimax bridge: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return port.AICompletionResponse{}, fmt.Errorf("read minimax bridge response: %w", err)
	}
	if len(respBody) > maxResponseBytes {
		return port.AICompletionResponse{}, fmt.Errorf("minimax bridge response exceeds %d bytes", maxResponseBytes)
	}
	if resp.StatusCode >= 400 {
		return port.AICompletionResponse{}, fmt.Errorf("minimax bridge status %d: %s", resp.StatusCode, string(respBody))
	}

	var out chatCompletionResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return port.AICompletionResponse{}, fmt.Errorf("decode minimax bridge response: %w", err)
	}
	if len(out.Choices) == 0 {
		return port.AICompletionResponse{}, errors.New("minimax bridge returned no choices")
	}
	return port.AICompletionResponse{
		Content:    out.Choices[0].Message.Content,
		TokensUsed: out.Usage.TotalTokens,
	}, nil
}

func (c *Client) chatCompletionsURL() string {
	if strings.HasSuffix(c.bridgeURL, "/v1") {
		return c.bridgeURL + "/chat/completions"
	}
	return c.bridgeURL + "/v1/chat/completions"
}

func validateBridgeURL(raw string, allowTestLocalhost bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrMissingBridgeURL
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse bridge url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported bridge url scheme %q", parsed.Scheme)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", ErrMissingBridgeURL
	}
	if strings.HasSuffix(host, "minimaxi.com") {
		return "", ErrDirectMiniMaxURL
	}
	if isLocalHost(host) && !allowTestLocalhost {
		return "", ErrLocalBridgeURL
	}
	return strings.TrimRight(raw, "/"), nil
}

func isLocalHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

type chatCompletionRequest struct {
	Model       string          `json:"model"`
	Messages    []bridgeMessage `json:"messages"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
}

type bridgeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func toBridgeMessages(messages []port.AIMessage) []bridgeMessage {
	out := make([]bridgeMessage, len(messages))
	for i, msg := range messages {
		out[i] = bridgeMessage{Role: msg.Role, Content: msg.Content}
	}
	return out
}

type chatCompletionResponse struct {
	Choices []struct {
		Message bridgeMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}
