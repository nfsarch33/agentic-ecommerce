// File scope: v3.6.0 EC-8-3 shared customer-message processing
// pipeline.
//
// MessagingPipeline is the channel-agnostic component that the
// per-channel webhooks (tiktok_message.go, facebook_message.go)
// hand a normalised InboundMessage off to. The pipeline:
//
//  1. Emits CustomerMessageReceived (the inbound webhook has
//     already verified HMAC + idempotency).
//  2. Calls the EC-8-1 classifier.
//  3. Calls the EC-8-2 responder.
//  4. If the responder produced an auto-reply, calls the
//     OutboundMessageSender (per-channel) to deliver the reply,
//     then emits CustomerMessageReplied.
//  5. Otherwise emits CustomerMessageEscalatedToOperator.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4):
//
//   - Process               -> envelope (cyclomatic 4)
//   - emitReceived          -> publish (cyclomatic 2)
//   - classify              -> classifier dispatch (cyclomatic 3)
//   - respond               -> responder dispatch (cyclomatic 3)
//   - dispatchReply         -> sender + emit (cyclomatic 5)
//   - escalate              -> emit + log (cyclomatic 2)
package webhook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/social"
	"github.com/nfsarch33/agentic-ecommerce/internal/agent/customerservice"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// EC-8-3 typed sentinels.
var (
	// ErrInvalidMessageHMAC is returned by the per-channel webhook
	// when the inbound HMAC verification fails.
	ErrInvalidMessageHMAC = errors.New("messaging: invalid hmac signature")

	// ErrMessageDuplicate is returned by the idempotency guard.
	ErrMessageDuplicate = errors.New("messaging: duplicate delivery")

	// ErrChannelSendFailed is returned when the per-channel
	// adapter's SendMessage call failed.
	ErrChannelSendFailed = errors.New("messaging: channel send failed")
)

// InboundMessage is the channel-normalised shape the pipeline
// processes. The per-channel webhook is responsible for HMAC
// verification + envelope decoding; the pipeline only sees this
// canonical shape.
type InboundMessage struct {
	TenantID   string
	MessageID  string
	Channel    string
	ThreadID   string
	BuyerID    string
	Text       string
	OccurredAt time.Time
}

// MessagingPipelineMetrics is the small port the pipeline emits
// counters through. Mirrors the webhook tiktok metrics pattern.
type MessagingPipelineMetrics interface {
	RecordMessageWebhook(tenantID, channel, status string)
}

// MessagingPipelineConfig wires the pipeline.
type MessagingPipelineConfig struct {
	Classifier *customerservice.EnquiryClassifier
	Responder  *customerservice.FAQResponder
	Senders    map[string]port.OutboundMessageSender // channel -> sender
	Publisher  eventbus.Publisher
	Now        func() time.Time
	Metrics    MessagingPipelineMetrics
}

// MessagingPipeline is the channel-agnostic processor.
type MessagingPipeline struct {
	classifier *customerservice.EnquiryClassifier
	responder  *customerservice.FAQResponder
	senders    map[string]port.OutboundMessageSender
	publisher  eventbus.Publisher
	now        func() time.Time
	logger     *slog.Logger
	metrics    MessagingPipelineMetrics

	mu     sync.Mutex
	closed bool
}

// NewMessagingPipeline constructs the pipeline.
func NewMessagingPipeline(logger *slog.Logger, cfg MessagingPipelineConfig) (*MessagingPipeline, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Classifier == nil {
		return nil, fmt.Errorf("%w: EnquiryClassifier required", ErrWebhookUnconfigured)
	}
	if cfg.Responder == nil {
		return nil, fmt.Errorf("%w: FAQResponder required", ErrWebhookUnconfigured)
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("%w: eventbus.Publisher required", ErrWebhookUnconfigured)
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Senders == nil {
		cfg.Senders = map[string]port.OutboundMessageSender{}
	}
	return &MessagingPipeline{
		classifier: cfg.Classifier,
		responder:  cfg.Responder,
		senders:    cfg.Senders,
		publisher:  cfg.Publisher,
		now:        cfg.Now,
		logger:     logger,
		metrics:    cfg.Metrics,
	}, nil
}

// Close marks the pipeline closed. Implements lifecycle.Closer.
func (p *MessagingPipeline) Close(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

// PipelineOutcome captures the routed destination of a message.
type PipelineOutcome string

const (
	PipelineOutcomeReplied   PipelineOutcome = "replied"
	PipelineOutcomeSuggested PipelineOutcome = "suggested"
	PipelineOutcomeEscalated PipelineOutcome = "escalated"
	PipelineOutcomeFailed    PipelineOutcome = "failed"
)

// Process drives the inbound message through classify -> respond
// -> route. Returns the routed outcome so the per-channel webhook
// can emit the right metric label.
func (p *MessagingPipeline) Process(ctx context.Context, msg InboundMessage) (PipelineOutcome, error) {
	if err := p.guardProcess(msg); err != nil {
		return PipelineOutcomeFailed, err
	}
	if err := p.emitReceived(ctx, msg); err != nil {
		return PipelineOutcomeFailed, fmt.Errorf("emit received: %w", err)
	}
	classification, err := p.classify(ctx, msg)
	if err != nil {
		return PipelineOutcomeFailed, fmt.Errorf("classify: %w", err)
	}
	answer, err := p.respond(ctx, msg, classification)
	if err != nil {
		return PipelineOutcomeFailed, fmt.Errorf("respond: %w", err)
	}
	if answer.Outcome == customerservice.FAQOutcomeAutoReplied {
		return p.dispatchReply(ctx, msg, classification, answer)
	}
	return p.escalate(ctx, msg, classification, answer)
}

func (p *MessagingPipeline) guardProcess(msg InboundMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrWebhookClosed
	}
	if strings.TrimSpace(msg.MessageID) == "" || strings.TrimSpace(msg.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id + message_id required", ErrWebhookPayloadInvalid)
	}
	return nil
}

// emitReceived publishes the CustomerMessageReceived event.
func (p *MessagingPipeline) emitReceived(ctx context.Context, msg InboundMessage) error {
	payload := eventbus.CustomerMessagePayload{
		Version:    eventbus.CustomerMessagePayloadVersion,
		TenantID:   msg.TenantID,
		MessageID:  msg.MessageID,
		Channel:    msg.Channel,
		ThreadID:   msg.ThreadID,
		BuyerID:    msg.BuyerID,
		OccurredAt: msg.OccurredAt,
	}
	evt, err := eventbus.NewCustomerMessageReceivedEvent("webhook.messaging."+msg.Channel, p.now(), payload)
	if err != nil {
		return err
	}
	return p.publisher.Publish(ctx, evt)
}

// classify dispatches to the EC-8-1 classifier.
func (p *MessagingPipeline) classify(ctx context.Context, msg InboundMessage) (customerservice.EnquiryResult, error) {
	return p.classifier.Classify(ctx, customerservice.EnquiryRequest{
		MessageID: msg.MessageID,
		TenantID:  msg.TenantID,
		Channel:   msg.Channel,
		Text:      msg.Text,
	})
}

// respond dispatches to the EC-8-2 responder.
func (p *MessagingPipeline) respond(ctx context.Context, msg InboundMessage, cls customerservice.EnquiryResult) (customerservice.FAQResult, error) {
	return p.responder.Respond(ctx, customerservice.FAQRequest{
		MessageID:      msg.MessageID,
		TenantID:       msg.TenantID,
		Channel:        msg.Channel,
		Text:           msg.Text,
		Classification: cls,
	})
}

// dispatchReply sends the reply via the channel adapter and emits
// CustomerMessageReplied. On send failure routes to escalate.
func (p *MessagingPipeline) dispatchReply(ctx context.Context, msg InboundMessage, cls customerservice.EnquiryResult, answer customerservice.FAQResult) (PipelineOutcome, error) {
	sender, ok := p.senders[msg.Channel]
	if !ok {
		p.logger.Warn("messaging.no_sender_registered", "tenant_id", msg.TenantID, "channel", msg.Channel)
		_, _ = p.escalate(ctx, msg, cls, answer)
		return PipelineOutcomeEscalated, nil
	}
	resp, err := sender.SendMessage(ctx, port.OutboundMessageRequest{
		TenantID: msg.TenantID,
		ThreadID: msg.ThreadID,
		Text:     answer.ReplyText,
	})
	if err != nil {
		p.logger.Warn("messaging.channel_send_failed", "tenant_id", msg.TenantID, "channel", msg.Channel, "error", err)
		// Channel send failure -> escalate so an operator can retry.
		_, _ = p.escalate(ctx, msg, cls, answer)
		return PipelineOutcomeEscalated, fmt.Errorf("%w: %v", ErrChannelSendFailed, err)
	}
	if err := p.publishReplied(ctx, msg, cls, answer, resp.ProviderMessageID); err != nil {
		return PipelineOutcomeFailed, err
	}
	return PipelineOutcomeReplied, nil
}

// escalate emits CustomerMessageEscalatedToOperator (and surfaces
// the suggest path to the operator queue with the suggested reply).
func (p *MessagingPipeline) escalate(ctx context.Context, msg InboundMessage, cls customerservice.EnquiryResult, answer customerservice.FAQResult) (PipelineOutcome, error) {
	payload := eventbus.CustomerMessagePayload{
		Version:         eventbus.CustomerMessagePayloadVersion,
		TenantID:        msg.TenantID,
		MessageID:       msg.MessageID,
		Channel:         msg.Channel,
		ThreadID:        msg.ThreadID,
		BuyerID:         msg.BuyerID,
		Intent:          string(cls.Intent),
		Sentiment:       string(cls.Sentiment),
		Language:        string(cls.Language),
		ConfidenceScore: cls.Confidence,
		Outcome:         string(answer.Outcome),
		ReplyText:       answer.ReplyText,
		Reason:          escalationReason(cls, answer),
		OccurredAt:      msg.OccurredAt,
	}
	evt, err := eventbus.NewCustomerMessageEscalatedEvent("webhook.messaging."+msg.Channel, p.now(), payload)
	if err != nil {
		return PipelineOutcomeFailed, err
	}
	if err := p.publisher.Publish(ctx, evt); err != nil {
		return PipelineOutcomeFailed, err
	}
	if answer.Outcome == customerservice.FAQOutcomeSuggested {
		return PipelineOutcomeSuggested, nil
	}
	return PipelineOutcomeEscalated, nil
}

// publishReplied publishes the CustomerMessageReplied event.
func (p *MessagingPipeline) publishReplied(ctx context.Context, msg InboundMessage, cls customerservice.EnquiryResult, answer customerservice.FAQResult, providerID string) error {
	payload := eventbus.CustomerMessagePayload{
		Version:           eventbus.CustomerMessagePayloadVersion,
		TenantID:          msg.TenantID,
		MessageID:         msg.MessageID,
		Channel:           msg.Channel,
		ThreadID:          msg.ThreadID,
		BuyerID:           msg.BuyerID,
		Intent:            string(cls.Intent),
		Sentiment:         string(cls.Sentiment),
		Language:          string(cls.Language),
		ConfidenceScore:   cls.Confidence,
		Outcome:           string(answer.Outcome),
		ReplyText:         answer.ReplyText,
		ProviderMessageID: providerID,
		OccurredAt:        msg.OccurredAt,
	}
	evt, err := eventbus.NewCustomerMessageRepliedEvent("webhook.messaging."+msg.Channel, p.now(), payload)
	if err != nil {
		return err
	}
	return p.publisher.Publish(ctx, evt)
}

// statusForMessagingHMACError maps an HMAC verification error
// surfaced by the per-channel handler to an HTTP status. Both
// TikTok + Facebook fail with 401 on invalid signature so callers
// retry with a properly signed delivery; 400 is reserved for
// expired-event poison-pill semantics so retries stop.
func statusForMessagingHMACError(err error) int {
	if errors.Is(err, social.ErrTikTokEventTooOld) {
		return 400
	}
	if errors.Is(err, ErrInvalidMessageHMAC) {
		return 401
	}
	return 500
}

// escalationReason composes the reason label for the operator
// queue so the dashboard can pivot on it.
func escalationReason(cls customerservice.EnquiryResult, answer customerservice.FAQResult) string {
	switch {
	case answer.MatchError != nil:
		return "no_faq_match"
	case cls.FlagForReview:
		return "low_confidence"
	case answer.Outcome == customerservice.FAQOutcomeSuggested:
		return "operator_suggestion"
	default:
		return "policy_escalation"
	}
}

// recordOutcome emits the per-tenant message webhook counter.
func (p *MessagingPipeline) recordOutcome(tenantID, channel string, outcome PipelineOutcome) {
	if p.metrics == nil {
		return
	}
	p.metrics.RecordMessageWebhook(tenantID, channel, string(outcome))
}
