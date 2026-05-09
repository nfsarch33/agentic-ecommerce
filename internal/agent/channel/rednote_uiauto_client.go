// File scope: v3.4.0 EC-4-1 RedNote (Xiaohongshu / XHS) organic-
// posting facade.
//
// RedNote has NO public API for third-party posting. The agent
// must drive a real browser session (cookie-based login, OmniParser-
// assisted dynamic-element detection, then headed/headless chromedp
// post). Per the v3.3.0 EC-3-5 cross-repo split decision documented
// in ADR-028, the agentic-ecommerce backend ships ONLY the
// **client-side facade** that calls the omniparser-bridge HTTP
// endpoint via a signed POST. The actual chromedp poster ships
// from the uiauto-framework repo (deferred to v3.7.0 EC-10 OR a
// parallel uiauto-framework PR -- whichever the operator
// prioritises after this MVP merges).
//
// The facade signs every request with the EC-3-1 HMAC primitive so
// the omniparser-bridge can reject forged calls. The bridge URL is
// supplied as a runx alias resolved at composition root time:
// the agent NEVER sees the literal IP / hostname; only the alias.
// Cite skill: rules-no-shell-leak.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): split into Post (envelope), buildSignedRequest (signing),
// parseResponse (decode + status). Per-function cyclomatic stays
// under 6.
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

// MaxRedNoteBridgeBodyBytes caps the omniparser-bridge response.
// Same 1 MiB cap as the TikTok facade so the OOM lessons from
// v322-2 stay enforced.
const MaxRedNoteBridgeBodyBytes = 1 << 20

// DefaultRedNoteBridgeTimeout is the per-request HTTP timeout. The
// uiauto path is slower than direct API calls (cookie load + page
// render + chromedp navigation) so 60s is the conservative default.
const DefaultRedNoteBridgeTimeout = 60 * time.Second

// RedNoteBridgePostPath is the canonical path the omniparser-bridge
// exposes for RedNote posts. Used in the HMAC signature canonical
// form so the bridge can re-verify.
const RedNoteBridgePostPath = "/uiauto/rednote/post"

// RedNoteBridgePlatform is the literal value used for the
// X-Bridge-Platform header so a single omniparser-bridge instance
// can route TikTok / RedNote / future platforms over the same
// HMAC scheme.
const RedNoteBridgePlatform = "rednote"

// EC-4-1 sentinels.
var (
	// ErrRedNoteBridgeUnreachable is returned when the omniparser
	// bridge transport call failed (network error, timeout, DNS
	// failure). Callers can errors.Is to apply backoff.
	ErrRedNoteBridgeUnreachable = errors.New("rednote: bridge unreachable")

	// ErrRedNoteBridgeRejected is returned when the bridge
	// responded with a non-2xx status. The body (truncated) is
	// included.
	ErrRedNoteBridgeRejected = errors.New("rednote: bridge rejected request")

	// ErrRedNoteUnconfigured is returned when a required
	// dependency is missing.
	ErrRedNoteUnconfigured = errors.New("rednote: client unconfigured")

	// ErrRedNoteSignatureBuild is returned when the HMAC signature
	// could not be built (e.g. secret too short).
	ErrRedNoteSignatureBuild = errors.New("rednote: signature build failed")

	// ErrRedNoteClosed is returned by Post after Close.
	ErrRedNoteClosed = errors.New("rednote: client closed")
)

// RedNoteOrganicPost is the EC-4-1 input. The bridge translates
// this into chromedp commands.
type RedNoteOrganicPost struct {
	TenantID    string
	ProductID   string
	ImageURLs   []string
	Caption     string
	Hashtags    []string
	Topics      []string // RedNote-specific topic tags
	ScheduleAt  time.Time
	SessionRef  string // operator-bootstrapped session token alias
	IsLifestyle bool
}

// RedNoteOrganicResult is the EC-4-1 output from the bridge.
type RedNoteOrganicResult struct {
	NoteID     string
	Status     string
	OccurredAt time.Time
}

// RedNoteUIAutoMetrics is the small port the EC-4-1 facade emits
// counters through.
type RedNoteUIAutoMetrics interface {
	RecordRedNoteBridgeCall(tenantID, status string)
}

// RedNoteUIAutoClientConfig wires the EC-4-1 facade. BridgeURL is a
// runx alias resolved at the composition root; never an IP /
// hostname literal in the agent.
type RedNoteUIAutoClientConfig struct {
	HTTPClient   *http.Client
	BridgeURL    string
	BridgeSecret []byte
	TenantID     string
	UserAgent    string
	Now          func() time.Time
	Metrics      RedNoteUIAutoMetrics
}

// RedNoteUIAutoClient calls the omniparser-bridge to drive a
// chromedp poster on the node-a side. Client-side facade only; the
// real chromedp poster ships in uiauto-framework.
type RedNoteUIAutoClient struct {
	cfg    RedNoteUIAutoClientConfig
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewRedNoteUIAutoClient constructs a client.
func NewRedNoteUIAutoClient(logger *slog.Logger, cfg RedNoteUIAutoClientConfig) (*RedNoteUIAutoClient, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.BridgeURL) == "" {
		return nil, fmt.Errorf("%w: BridgeURL required (runx alias for omniparser-bridge node-a)", ErrRedNoteUnconfigured)
	}
	if len(cfg.BridgeSecret) < social.MinTikTokSecretBytes {
		return nil, fmt.Errorf("%w: BridgeSecret < %d bytes", ErrRedNoteUnconfigured, social.MinTikTokSecretBytes)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrRedNoteUnconfigured)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultRedNoteBridgeTimeout}
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (compatible; agentic-ecommerce/3.4.0; rednote-uiauto)"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &RedNoteUIAutoClient{cfg: cfg, logger: logger}, nil
}

// Close marks the client closed.
func (c *RedNoteUIAutoClient) Close(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// Post sends the organic note to the omniparser bridge for chromedp
// dispatch. Returns the bridge result on 2xx.
func (c *RedNoteUIAutoClient) Post(ctx context.Context, req RedNoteOrganicPost) (RedNoteOrganicResult, error) {
	if err := c.guard(); err != nil {
		return RedNoteOrganicResult{}, err
	}
	body, err := json.Marshal(map[string]any{
		"tenant_id":    requireString(req.TenantID, c.cfg.TenantID),
		"product_id":   req.ProductID,
		"image_urls":   req.ImageURLs,
		"caption":      req.Caption,
		"hashtags":     req.Hashtags,
		"topics":       req.Topics,
		"schedule_at":  req.ScheduleAt.UTC().Format(time.RFC3339Nano),
		"session_ref":  req.SessionRef,
		"is_lifestyle": req.IsLifestyle,
	})
	if err != nil {
		c.recordOutcome("encode_failed")
		return RedNoteOrganicResult{}, fmt.Errorf("rednote: encode body: %w", err)
	}
	httpReq, err := c.buildSignedRequest(ctx, body)
	if err != nil {
		c.recordOutcome("sign_failed")
		return RedNoteOrganicResult{}, err
	}
	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		c.recordOutcome("transport_error")
		return RedNoteOrganicResult{}, fmt.Errorf("%w: %v", ErrRedNoteBridgeUnreachable, err)
	}
	defer resp.Body.Close()
	return c.parseResponse(resp)
}

// buildSignedRequest stamps the X-Bridge-* headers with an HMAC
// over the canonical form. Reuses social.ComputeTikTokSignature so
// the bridge has ONE HMAC code path for all platforms.
func (c *RedNoteUIAutoClient) buildSignedRequest(ctx context.Context, body []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BridgeURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rednote: build request: %w", err)
	}
	timestamp := c.cfg.Now().Unix()
	signature, err := social.ComputeTikTokSignature(social.TikTokSignRequest{
		Secret:    c.cfg.BridgeSecret,
		Timestamp: timestamp,
		Path:      RedNoteBridgePostPath,
		Body:      body,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRedNoteSignatureBuild, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	httpReq.Header.Set("X-Bridge-Timestamp", fmt.Sprintf("%d", timestamp))
	httpReq.Header.Set("X-Bridge-Sign", signature)
	httpReq.Header.Set("X-Bridge-Tenant", c.cfg.TenantID)
	httpReq.Header.Set("X-Bridge-Platform", RedNoteBridgePlatform)
	return httpReq, nil
}

// parseResponse decodes the bridge response.
func (c *RedNoteUIAutoClient) parseResponse(resp *http.Response) (RedNoteOrganicResult, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxRedNoteBridgeBodyBytes))
	if err != nil {
		c.recordOutcome("read_failed")
		return RedNoteOrganicResult{}, fmt.Errorf("rednote: read bridge body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.recordOutcome("bridge_rejected")
		return RedNoteOrganicResult{}, fmt.Errorf("%w: status=%d body=%s", ErrRedNoteBridgeRejected, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var wire redNoteBridgeResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		c.recordOutcome("decode_failed")
		return RedNoteOrganicResult{}, fmt.Errorf("rednote: decode bridge body: %w", err)
	}
	occurred, _ := time.Parse(time.RFC3339, wire.OccurredAt)
	if occurred.IsZero() {
		occurred = c.cfg.Now().UTC()
	}
	c.recordOutcome("ok")
	return RedNoteOrganicResult{NoteID: wire.NoteID, Status: wire.Status, OccurredAt: occurred.UTC()}, nil
}

func (c *RedNoteUIAutoClient) guard() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrRedNoteClosed
	}
	return nil
}

func (c *RedNoteUIAutoClient) recordOutcome(outcome string) {
	if c.cfg.Metrics == nil {
		return
	}
	c.cfg.Metrics.RecordRedNoteBridgeCall(c.cfg.TenantID, outcome)
}

// redNoteBridgeResponse mirrors the omniparser-bridge wire shape.
type redNoteBridgeResponse struct {
	NoteID     string `json:"note_id"`
	Status     string `json:"status"`
	OccurredAt string `json:"occurred_at"`
}
