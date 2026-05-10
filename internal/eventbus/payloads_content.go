// Content domain payloads: content calendar and engagement EMA.
//
// Consolidated from v390_payloads.go in v5.4.0.
package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// --- Content calendar (v3.9.0 EC-5-2) ---

const ContentCalendarPayloadVersion = 1

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

var ErrContentCalendarPayloadInvalid = errors.New("invalid content calendar payload")

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

func NewContentCalendarEntryScheduledEvent(source string, occurredAt time.Time, payload ContentCalendarPayload) (Event, error) {
	return newContentCalendarEvent(ContentCalendarEntryScheduled, source, occurredAt, payload)
}

func NewContentCalendarEntryPublishedEvent(source string, occurredAt time.Time, payload ContentCalendarPayload) (Event, error) {
	return newContentCalendarEvent(ContentCalendarEntryPublished, source, occurredAt, payload)
}

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

// --- Content EMA updated (v3.9.0 EC-5-5) ---

const ContentEMAUpdatedPayloadVersion = 1

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

var ErrContentEMAUpdatedInvalid = errors.New("invalid content ema updated payload")

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
