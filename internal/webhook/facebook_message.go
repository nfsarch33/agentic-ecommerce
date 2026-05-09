// File scope: v3.6.0 EC-8-3 inbound Facebook Messenger webhook.
//
// Mirrors the v3.4.0 EC-4-2 Facebook Shop signing pattern (X-Hub-
// Signature-256: sha256=<hex(HMAC-SHA256(body, app_secret))>).
// Idempotency on message_id + channel via the shared
// IdempotencyStore.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4): same shape as TikTokMessageHandler so reviewers
// can diff the two surfaces.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/social"
)

// FacebookMessageHandlerConfig wires the Facebook inbound message
// webhook handler.
type FacebookMessageHandlerConfig struct {
	AppSecret   []byte
	Pipeline    *MessagingPipeline
	Idempotency IdempotencyStore
	TenantID    string
	Channel     string
	Metrics     MessagingPipelineMetrics
	Now         func() time.Time
}

// FacebookMessageHandler is the EC-8-3 Facebook Messenger inbound
// receiver.
type FacebookMessageHandler struct {
	cfg    FacebookMessageHandlerConfig
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewFacebookMessageHandler constructs a Facebook message webhook
// handler.
func NewFacebookMessageHandler(logger *slog.Logger, cfg FacebookMessageHandlerConfig) (*FacebookMessageHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(cfg.AppSecret) == 0 {
		return nil, fmt.Errorf("%w: AppSecret required", ErrWebhookUnconfigured)
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
		cfg.Channel = "facebook"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &FacebookMessageHandler{cfg: cfg, logger: logger}, nil
}

// Close marks the handler closed.
func (h *FacebookMessageHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP implements net/http Handler.
func (h *FacebookMessageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		writeStatus(w, http.StatusUnauthorized, err)
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

func (h *FacebookMessageHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrWebhookClosed
	}
	return nil
}

func (h *FacebookMessageHandler) verifySignature(r *http.Request, body []byte) error {
	header := r.Header.Get("X-Hub-Signature-256")
	if err := social.VerifyFacebookWebhook(h.cfg.AppSecret, header, body); err != nil {
		h.recordOutcome("hmac_failed")
		return fmt.Errorf("%w: %v", ErrInvalidMessageHMAC, err)
	}
	return nil
}

func (h *FacebookMessageHandler) decodeMessageEnv(body []byte) (InboundMessage, error) {
	if len(body) == 0 {
		return InboundMessage{}, fmt.Errorf("%w: empty body", ErrWebhookPayloadInvalid)
	}
	var env facebookMessageWire
	if err := json.Unmarshal(body, &env); err != nil {
		return InboundMessage{}, fmt.Errorf("%w: %v", ErrWebhookPayloadInvalid, err)
	}
	flat, err := flattenFacebookEnvelope(env)
	if err != nil {
		return InboundMessage{}, err
	}
	tenantID := h.cfg.TenantID
	if flat.TenantID != "" {
		tenantID = flat.TenantID
	}
	occurredAt := time.Unix(flat.TimestampMs/1000, (flat.TimestampMs%1000)*int64(time.Millisecond)).UTC()
	if flat.TimestampMs <= 0 {
		occurredAt = h.cfg.Now().UTC()
	}
	return InboundMessage{
		TenantID:   tenantID,
		MessageID:  flat.MessageID,
		Channel:    h.cfg.Channel,
		ThreadID:   flat.ThreadID,
		BuyerID:    flat.BuyerID,
		Text:       flat.Text,
		OccurredAt: occurredAt,
	}, nil
}

func (h *FacebookMessageHandler) reserveDup(ctx context.Context, messageID string) (bool, error) {
	key := h.cfg.Channel + "\x00" + messageID
	allowed, err := h.cfg.Idempotency.Reserve(ctx, h.cfg.TenantID, key)
	if err != nil {
		return false, fmt.Errorf("idempotency: %w", err)
	}
	return !allowed, nil
}

func (h *FacebookMessageHandler) process(ctx context.Context, w http.ResponseWriter, msg InboundMessage) {
	outcome, err := h.cfg.Pipeline.Process(ctx, msg)
	if err != nil {
		h.logger.Warn("facebook.message_pipeline_error", "tenant_id", msg.TenantID, "message_id", msg.MessageID, "error", err)
		h.recordOutcome("pipeline_error")
		writeStatus(w, http.StatusInternalServerError, err)
		return
	}
	h.recordOutcome(string(outcome))
	writeStatus(w, http.StatusOK, nil)
}

func (h *FacebookMessageHandler) recordOutcome(status string) {
	if h.cfg.Metrics == nil {
		return
	}
	h.cfg.Metrics.RecordMessageWebhook(h.cfg.TenantID, h.cfg.Channel, status)
}

// facebookMessageWire mirrors the Messenger Send API webhook
// envelope shape (entry[].messaging[].{message,sender,timestamp}).
// We accept either the production entry/messaging shape or the
// flattened shape used in fixtures/tests.
type facebookMessageWire struct {
	TenantID string                 `json:"tenant_id,omitempty"`
	Object   string                 `json:"object,omitempty"`
	Entry    []facebookMessageEntry `json:"entry,omitempty"`
	// Flat fields (for test fixtures + simplified payloads).
	MessageID  string `json:"message_id,omitempty"`
	ThreadID   string `json:"thread_id,omitempty"`
	BuyerID    string `json:"buyer_id,omitempty"`
	Text       string `json:"text,omitempty"`
	OccurredAt string `json:"occurred_at,omitempty"`
}

type facebookMessageEntry struct {
	ID        string                   `json:"id"`
	Time      int64                    `json:"time"`
	Messaging []facebookMessagingEntry `json:"messaging"`
}

type facebookMessagingEntry struct {
	Sender    facebookID         `json:"sender"`
	Recipient facebookID         `json:"recipient"`
	Timestamp int64              `json:"timestamp"`
	Message   facebookMessageObj `json:"message"`
}

type facebookID struct {
	ID string `json:"id"`
}

type facebookMessageObj struct {
	MID  string `json:"mid"`
	Text string `json:"text"`
}

// flattenFacebookEnvelope normalises the Messenger Send API
// nested envelope into the canonical flat shape the pipeline
// consumes. Falls back to the flattened wire fields when the
// nested entry/messaging block is absent (tests use the flat
// shape; production uses the nested shape).
type facebookFlatMessage struct {
	TenantID    string
	MessageID   string
	ThreadID    string
	BuyerID     string
	Text        string
	TimestampMs int64
}

func flattenFacebookEnvelope(env facebookMessageWire) (facebookFlatMessage, error) {
	flat := facebookFlatMessage{
		TenantID:  env.TenantID,
		MessageID: env.MessageID,
		ThreadID:  env.ThreadID,
		BuyerID:   env.BuyerID,
		Text:      env.Text,
	}
	if env.OccurredAt != "" {
		t, _ := time.Parse(time.RFC3339, env.OccurredAt)
		flat.TimestampMs = t.UnixMilli()
	}
	if len(env.Entry) > 0 && len(env.Entry[0].Messaging) > 0 {
		entry := env.Entry[0]
		first := entry.Messaging[0]
		if flat.MessageID == "" {
			flat.MessageID = first.Message.MID
		}
		if flat.Text == "" {
			flat.Text = first.Message.Text
		}
		if flat.BuyerID == "" {
			flat.BuyerID = first.Sender.ID
		}
		if flat.ThreadID == "" {
			flat.ThreadID = first.Sender.ID
		}
		if flat.TimestampMs <= 0 {
			flat.TimestampMs = first.Timestamp
		}
	}
	if flat.MessageID == "" {
		return flat, fmt.Errorf("%w: message_id (mid) missing", ErrWebhookPayloadInvalid)
	}
	if flat.Text == "" {
		return flat, fmt.Errorf("%w: text missing", ErrWebhookPayloadInvalid)
	}
	return flat, nil
}
