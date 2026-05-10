package webhook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// v4.3.0 payment webhook normaliser errors.
var (
	ErrNormaliserUnconfigured = errors.New("webhook: normaliser unconfigured")
	ErrNormaliserClosed       = errors.New("webhook: normaliser closed")
	ErrUnknownProvider        = errors.New("webhook: unknown payment provider")
)

const maxWebhookBodyBytes = 64 * 1024

// PaymentWebhookVerifiedEvent carries the normalised outcome of a
// verified inbound payment webhook, ready for downstream consumers.
type PaymentWebhookVerifiedEvent struct {
	Provider  string                `json:"provider"`
	EventID   string                `json:"event_id"`
	EventType port.WebhookEventType `json:"event_type"`
	PaymentID string                `json:"payment_id"`
	Timestamp time.Time             `json:"timestamp"`
}

// PaymentNormaliserConfig wires the normaliser.
type PaymentNormaliserConfig struct {
	Providers  map[string]port.MultiPaymentGateway
	Publisher  eventbus.Publisher
	Idempotent IdempotencyStore
	Now        func() time.Time
	Metrics    PaymentNormaliserMetrics
}

// PaymentNormaliserMetrics is the small port for Prometheus counters.
type PaymentNormaliserMetrics interface {
	IncWebhookNormalised(provider, outcome string)
}

// PaymentNormaliser is the v4.3.0 unified payment webhook handler.
// Routes POST /api/v1/webhooks/payment/:provider to the correct
// adapter's VerifyWebhook, deduplicates, then publishes a
// normalised event to the eventbus.
type PaymentNormaliser struct {
	providers  map[string]port.MultiPaymentGateway
	publisher  eventbus.Publisher
	idempotent IdempotencyStore
	now        func() time.Time
	logger     *slog.Logger
	metrics    PaymentNormaliserMetrics

	mu     sync.Mutex
	closed bool
}

// NewPaymentNormaliser constructs the normaliser.
func NewPaymentNormaliser(logger *slog.Logger, cfg PaymentNormaliserConfig) (*PaymentNormaliser, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("%w: at least one provider required", ErrNormaliserUnconfigured)
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("%w: publisher required", ErrNormaliserUnconfigured)
	}
	if cfg.Idempotent == nil {
		cfg.Idempotent = NewMemoryIdempotencyStore()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &PaymentNormaliser{
		providers:  cfg.Providers,
		publisher:  cfg.Publisher,
		idempotent: cfg.Idempotent,
		now:        cfg.Now,
		logger:     logger,
		metrics:    cfg.Metrics,
	}, nil
}

// Close marks the handler closed.
func (n *PaymentNormaliser) Close(_ context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closed = true
	return nil
}

// ServeHTTP routes by provider suffix, verifies, deduplicates, and
// publishes normalised events.
func (n *PaymentNormaliser) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := n.guard(); err != nil {
		writeNormaliserError(w, http.StatusServiceUnavailable, err)
		return
	}
	if r.Method != http.MethodPost {
		writeNormaliserError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	provider := routeProvider(r.URL.Path)
	if provider == "" {
		writeNormaliserError(w, http.StatusNotFound, ErrUnknownProvider)
		return
	}
	n.verifyAndEmit(w, r, provider)
}

func (n *PaymentNormaliser) guard() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return ErrNormaliserClosed
	}
	return nil
}

// routeProvider extracts the provider slug from the path.
func routeProvider(path string) string {
	const prefix = "/api/v1/webhooks/payment/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	provider := strings.TrimPrefix(path, prefix)
	provider = strings.TrimSuffix(provider, "/")
	switch provider {
	case "stripe", "alipay", "wechat", "paypal":
		return provider
	default:
		return ""
	}
}

// verifyAndEmit verifies the webhook with the provider adapter,
// deduplicates via idempotency store, and publishes a normalised
// event to the eventbus.
func (n *PaymentNormaliser) verifyAndEmit(w http.ResponseWriter, r *http.Request, provider string) {
	adapter, ok := n.providers[provider]
	if !ok {
		n.incMetric(provider, "unknown_provider")
		writeNormaliserError(w, http.StatusBadRequest, ErrUnknownProvider)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes))
	if err != nil {
		n.incMetric(provider, "read_error")
		writeNormaliserError(w, http.StatusBadRequest, fmt.Errorf("read body: %w", err))
		return
	}
	evt, err := adapter.VerifyWebhook(r.Context(), r.Header, body)
	if err != nil {
		n.incMetric(provider, "rejected")
		n.logger.Warn("webhook.verify_failed", "provider", provider, "error", err)
		writeNormaliserError(w, http.StatusUnauthorized, port.ErrInvalidWebhookSignature)
		return
	}
	dedupKey := evt.PaymentID + ":" + string(evt.Type)
	novel, err := n.idempotent.Reserve(r.Context(), provider, dedupKey)
	if err != nil {
		n.incMetric(provider, "dedup_error")
		writeNormaliserError(w, http.StatusInternalServerError, err)
		return
	}
	if !novel {
		n.incMetric(provider, "duplicate")
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := n.publishVerifiedEvent(r.Context(), provider, evt); err != nil {
		n.incMetric(provider, "publish_error")
		n.logger.Error("webhook.publish_failed", "provider", provider, "error", err)
		writeNormaliserError(w, http.StatusInternalServerError, err)
		return
	}
	n.incMetric(provider, "accepted")
	w.WriteHeader(http.StatusOK)
}

func (n *PaymentNormaliser) publishVerifiedEvent(ctx context.Context, provider string, evt port.WebhookEvent) error {
	payload := eventbus.PaymentSagaPayload{
		Version:  eventbus.PaymentSagaPayloadVersion,
		TenantID: provider,
		OrderID:  evt.PaymentID,
		Provider: provider,
		Status:   string(evt.Type),
	}
	busEvt, err := eventbus.NewPaymentCompletedEvent("webhook.normaliser", n.now(), payload)
	if err != nil {
		return err
	}
	return n.publisher.Publish(ctx, busEvt)
}

func (n *PaymentNormaliser) incMetric(provider, outcome string) {
	if n.metrics != nil {
		n.metrics.IncWebhookNormalised(provider, outcome)
	}
}

func writeNormaliserError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, err.Error())
}
