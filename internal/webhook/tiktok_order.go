// File scope: v3.3.0 EC-3-3 TikTok Shop order webhook handler.
//
// The handler is registered against /webhooks/tiktok/orders. On
// every POST it:
//
//  1. Reads the raw body (LimitReader 1 MiB ceiling).
//  2. Verifies the X-Tts-Signature HMAC via the shared
//     social.TikTokWebhookVerifier (verify-then-parse pattern from
//     internal/billing.WebhookVerifier).
//  3. Decodes the wire envelope into the canonical TikTokOrder
//     domain struct.
//  4. Reserves the tenant+order_id idempotency key; duplicates
//     short-circuit with 200 OK so TikTok stops retrying.
//  5. Emits OrderReceivedEvent on the bus.
//
// Decomposition: every step is a small helper (readBody, verify,
// decode, reserve, emit) so per-function cyclomatic stays under 4.
package webhook

import (
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
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

// MaxTikTokOrderBodyBytes is the upper bound for an inbound order
// payload. TikTok's order envelope is well under 64 KiB; we set the
// ceiling at 1 MiB so a malformed huge-body POST cannot OOM the
// handler.
const MaxTikTokOrderBodyBytes = 1 << 20

// TikTokWebhookMetrics is the small port the handler emits webhook
// counters through.
type TikTokWebhookMetrics interface {
	RecordWebhook(tenantID, eventType, status string)
	RecordSignatureFailure(tenantID, reason string)
}

// TikTokOrderHandlerConfig wires the handler.
type TikTokOrderHandlerConfig struct {
	Verifier    *social.TikTokWebhookVerifier
	Publisher   eventbus.Publisher
	Idempotency IdempotencyStore
	TenantID    string // single-tenant in v3.3.0; multi-tenant routing in v3.4
	Channel     string
	Metrics     TikTokWebhookMetrics
	Now         func() time.Time
}

// TikTokOrderHandler is the EC-3-3 receiver.
type TikTokOrderHandler struct {
	cfg    TikTokOrderHandlerConfig
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewTikTokOrderHandler constructs a handler.
func NewTikTokOrderHandler(logger *slog.Logger, cfg TikTokOrderHandlerConfig) (*TikTokOrderHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Verifier == nil {
		return nil, fmt.Errorf("%w: TikTokWebhookVerifier required", ErrWebhookUnconfigured)
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("%w: eventbus.Publisher required", ErrWebhookUnconfigured)
	}
	if cfg.Idempotency == nil {
		return nil, fmt.Errorf("%w: IdempotencyStore required", ErrWebhookUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrWebhookUnconfigured)
	}
	if cfg.Channel == "" {
		cfg.Channel = "tiktok_shop"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &TikTokOrderHandler{cfg: cfg, logger: logger}, nil
}

// Close marks the handler closed.
func (h *TikTokOrderHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP implements net/http Handler.
func (h *TikTokOrderHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.guard(); err != nil {
		writeStatus(w, http.StatusServiceUnavailable, err)
		return
	}
	if r.Method != http.MethodPost {
		writeStatus(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	body, err := readBody(r)
	if err != nil {
		h.recordWebhook("read_failed")
		writeStatus(w, http.StatusBadRequest, err)
		return
	}
	if err := h.verifySignature(r, body); err != nil {
		writeStatus(w, statusForVerifyError(err), err)
		return
	}
	envelope, err := decodeEnvelope(body)
	if err != nil {
		h.recordWebhook("decode_failed")
		writeStatus(w, http.StatusBadRequest, err)
		return
	}
	payload, err := h.buildPayload(envelope)
	if err != nil {
		h.recordWebhook("payload_invalid")
		writeStatus(w, http.StatusBadRequest, err)
		return
	}
	if duplicate, err := h.reserveIdempotency(r.Context(), payload.IdempotencyKey); err != nil {
		h.recordWebhook("idempotency_error")
		writeStatus(w, http.StatusInternalServerError, err)
		return
	} else if duplicate {
		h.recordWebhook("duplicate")
		writeStatus(w, http.StatusOK, nil)
		return
	}
	if err := h.emit(r.Context(), payload); err != nil {
		h.recordWebhook("publish_failed")
		writeStatus(w, http.StatusInternalServerError, err)
		return
	}
	h.recordWebhook("ok")
	writeStatus(w, http.StatusOK, nil)
}

func (h *TikTokOrderHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrWebhookClosed
	}
	return nil
}

// verifySignature wraps verify-then-parse semantics around the
// shared HMAC primitive. The header is X-Tts-Signature.
func (h *TikTokOrderHandler) verifySignature(r *http.Request, body []byte) error {
	header := r.Header.Get("X-Tts-Signature")
	if err := h.cfg.Verifier.Verify(header, body); err != nil {
		reason := signatureFailureReason(err)
		h.cfg.Metrics.RecordSignatureFailure(h.cfg.TenantID, reason)
		return err
	}
	return nil
}

func (h *TikTokOrderHandler) buildPayload(env tiktokOrderWire) (eventbus.OrderReceivedPayload, error) {
	tenantID := h.cfg.TenantID
	if env.TenantID != "" {
		tenantID = env.TenantID
	}
	if env.OrderID == "" {
		return eventbus.OrderReceivedPayload{}, fmt.Errorf("%w: order_id missing", ErrWebhookPayloadInvalid)
	}
	if len(env.Items) == 0 {
		return eventbus.OrderReceivedPayload{}, fmt.Errorf("%w: at least one item required", ErrWebhookPayloadInvalid)
	}
	idempotencyKey := env.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = env.OrderID
	}
	occurredAt, _ := time.Parse(time.RFC3339, env.OccurredAt)
	if occurredAt.IsZero() {
		occurredAt = h.cfg.Now().UTC()
	}
	items := make([]eventbus.OrderReceivedLine, 0, len(env.Items))
	for _, line := range env.Items {
		items = append(items, eventbus.OrderReceivedLine{
			SKU:         line.SKU,
			Quantity:    line.Quantity,
			UnitCents:   line.UnitCents,
			ProductID:   line.ProductID,
			WarehouseID: line.WarehouseID,
		})
	}
	return eventbus.OrderReceivedPayload{
		TenantID:       tenantID,
		OrderID:        env.OrderID,
		ShopID:         env.ShopID,
		Channel:        h.cfg.Channel,
		BuyerEmail:     env.BuyerEmail,
		TotalCents:     env.TotalCents,
		Currency:       env.Currency,
		Items:          items,
		Status:         env.Status,
		IdempotencyKey: idempotencyKey,
		OccurredAt:     occurredAt.UTC(),
	}, nil
}

func (h *TikTokOrderHandler) reserveIdempotency(ctx context.Context, key string) (bool, error) {
	allowed, err := h.cfg.Idempotency.Reserve(ctx, h.cfg.TenantID, key)
	if err != nil {
		return false, fmt.Errorf("idempotency: %w", err)
	}
	return !allowed, nil
}

func (h *TikTokOrderHandler) emit(ctx context.Context, payload eventbus.OrderReceivedPayload) error {
	evt, err := eventbus.NewOrderReceivedEvent("webhook.tiktok.order", h.cfg.Now(), payload)
	if err != nil {
		return fmt.Errorf("build order event: %w", err)
	}
	return h.cfg.Publisher.Publish(ctx, evt)
}

func (h *TikTokOrderHandler) recordWebhook(status string) {
	if h.cfg.Metrics == nil {
		return
	}
	h.cfg.Metrics.RecordWebhook(h.cfg.TenantID, "order", status)
}

// readBody reads up to MaxTikTokOrderBodyBytes from r.Body.
func readBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxTikTokOrderBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// decodeEnvelope unmarshals the raw body to the wire shape.
func decodeEnvelope(body []byte) (tiktokOrderWire, error) {
	if len(body) == 0 {
		return tiktokOrderWire{}, fmt.Errorf("%w: empty body", ErrWebhookPayloadInvalid)
	}
	var env tiktokOrderWire
	if err := json.Unmarshal(body, &env); err != nil {
		return tiktokOrderWire{}, fmt.Errorf("%w: %v", ErrWebhookPayloadInvalid, err)
	}
	return env, nil
}

// statusForVerifyError maps a verifier error category to an HTTP
// status code. The webhook surface uses 401 for both missing /
// malformed / mismatch (TikTok retries) and 400 for old events
// (poison-pill so retries stop).
func statusForVerifyError(err error) int {
	switch {
	case errors.Is(err, social.ErrTikTokMissingSignature),
		errors.Is(err, social.ErrTikTokSignatureMismatch),
		errors.Is(err, social.ErrTikTokSignatureMalformed):
		return http.StatusUnauthorized
	case errors.Is(err, social.ErrTikTokEventTooOld):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// signatureFailureReason returns a Prometheus-friendly label for the
// signature failure metric. Bounded set so cardinality stays low.
func signatureFailureReason(err error) string {
	switch {
	case errors.Is(err, social.ErrTikTokMissingSignature):
		return "missing"
	case errors.Is(err, social.ErrTikTokSignatureMismatch):
		return "mismatch"
	case errors.Is(err, social.ErrTikTokSignatureMalformed):
		return "malformed"
	case errors.Is(err, social.ErrTikTokEventTooOld):
		return "expired"
	default:
		return "other"
	}
}

func writeStatus(w http.ResponseWriter, status int, err error) {
	w.WriteHeader(status)
	if err != nil {
		_, _ = io.WriteString(w, err.Error())
	}
}

// tiktokOrderWire is the inbound JSON shape. Mirrors the fields the
// official TikTok Shop order webhook ships.
type tiktokOrderWire struct {
	TenantID       string                `json:"tenant_id,omitempty"`
	OrderID        string                `json:"order_id"`
	ShopID         string                `json:"shop_id"`
	BuyerEmail     string                `json:"buyer_email"`
	TotalCents     int                   `json:"total_cents"`
	Currency       string                `json:"currency"`
	Items          []tiktokOrderLineWire `json:"items"`
	Status         string                `json:"status"`
	IdempotencyKey string                `json:"idempotency_key,omitempty"`
	OccurredAt     string                `json:"occurred_at,omitempty"`
}

type tiktokOrderLineWire struct {
	SKU         string `json:"sku"`
	Quantity    int    `json:"quantity"`
	UnitCents   int    `json:"unit_cents"`
	ProductID   string `json:"product_id,omitempty"`
	WarehouseID string `json:"warehouse_id,omitempty"`
}
