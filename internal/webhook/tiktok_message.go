// File scope: v3.6.0 EC-8-3 inbound TikTok Shop messaging webhook.
//
// Mirrors the v3.3.0 EC-3-3 order webhook shape (HMAC verify-then-
// parse via the shared social.TikTokWebhookVerifier; idempotency on
// message_id + channel; emit typed event) so reviewers comparing
// the two webhook surfaces see the same scheme.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4):
//
//   - ServeHTTP        -> envelope (cyclomatic 5)
//   - readBody         -> shared helper from tiktok_order.go
//   - verifySignature  -> HMAC delegate (cyclomatic 3)
//   - decodeMessageEnv -> JSON parse (cyclomatic 3)
//   - reserveDup       -> idempotency gate (cyclomatic 3)
//   - process          -> pipeline dispatch (cyclomatic 3)
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/social"
)

// MaxMessageWebhookBodyBytes is the upper bound for an inbound
// message webhook payload. The TikTok message envelope is well
// under 64 KiB; we cap at 1 MiB so a malformed huge-body POST
// cannot OOM the handler.
const MaxMessageWebhookBodyBytes = 1 << 20

// TikTokMessageHandlerConfig wires the TikTok inbound message
// webhook handler.
type TikTokMessageHandlerConfig struct {
	Verifier    *social.TikTokWebhookVerifier
	Pipeline    *MessagingPipeline
	Idempotency IdempotencyStore
	TenantID    string
	Channel     string
	Metrics     MessagingPipelineMetrics
	Now         func() time.Time
}

// TikTokMessageHandler is the EC-8-3 TikTok inbound receiver.
type TikTokMessageHandler struct {
	cfg    TikTokMessageHandlerConfig
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewTikTokMessageHandler constructs a TikTok message webhook handler.
func NewTikTokMessageHandler(logger *slog.Logger, cfg TikTokMessageHandlerConfig) (*TikTokMessageHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Verifier == nil {
		return nil, fmt.Errorf("%w: TikTokWebhookVerifier required", ErrWebhookUnconfigured)
	}
	if cfg.Pipeline == nil {
		return nil, fmt.Errorf("%w: MessagingPipeline required", ErrWebhookUnconfigured)
	}
	if cfg.Idempotency == nil {
		return nil, fmt.Errorf("%w: IdempotencyStore required", ErrWebhookUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrWebhookUnconfigured)
	}
	if cfg.Channel == "" {
		cfg.Channel = "tiktok"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &TikTokMessageHandler{cfg: cfg, logger: logger}, nil
}

// Close marks the handler closed.
func (h *TikTokMessageHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP implements net/http Handler.
func (h *TikTokMessageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.guard(); err != nil {
		writeStatus(w, http.StatusServiceUnavailable, err)
		return
	}
	if r.Method != http.MethodPost {
		writeStatus(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	body, err := readMessageBody(r)
	if err != nil {
		h.recordOutcome("read_failed")
		writeStatus(w, http.StatusBadRequest, err)
		return
	}
	if err := h.verifySignature(r, body); err != nil {
		writeStatus(w, statusForMessagingHMACError(err), err)
		return
	}
	msg, err := h.decodeMessageEnv(body)
	if err != nil {
		h.recordOutcome("decode_failed")
		writeStatus(w, http.StatusBadRequest, err)
		return
	}
	if duplicate, err := h.reserveDup(r.Context(), msg.MessageID); err != nil {
		h.recordOutcome("idempotency_error")
		writeStatus(w, http.StatusInternalServerError, err)
		return
	} else if duplicate {
		h.recordOutcome("duplicate")
		writeStatus(w, http.StatusOK, nil)
		return
	}
	h.process(r.Context(), w, msg)
}

func (h *TikTokMessageHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrWebhookClosed
	}
	return nil
}

func (h *TikTokMessageHandler) verifySignature(r *http.Request, body []byte) error {
	header := r.Header.Get("X-Tts-Signature")
	if err := h.cfg.Verifier.Verify(header, body); err != nil {
		h.recordOutcome("hmac_failed")
		return fmt.Errorf("%w: %v", ErrInvalidMessageHMAC, err)
	}
	return nil
}

func (h *TikTokMessageHandler) decodeMessageEnv(body []byte) (InboundMessage, error) {
	if len(body) == 0 {
		return InboundMessage{}, fmt.Errorf("%w: empty body", ErrWebhookPayloadInvalid)
	}
	var env tikTokMessageWire
	if err := json.Unmarshal(body, &env); err != nil {
		return InboundMessage{}, fmt.Errorf("%w: %v", ErrWebhookPayloadInvalid, err)
	}
	if env.MessageID == "" {
		return InboundMessage{}, fmt.Errorf("%w: message_id missing", ErrWebhookPayloadInvalid)
	}
	if env.Text == "" {
		return InboundMessage{}, fmt.Errorf("%w: text missing", ErrWebhookPayloadInvalid)
	}
	tenantID := h.cfg.TenantID
	if env.TenantID != "" {
		tenantID = env.TenantID
	}
	occurredAt, _ := time.Parse(time.RFC3339, env.OccurredAt)
	if occurredAt.IsZero() {
		occurredAt = h.cfg.Now().UTC()
	}
	return InboundMessage{
		TenantID:   tenantID,
		MessageID:  env.MessageID,
		Channel:    h.cfg.Channel,
		ThreadID:   env.ThreadID,
		BuyerID:    env.BuyerID,
		Text:       env.Text,
		OccurredAt: occurredAt.UTC(),
	}, nil
}

func (h *TikTokMessageHandler) reserveDup(ctx context.Context, messageID string) (bool, error) {
	key := h.cfg.Channel + "\x00" + messageID
	allowed, err := h.cfg.Idempotency.Reserve(ctx, h.cfg.TenantID, key)
	if err != nil {
		return false, fmt.Errorf("idempotency: %w", err)
	}
	return !allowed, nil
}

func (h *TikTokMessageHandler) process(ctx context.Context, w http.ResponseWriter, msg InboundMessage) {
	outcome, err := h.cfg.Pipeline.Process(ctx, msg)
	if err != nil {
		h.logger.Warn("tiktok.message_pipeline_error", "tenant_id", msg.TenantID, "message_id", msg.MessageID, "error", err)
		h.recordOutcome("pipeline_error")
		writeStatus(w, http.StatusInternalServerError, err)
		return
	}
	h.recordOutcome(string(outcome))
	writeStatus(w, http.StatusOK, nil)
}

func (h *TikTokMessageHandler) recordOutcome(status string) {
	if h.cfg.Metrics == nil {
		return
	}
	h.cfg.Metrics.RecordMessageWebhook(h.cfg.TenantID, h.cfg.Channel, status)
}

// readMessageBody reads up to MaxMessageWebhookBodyBytes from the
// request body. Shared between the TikTok + Facebook receivers.
func readMessageBody(r *http.Request) ([]byte, error) {
	body, err := readLimitedBody(r.Body, MaxMessageWebhookBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// readLimitedBody is the io-injectable variant of readBody from
// tiktok_order.go. Callers pass either an *http.Request body or
// any io.Reader so the helper is reusable across the messaging
// surfaces.
func readLimitedBody(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// tikTokMessageWire is the inbound TikTok message JSON shape.
type tikTokMessageWire struct {
	TenantID   string `json:"tenant_id,omitempty"`
	MessageID  string `json:"message_id"`
	ThreadID   string `json:"thread_id"`
	BuyerID    string `json:"buyer_id"`
	Text       string `json:"text"`
	OccurredAt string `json:"occurred_at,omitempty"`
}
