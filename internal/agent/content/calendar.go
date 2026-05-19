// File scope: v3.9.0 EC-5-2 content calendar agent.
//
// The calendar tracks scheduled posts across channels (TikTok
// video, RedNote post, FB post, Instagram reel). Each entry is
// keyed by (tenant_id, id) and carries:
//   - scheduled_at + channel + content_type + payload_ref
//   - status: scheduled | publishing | published | failed | cancelled
//   - attempt_count + last_error for retry accounting
//
// Cross-channel coordination ensures a configurable minimum gap
// between entries on the same channel (Plan EC-5-2 acceptance
// criterion). The companion n8n workflow file
// `workflows/v390-content-calendar.json` triggers the actual
// publish call by hitting the backend API at scheduled_at.
//
// Reuse evidence:
//   - eventbus.Publisher contract from v3.3.0 EC-3-3.
//   - The rate-limit gate is supplied as a thin port; production
//     wires v3.7.0 EC-10-3 ratelimit.RateLimiter behind it. Tests
//     pass an in-memory stub so the calendar package itself stays
//     decoupled from the uiauto package.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4 -- 16-sprint streak; v3.9.0 sprint 16 target):
//   - Schedule (envelope -> validate -> conflict -> ratelimit -> store -> emit)
//   - validateEntry (typed-error checks; pure)
//   - detectConflict (per-channel gap check; pure)
//   - storeEntry (synchronous in-memory write)
//   - emitScheduled (eventbus dispatch)
//
// Each helper stays under cyclomatic 6.
package content

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// ContentCalendarStatus is the typed lifecycle state.
type ContentCalendarStatus string

// Calendar status enum values.
const (
	ContentCalendarStatusScheduled  ContentCalendarStatus = "scheduled"
	ContentCalendarStatusPublishing ContentCalendarStatus = "publishing"
	ContentCalendarStatusPublished  ContentCalendarStatus = "published"
	ContentCalendarStatusFailed     ContentCalendarStatus = "failed"
	ContentCalendarStatusCancelled  ContentCalendarStatus = "cancelled"
)

// DefaultCalendarMinChannelGap is the minimum gap between two
// scheduled posts on the SAME channel. Matches the EC-10-3
// stealth-pacing default tier.
const DefaultCalendarMinChannelGap = 30 * time.Minute

// DefaultCalendarMaxRetries caps the per-entry retry budget.
const DefaultCalendarMaxRetries = 3

// EC-5-2 typed sentinels.
var (
	// ErrCalendarUnconfigured is returned when a required dependency
	// is missing.
	ErrCalendarUnconfigured = errors.New("content_calendar: unconfigured")

	// ErrCalendarClosed is returned after Close.
	ErrCalendarClosed = errors.New("content_calendar: closed")

	// ErrCalendarInvalidEntry is returned when the entry violates the
	// validation contract.
	ErrCalendarInvalidEntry = errors.New("content_calendar: invalid entry")

	// ErrCalendarConflictDetected is returned when a new entry
	// would violate the per-channel min-gap.
	ErrCalendarConflictDetected = errors.New("content_calendar: conflict detected")

	// ErrCalendarRateLimited is returned when the supplied
	// CalendarRateLimiter denied scheduling on the channel.
	ErrCalendarRateLimited = errors.New("content_calendar: rate limited")

	// ErrCalendarEntryNotFound is returned when MarkPublished /
	// MarkFailed reference a missing id.
	ErrCalendarEntryNotFound = errors.New("content_calendar: entry not found")
)

// ContentCalendarEntry is one scheduled item.
type ContentCalendarEntry struct {
	ID           string
	ScheduledAt  time.Time
	Channel      string
	ContentType  string
	PayloadRef   string
	Status       ContentCalendarStatus
	AttemptCount int
	LastError    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CalendarRateLimiter is the small port the calendar consumes to
// honour platform rate limits at schedule time. Production wires
// internal/uiauto/ratelimit.RateLimiter via a thin adapter.
type CalendarRateLimiter interface {
	Allow(ctx context.Context, channel string) (bool, error)
}

// CalendarMetrics is the small port the calendar emits counters
// + duration through.
type CalendarMetrics interface {
	RecordCalendarEntry(tenantID, channel, status string)
	ObserveCalendarPublishingDuration(tenantID, channel string, durationSec float64)
}

// CalendarKPISample is the v3.9.0 EvoMap KPI sample.
type CalendarKPISample struct {
	TenantID     string
	Channel      string
	Status       string
	PublishingMs int64
}

// CalendarKPIHook is the optional EvoMap emission hook.
type CalendarKPIHook func(CalendarKPISample)

// ContentCalendarConfig wires the calendar.
type ContentCalendarConfig struct {
	TenantID      string
	Publisher     eventbus.Publisher
	RateLimiter   CalendarRateLimiter
	Metrics       CalendarMetrics
	KPIHook       CalendarKPIHook
	MinChannelGap time.Duration
	MaxRetries    int
	Now           func() time.Time
}

// ContentCalendar is the v3.9.0 EC-5-2 calendar.
type ContentCalendar struct {
	cfg    ContentCalendarConfig
	logger *slog.Logger

	mu      sync.Mutex
	entries map[string]ContentCalendarEntry
	closed  bool
}

// NewContentCalendar constructs a calendar.
func NewContentCalendar(logger *slog.Logger, cfg ContentCalendarConfig) (*ContentCalendar, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := validateCalendarConfig(cfg); err != nil {
		return nil, err
	}
	applyCalendarDefaults(&cfg)
	return &ContentCalendar{
		cfg:     cfg,
		logger:  logger,
		entries: map[string]ContentCalendarEntry{},
	}, nil
}

func validateCalendarConfig(cfg ContentCalendarConfig) error {
	if strings.TrimSpace(cfg.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrCalendarUnconfigured)
	}
	if cfg.Publisher == nil {
		return fmt.Errorf("%w: Publisher required", ErrCalendarUnconfigured)
	}
	return nil
}

func applyCalendarDefaults(cfg *ContentCalendarConfig) {
	if cfg.MinChannelGap <= 0 {
		cfg.MinChannelGap = DefaultCalendarMinChannelGap
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultCalendarMaxRetries
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
}

// Close marks the calendar closed. Implements lifecycle.Closer.
func (c *ContentCalendar) Close(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// Schedule accepts a new entry. Cyclomatic 5.
func (c *ContentCalendar) Schedule(ctx context.Context, entry ContentCalendarEntry) (ContentCalendarEntry, error) {
	if err := c.guard(); err != nil {
		return ContentCalendarEntry{}, err
	}
	if err := validateCalendarEntry(entry); err != nil {
		return ContentCalendarEntry{}, err
	}
	if err := c.checkRateLimit(ctx, entry.Channel); err != nil {
		return ContentCalendarEntry{}, err
	}
	if err := c.detectConflict(entry); err != nil {
		return ContentCalendarEntry{}, err
	}
	now := c.cfg.Now()
	entry.Status = ContentCalendarStatusScheduled
	entry.AttemptCount = 0
	entry.CreatedAt = now
	entry.UpdatedAt = now
	c.storeEntry(entry)
	c.emitLifecycle(ctx, entry, eventbus.ContentCalendarEntryScheduled)
	c.recordMetric(entry.Channel, string(entry.Status))
	return entry, nil
}

// MarkPublished transitions an entry to published.
func (c *ContentCalendar) MarkPublished(ctx context.Context, id string) (ContentCalendarEntry, error) {
	return c.transition(ctx, id, ContentCalendarStatusPublished, "", eventbus.ContentCalendarEntryPublished)
}

// MarkFailed bumps the attempt count and transitions to failed.
// If attempt_count <= MaxRetries, the entry is scheduled for retry.
func (c *ContentCalendar) MarkFailed(ctx context.Context, id, reason string) (ContentCalendarEntry, error) {
	if err := c.guard(); err != nil {
		return ContentCalendarEntry{}, err
	}
	c.mu.Lock()
	entry, ok := c.entries[id]
	if !ok {
		c.mu.Unlock()
		return ContentCalendarEntry{}, fmt.Errorf("%w: id=%s", ErrCalendarEntryNotFound, id)
	}
	entry.AttemptCount++
	entry.LastError = reason
	entry.Status = ContentCalendarStatusFailed
	entry.UpdatedAt = c.cfg.Now()
	c.entries[id] = entry
	c.mu.Unlock()
	if entry.AttemptCount > c.cfg.MaxRetries {
		c.emitLifecycle(ctx, entry, eventbus.ContentCalendarEntryFailed)
	}
	c.recordMetric(entry.Channel, string(entry.Status))
	return entry, nil
}

// RetryEligible returns failed entries whose attempt_count <
// MaxRetries; each returned entry is transitioned back to scheduled.
func (c *ContentCalendar) RetryEligible(ctx context.Context) ([]ContentCalendarEntry, error) {
	if err := c.guard(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ContentCalendarEntry, 0)
	for id, entry := range c.entries {
		if entry.Status != ContentCalendarStatusFailed {
			continue
		}
		if entry.AttemptCount > c.cfg.MaxRetries {
			continue
		}
		entry.Status = ContentCalendarStatusScheduled
		entry.UpdatedAt = c.cfg.Now()
		c.entries[id] = entry
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ScheduledAt.Before(out[j].ScheduledAt) })
	return out, nil
}

// ListUpcoming returns scheduled entries on the channel, sorted
// ascending by scheduled_at.
func (c *ContentCalendar) ListUpcoming(_ context.Context, channel string, limit int) ([]ContentCalendarEntry, error) {
	if err := c.guard(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ContentCalendarEntry, 0)
	for _, e := range c.entries {
		if e.Status != ContentCalendarStatusScheduled {
			continue
		}
		if channel != "" && e.Channel != channel {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ScheduledAt.Before(out[j].ScheduledAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Lookup returns a snapshot of the entry. The boolean is false when
// no entry exists for that id.
func (c *ContentCalendar) Lookup(id string) (ContentCalendarEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	return e, ok
}

// Cancel transitions an entry to cancelled.
func (c *ContentCalendar) Cancel(ctx context.Context, id string) (ContentCalendarEntry, error) {
	return c.transition(ctx, id, ContentCalendarStatusCancelled, "", eventbus.ContentCalendarEntryFailed)
}

// transition is the shared state-machine helper. Cyclomatic 4.
func (c *ContentCalendar) transition(ctx context.Context, id string, status ContentCalendarStatus, reason string, evt eventbus.EventType) (ContentCalendarEntry, error) {
	if err := c.guard(); err != nil {
		return ContentCalendarEntry{}, err
	}
	c.mu.Lock()
	entry, ok := c.entries[id]
	if !ok {
		c.mu.Unlock()
		return ContentCalendarEntry{}, fmt.Errorf("%w: id=%s", ErrCalendarEntryNotFound, id)
	}
	entry.Status = status
	if reason != "" {
		entry.LastError = reason
	}
	entry.UpdatedAt = c.cfg.Now()
	c.entries[id] = entry
	c.mu.Unlock()
	c.emitLifecycle(ctx, entry, evt)
	c.recordMetric(entry.Channel, string(entry.Status))
	return entry, nil
}

func (c *ContentCalendar) detectConflict(incoming ContentCalendarEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, existing := range c.entries {
		if existing.Channel != incoming.Channel {
			continue
		}
		if existing.Status == ContentCalendarStatusCancelled || existing.Status == ContentCalendarStatusFailed {
			continue
		}
		gap := existing.ScheduledAt.Sub(incoming.ScheduledAt)
		if gap < 0 {
			gap = -gap
		}
		if gap < c.cfg.MinChannelGap {
			return fmt.Errorf("%w: channel=%s gap=%s < %s (existing=%s incoming=%s)",
				ErrCalendarConflictDetected, incoming.Channel, gap, c.cfg.MinChannelGap, existing.ID, incoming.ID)
		}
	}
	return nil
}

func (c *ContentCalendar) checkRateLimit(ctx context.Context, channel string) error {
	if c.cfg.RateLimiter == nil {
		return nil
	}
	allowed, err := c.cfg.RateLimiter.Allow(ctx, channel)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCalendarRateLimited, err)
	}
	if !allowed {
		return fmt.Errorf("%w: channel=%s", ErrCalendarRateLimited, channel)
	}
	return nil
}

func (c *ContentCalendar) storeEntry(entry ContentCalendarEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[entry.ID] = entry
}

func (c *ContentCalendar) emitLifecycle(ctx context.Context, entry ContentCalendarEntry, evtType eventbus.EventType) {
	payload := eventbus.ContentCalendarPayload{
		Version:      eventbus.ContentCalendarPayloadVersion,
		TenantID:     c.cfg.TenantID,
		EntryID:      entry.ID,
		Channel:      entry.Channel,
		ContentType:  entry.ContentType,
		PayloadRef:   entry.PayloadRef,
		Status:       string(entry.Status),
		ScheduledAt:  entry.ScheduledAt,
		OccurredAt:   c.cfg.Now(),
		AttemptCount: entry.AttemptCount,
		LastError:    entry.LastError,
	}
	var (
		evt eventbus.Event
		err error
	)
	switch evtType {
	case eventbus.ContentCalendarEntryScheduled:
		evt, err = eventbus.NewContentCalendarEntryScheduledEvent("agent.content.calendar", payload.OccurredAt, payload)
	case eventbus.ContentCalendarEntryPublished:
		evt, err = eventbus.NewContentCalendarEntryPublishedEvent("agent.content.calendar", payload.OccurredAt, payload)
	default:
		evt, err = eventbus.NewContentCalendarEntryFailedEvent("agent.content.calendar", payload.OccurredAt, payload)
	}
	if err != nil {
		c.logger.Error("content_calendar.event_invalid", "tenant_id", c.cfg.TenantID, "id", entry.ID, "error", err)
		return
	}
	if err := c.cfg.Publisher.Publish(ctx, evt); err != nil {
		c.logger.Error("content_calendar.publish_failed", "tenant_id", c.cfg.TenantID, "error", err)
	}
}

func (c *ContentCalendar) recordMetric(channel, status string) {
	if c.cfg.Metrics == nil {
		return
	}
	c.cfg.Metrics.RecordCalendarEntry(c.cfg.TenantID, channel, status)
}

func (c *ContentCalendar) guard() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrCalendarClosed
	}
	return nil
}

func validateCalendarEntry(entry ContentCalendarEntry) error {
	if strings.TrimSpace(entry.ID) == "" {
		return fmt.Errorf("%w: id required", ErrCalendarInvalidEntry)
	}
	if entry.ScheduledAt.IsZero() {
		return fmt.Errorf("%w: scheduled_at required", ErrCalendarInvalidEntry)
	}
	if strings.TrimSpace(entry.Channel) == "" {
		return fmt.Errorf("%w: channel required", ErrCalendarInvalidEntry)
	}
	if strings.TrimSpace(entry.ContentType) == "" {
		return fmt.Errorf("%w: content_type required", ErrCalendarInvalidEntry)
	}
	if strings.TrimSpace(entry.PayloadRef) == "" {
		return fmt.Errorf("%w: payload_ref required", ErrCalendarInvalidEntry)
	}
	return nil
}
