// File scope: v3.9.0 typed event payloads for Epic 6 (pricing
// polish) + Epic 5 (content polish). Every payload follows the
// v3.5.0 + v3.8.0 envelope pattern: typed Validate, typed asMap,
// typed constructor.
//
// Reuse evidence:
//   - Pattern mirrors v3.5.0 EC-6/EC-7 (v350_payloads.go) +
//     v3.8.0 EC-7/EC-9-3 (v380_payloads.go).
//   - Error sentinel + %w-wrap from the package convention.
package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// CompetitorPricePayloadVersion is the schema version shared by
// CompetitorPriceObservedPayload + CompetitorUndercutPayload (the
// shape is identical so a single version constant covers both --
// downstream consumers pivot on EventType, not payload shape).
const CompetitorPricePayloadVersion = 1

// CompetitorPricePayload is the v3.9.0 EC-6-4 envelope. Used by the
// observation event AND by the undercut event so dashboards can
// pivot on a single shape. UndercutPct is zero on the observation
// event and non-zero (positive == competitor cheaper than tenant)
// on the undercut event.
type CompetitorPricePayload struct {
	Version          int       `json:"version"`
	TenantID         string    `json:"tenant_id"`
	SKU              string    `json:"sku"`
	Channel          string    `json:"channel"`
	CompetitorID     string    `json:"competitor_id"`
	CompetitorName   string    `json:"competitor_name,omitempty"`
	CompetitorURL    string    `json:"competitor_url,omitempty"`
	PriceAUDCents    int       `json:"price_aud_cents"`
	OurPriceAUDCents int       `json:"our_price_aud_cents,omitempty"`
	UndercutPct      float64   `json:"undercut_pct,omitempty"`
	ImageFingerprint string    `json:"image_fingerprint,omitempty"`
	ObservedAt       time.Time `json:"observed_at"`
}

// ErrCompetitorPricePayloadInvalid is returned by Validate.
var ErrCompetitorPricePayloadInvalid = errors.New("invalid competitor price payload")

// Validate enforces required fields.
func (p CompetitorPricePayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrCompetitorPricePayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrCompetitorPricePayloadInvalid)
	}
	if p.SKU == "" {
		return fmt.Errorf("%w: sku missing", ErrCompetitorPricePayloadInvalid)
	}
	if p.Channel == "" {
		return fmt.Errorf("%w: channel missing", ErrCompetitorPricePayloadInvalid)
	}
	if p.CompetitorID == "" {
		return fmt.Errorf("%w: competitor_id missing", ErrCompetitorPricePayloadInvalid)
	}
	if p.PriceAUDCents < 0 {
		return fmt.Errorf("%w: price_aud_cents cannot be negative", ErrCompetitorPricePayloadInvalid)
	}
	return nil
}

func (p CompetitorPricePayload) asMap() map[string]any {
	return map[string]any{
		"version":             p.Version,
		"tenant_id":           p.TenantID,
		"sku":                 p.SKU,
		"channel":             p.Channel,
		"competitor_id":       p.CompetitorID,
		"competitor_name":     p.CompetitorName,
		"competitor_url":      p.CompetitorURL,
		"price_aud_cents":     p.PriceAUDCents,
		"our_price_aud_cents": p.OurPriceAUDCents,
		"undercut_pct":        p.UndercutPct,
		"image_fingerprint":   p.ImageFingerprint,
		"observed_at":         p.ObservedAt.UTC().Format(time.RFC3339Nano),
	}
}

// NewCompetitorPriceObservedEvent fires when the scraper records a
// new competitor observation (delta below the undercut threshold).
func NewCompetitorPriceObservedEvent(source string, occurredAt time.Time, payload CompetitorPricePayload) (Event, error) {
	return newCompetitorPriceEvent(CompetitorPriceObserved, source, occurredAt, payload)
}

// NewCompetitorUndercutEvent fires when the observed competitor
// price is below the tenant's current price by more than the
// configurable threshold. EC-6-3 dynamic pricing subscribes.
func NewCompetitorUndercutEvent(source string, occurredAt time.Time, payload CompetitorPricePayload) (Event, error) {
	return newCompetitorPriceEvent(CompetitorUndercut, source, occurredAt, payload)
}

func newCompetitorPriceEvent(kind EventType, source string, occurredAt time.Time, payload CompetitorPricePayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = CompetitorPricePayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "agent.pricing.competitor_scraper"
	}
	return Event{
		Type:      kind,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// ContentCalendarPayloadVersion is the schema version shared by the
// scheduled / publishing / published / failed events.
const ContentCalendarPayloadVersion = 1

// ContentCalendarPayload is the v3.9.0 EC-5-2 envelope.
type ContentCalendarPayload struct {
	Version      int       `json:"version"`
	TenantID     string    `json:"tenant_id"`
	EntryID      string    `json:"entry_id"`
	Channel      string    `json:"channel"`
	ContentType  string    `json:"content_type"`
	PayloadRef   string    `json:"payload_ref"`
	Status       string    `json:"status"`
	ScheduledAt  time.Time `json:"scheduled_at"`
	OccurredAt   time.Time `json:"occurred_at"`
	AttemptCount int       `json:"attempt_count,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
}

// ErrContentCalendarPayloadInvalid is returned by Validate.
var ErrContentCalendarPayloadInvalid = errors.New("invalid content calendar payload")

// Validate enforces required fields.
func (p ContentCalendarPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrContentCalendarPayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrContentCalendarPayloadInvalid)
	}
	if p.EntryID == "" {
		return fmt.Errorf("%w: entry_id missing", ErrContentCalendarPayloadInvalid)
	}
	if p.Channel == "" {
		return fmt.Errorf("%w: channel missing", ErrContentCalendarPayloadInvalid)
	}
	if p.Status == "" {
		return fmt.Errorf("%w: status missing", ErrContentCalendarPayloadInvalid)
	}
	return nil
}

func (p ContentCalendarPayload) asMap() map[string]any {
	return map[string]any{
		"version":       p.Version,
		"tenant_id":     p.TenantID,
		"entry_id":      p.EntryID,
		"channel":       p.Channel,
		"content_type":  p.ContentType,
		"payload_ref":   p.PayloadRef,
		"status":        p.Status,
		"scheduled_at":  p.ScheduledAt.UTC().Format(time.RFC3339Nano),
		"occurred_at":   p.OccurredAt.UTC().Format(time.RFC3339Nano),
		"attempt_count": p.AttemptCount,
		"last_error":    p.LastError,
	}
}

// NewContentCalendarEntryScheduledEvent fires when the calendar
// accepts a new entry.
func NewContentCalendarEntryScheduledEvent(source string, occurredAt time.Time, payload ContentCalendarPayload) (Event, error) {
	return newContentCalendarEvent(ContentCalendarEntryScheduled, source, occurredAt, payload)
}

// NewContentCalendarEntryPublishedEvent fires when the n8n scheduler
// reports a successful publish.
func NewContentCalendarEntryPublishedEvent(source string, occurredAt time.Time, payload ContentCalendarPayload) (Event, error) {
	return newContentCalendarEvent(ContentCalendarEntryPublished, source, occurredAt, payload)
}

// NewContentCalendarEntryFailedEvent fires when publishing exceeded
// the retry budget.
func NewContentCalendarEntryFailedEvent(source string, occurredAt time.Time, payload ContentCalendarPayload) (Event, error) {
	return newContentCalendarEvent(ContentCalendarEntryFailed, source, occurredAt, payload)
}

func newContentCalendarEvent(kind EventType, source string, occurredAt time.Time, payload ContentCalendarPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = ContentCalendarPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "agent.content.calendar"
	}
	return Event{
		Type:      kind,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// ContentEMAUpdatedPayloadVersion is the schema version of
// ContentEMAUpdatedPayload.
const ContentEMAUpdatedPayloadVersion = 1

// ContentEMAUpdatedPayload is the v3.9.0 EC-5-5 envelope. Emitted
// by the EMA learner on each engagement-metric ingestion.
type ContentEMAUpdatedPayload struct {
	Version             int       `json:"version"`
	TenantID            string    `json:"tenant_id"`
	ContentID           string    `json:"content_id"`
	Channel             string    `json:"channel"`
	ContentType         string    `json:"content_type"`
	EMAScore            float64   `json:"ema_score"`
	LastEngagementScore float64   `json:"last_engagement_score"`
	SampleCount         int       `json:"sample_count"`
	Alpha               float64   `json:"alpha"`
	OccurredAt          time.Time `json:"occurred_at"`
}

// ErrContentEMAUpdatedInvalid is returned by Validate.
var ErrContentEMAUpdatedInvalid = errors.New("invalid content ema updated payload")

// Validate enforces required fields.
func (p ContentEMAUpdatedPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrContentEMAUpdatedInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrContentEMAUpdatedInvalid)
	}
	if p.ContentID == "" {
		return fmt.Errorf("%w: content_id missing", ErrContentEMAUpdatedInvalid)
	}
	if p.Channel == "" {
		return fmt.Errorf("%w: channel missing", ErrContentEMAUpdatedInvalid)
	}
	if p.Alpha <= 0 || p.Alpha > 1 {
		return fmt.Errorf("%w: alpha must be in (0,1]", ErrContentEMAUpdatedInvalid)
	}
	return nil
}

func (p ContentEMAUpdatedPayload) asMap() map[string]any {
	return map[string]any{
		"version":               p.Version,
		"tenant_id":             p.TenantID,
		"content_id":            p.ContentID,
		"channel":               p.Channel,
		"content_type":          p.ContentType,
		"ema_score":             p.EMAScore,
		"last_engagement_score": p.LastEngagementScore,
		"sample_count":          p.SampleCount,
		"alpha":                 p.Alpha,
		"occurred_at":           p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

// NewContentEMAUpdatedEvent is the canonical constructor.
func NewContentEMAUpdatedEvent(source string, occurredAt time.Time, payload ContentEMAUpdatedPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = ContentEMAUpdatedPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "agent.content.performance_loop"
	}
	return Event{
		Type:      ContentEMAUpdated,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
