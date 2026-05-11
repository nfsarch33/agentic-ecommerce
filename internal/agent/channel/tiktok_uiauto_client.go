// File scope: v3.3.0 EC-3-5 TikTok organic-posting facade.
//
// EC-3-5 in the external roadmap targets the uiauto-framework repo
// (`pkg/social/tiktok_poster.go` ships the chromedp poster) but
// this v3.3.0 sprint is scoped to agentic-ecommerce. The split per
// the plan: ship the *client-side facade* here that calls the
// omniparser-bridge HTTP endpoint via a signed POST; the actual
// chromedp poster ships from uiauto-framework in a v3.7.0 EC-10
// follow-up PR.
//
// The facade signs every request with the EC-3-1 HMAC primitive so
// the omniparser-bridge can reject forged calls. The bridge URL is
// supplied as a runx alias resolved at composition root time:
// the agent NEVER sees the literal IP / hostname; only the alias.
// Cite skill: rules-no-shell-leak.
package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/social"
)

// MaxOmniParserBridgeBodyBytes is the upper bound for an omniparser
// bridge response. The bridge ships small JSON envelopes; 1 MiB is
// generous and prevents an OOM on a misbehaving bridge.
const MaxOmniParserBridgeBodyBytes = 1 << 20

// DefaultOmniParserBridgeTimeout is the per-request HTTP timeout.
const DefaultOmniParserBridgeTimeout = 30 * time.Second

// EC-3-5 sentinels.
var (
	// ErrUIAutoUnconfigured is returned when a required dependency
	// is missing.
	ErrUIAutoUnconfigured = errors.New("uiauto: client unconfigured")

	// ErrUIAutoBridgeRejected is returned when the omniparser bridge
	// returned a non-2xx status. The body (truncated) is included.
	ErrUIAutoBridgeRejected = errors.New("uiauto: bridge rejected request")

	// ErrUIAutoSignatureBuild is returned when the HMAC signature
	// could not be built (e.g. secret too short).
	ErrUIAutoSignatureBuild = errors.New("uiauto: signature build failed")

	// ErrUIAutoClosed is returned by Post after Close.
	ErrUIAutoClosed = errors.New("uiauto: client closed")
)

// TikTokOrganicPost is the EC-3-5 input. The bridge translates this
// into chromedp commands.
type TikTokOrganicPost struct {
	TenantID    string
	ProductID   string
	VideoURL    string
	Caption     string
	Hashtags    []string
	ScheduleAt  time.Time
	SessionRef  string // operator-bootstrapped session token alias
	IsLifestyle bool
}

// TikTokOrganicResult is the EC-3-5 output from the bridge.
type TikTokOrganicResult struct {
	PostID     string
	Status     string
	OccurredAt time.Time
}

// TikTokUIAutoClientConfig wires the EC-3-5 facade.
type TikTokUIAutoClientConfig struct {
	HTTPClient   *http.Client
	BridgeURL    string // runx alias resolved at composition root; never an IP/hostname literal
	BridgeSecret []byte
	TenantID     string
	UserAgent    string
	Now          func() time.Time
	Metrics      TikTokListingMetrics
}

// TikTokUIAutoClient calls the omniparser-bridge to drive a chromedp
// poster on the gpu-host-1 side. It is the client-side facade; the real
// chromedp poster ships in uiauto-framework.
type TikTokUIAutoClient struct {
	cfg    TikTokUIAutoClientConfig
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewTikTokUIAutoClient constructs a client.
func NewTikTokUIAutoClient(logger *slog.Logger, cfg TikTokUIAutoClientConfig) (*TikTokUIAutoClient, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.BridgeURL) == "" {
		return nil, fmt.Errorf("%w: BridgeURL required (runx alias for omniparser-bridge gpu-host-1)", ErrUIAutoUnconfigured)
	}
	if len(cfg.BridgeSecret) < social.MinTikTokSecretBytes {
		return nil, fmt.Errorf("%w: BridgeSecret < %d bytes", ErrUIAutoUnconfigured, social.MinTikTokSecretBytes)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrUIAutoUnconfigured)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultOmniParserBridgeTimeout}
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (compatible; agentic-ecommerce/3.3.0; tiktok-uiauto)"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &TikTokUIAutoClient{cfg: cfg, logger: logger}, nil
}

// Close marks the client closed.
func (c *TikTokUIAutoClient) Close(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// Post sends the organic post to the omniparser bridge for chromedp
// dispatch. Returns the bridge result on 2xx.
func (c *TikTokUIAutoClient) Post(ctx context.Context, req TikTokOrganicPost) (TikTokOrganicResult, error) {
	if err := c.guard(); err != nil {
		return TikTokOrganicResult{}, err
	}
	body, err := json.Marshal(map[string]any{
		"tenant_id":    requireString(req.TenantID, c.cfg.TenantID),
		"product_id":   req.ProductID,
		"video_url":    req.VideoURL,
		"caption":      req.Caption,
		"hashtags":     req.Hashtags,
		"schedule_at":  req.ScheduleAt.UTC().Format(time.RFC3339Nano),
		"session_ref":  req.SessionRef,
		"is_lifestyle": req.IsLifestyle,
	})
	if err != nil {
		c.recordOutcome("encode_failed")
		return TikTokOrganicResult{}, fmt.Errorf("uiauto: encode body: %w", err)
	}
	httpReq, err := c.buildSignedRequest(ctx, body)
	if err != nil {
		c.recordOutcome("sign_failed")
		return TikTokOrganicResult{}, err
	}
	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		c.recordOutcome("transport_error")
		return TikTokOrganicResult{}, fmt.Errorf("uiauto: bridge call: %w", err)
	}
	defer resp.Body.Close()
	return c.parseResponse(resp)
}

func (c *TikTokUIAutoClient) buildSignedRequest(ctx context.Context, body []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BridgeURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("uiauto: build request: %w", err)
	}
	timestamp := c.cfg.Now().Unix()
	signature, err := social.ComputeTikTokSignature(social.TikTokSignRequest{
		Secret:    c.cfg.BridgeSecret,
		Timestamp: timestamp,
		Path:      "/uiauto/tiktok/post",
		Body:      body,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUIAutoSignatureBuild, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	httpReq.Header.Set("X-Bridge-Timestamp", fmt.Sprintf("%d", timestamp))
	httpReq.Header.Set("X-Bridge-Sign", signature)
	httpReq.Header.Set("X-Bridge-Tenant", c.cfg.TenantID)
	return httpReq, nil
}

func (c *TikTokUIAutoClient) parseResponse(resp *http.Response) (TikTokOrganicResult, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxOmniParserBridgeBodyBytes))
	if err != nil {
		c.recordOutcome("read_failed")
		return TikTokOrganicResult{}, fmt.Errorf("uiauto: read bridge body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.recordOutcome("bridge_rejected")
		return TikTokOrganicResult{}, fmt.Errorf("%w: status=%d body=%s", ErrUIAutoBridgeRejected, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var wire bridgePostResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		c.recordOutcome("decode_failed")
		return TikTokOrganicResult{}, fmt.Errorf("uiauto: decode bridge body: %w", err)
	}
	occurred, _ := time.Parse(time.RFC3339, wire.OccurredAt)
	if occurred.IsZero() {
		occurred = c.cfg.Now().UTC()
	}
	c.recordOutcome("ok")
	return TikTokOrganicResult{PostID: wire.PostID, Status: wire.Status, OccurredAt: occurred.UTC()}, nil
}

func (c *TikTokUIAutoClient) guard() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrUIAutoClosed
	}
	return nil
}

func (c *TikTokUIAutoClient) recordOutcome(outcome string) {
	if c.cfg.Metrics == nil {
		return
	}
	c.cfg.Metrics.RecordListing(c.cfg.TenantID, "uiauto."+outcome)
}

func requireString(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

// bridgePostResponse mirrors the omniparser-bridge shop wire shape.
type bridgePostResponse struct {
	PostID     string `json:"post_id"`
	Status     string `json:"status"`
	OccurredAt string `json:"occurred_at"`
}
