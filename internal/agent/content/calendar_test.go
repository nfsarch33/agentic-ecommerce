// File scope: v3.9.0 EC-5-2 content calendar RED tests.
package content

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

type stubCalendarPublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (p *stubCalendarPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *stubCalendarPublisher) Close() error { return nil }

func (p *stubCalendarPublisher) snapshot() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.events))
	copy(out, p.events)
	return out
}

// stubRateLimiter satisfies CalendarRateLimiter for tests.
type stubRateLimiter struct {
	mu       sync.Mutex
	denyList map[string]bool
	calls    int
}

func (s *stubRateLimiter) Allow(_ context.Context, channel string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.denyList[channel] {
		return false, nil
	}
	return true, nil
}

func newCalendarHarness(t *testing.T, opts ...func(*ContentCalendarConfig)) (*ContentCalendar, *stubCalendarPublisher) {
	t.Helper()
	pub := &stubCalendarPublisher{}
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	cfg := ContentCalendarConfig{
		TenantID:      "tenant-1",
		Publisher:     pub,
		Now:           func() time.Time { return clk },
		MinChannelGap: 30 * time.Minute,
		MaxRetries:    3,
	}
	for _, o := range opts {
		o(&cfg)
	}
	cal, err := NewContentCalendar(nil, cfg)
	if err != nil {
		t.Fatalf("NewContentCalendar: %v", err)
	}
	t.Cleanup(func() { _ = cal.Close(context.Background()) })
	return cal, pub
}

func TestCalendar_SchedulesEntry(t *testing.T) {
	t.Parallel()
	cal, pub := newCalendarHarness(t)
	entry := ContentCalendarEntry{
		ID:          "e-1",
		ScheduledAt: time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC),
		Channel:     "tiktok",
		ContentType: "video",
		PayloadRef:  "video-script-1",
	}
	stored, err := cal.Schedule(context.Background(), entry)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if stored.Status != ContentCalendarStatusScheduled {
		t.Fatalf("expected status=scheduled, got %s", stored.Status)
	}
	events := pub.snapshot()
	if len(events) == 0 || events[0].Type != eventbus.ContentCalendarEntryScheduled {
		t.Fatalf("expected scheduled event, got %v", events)
	}
}

func TestCalendar_PreventsConflictingScheduleSameChannel(t *testing.T) {
	t.Parallel()
	cal, _ := newCalendarHarness(t)
	first := ContentCalendarEntry{
		ID:          "e-1",
		ScheduledAt: time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC),
		Channel:     "tiktok",
		ContentType: "video",
		PayloadRef:  "v1",
	}
	if _, err := cal.Schedule(context.Background(), first); err != nil {
		t.Fatalf("Schedule first: %v", err)
	}
	conflicting := ContentCalendarEntry{
		ID:          "e-2",
		ScheduledAt: first.ScheduledAt.Add(15 * time.Minute), // < MinChannelGap (30m)
		Channel:     "tiktok",
		ContentType: "video",
		PayloadRef:  "v2",
	}
	_, err := cal.Schedule(context.Background(), conflicting)
	if !errors.Is(err, ErrCalendarConflictDetected) {
		t.Fatalf("expected ErrCalendarConflictDetected, got %v", err)
	}
}

func TestCalendar_RetriesFailedEntries(t *testing.T) {
	t.Parallel()
	cal, pub := newCalendarHarness(t)
	entry := ContentCalendarEntry{
		ID:          "e-1",
		ScheduledAt: time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC),
		Channel:     "tiktok",
		ContentType: "video",
		PayloadRef:  "v1",
	}
	if _, err := cal.Schedule(context.Background(), entry); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if _, err := cal.MarkFailed(context.Background(), "e-1", "transient_error"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	res, err := cal.RetryEligible(context.Background())
	if err != nil {
		t.Fatalf("RetryEligible: %v", err)
	}
	if len(res) != 1 || res[0].ID != "e-1" || res[0].AttemptCount != 1 {
		t.Fatalf("expected e-1 retry with attempt=1, got %v", res)
	}
	// Verify status went back to scheduled.
	if res[0].Status != ContentCalendarStatusScheduled {
		t.Fatalf("expected retry status=scheduled, got %s", res[0].Status)
	}
	// Mark failed again over budget -> stays failed.
	for i := 0; i < 5; i++ {
		_, _ = cal.MarkFailed(context.Background(), "e-1", "still_failing")
		_, _ = cal.RetryEligible(context.Background())
	}
	final, ok := cal.Lookup("e-1")
	if !ok {
		t.Fatalf("lookup failed")
	}
	if final.AttemptCount < 3 {
		t.Fatalf("expected attempt_count >= 3, got %d", final.AttemptCount)
	}
	// At least one ContentCalendarEntryFailed event must have been emitted.
	if !calendarHasEvent(pub.snapshot(), eventbus.ContentCalendarEntryFailed) {
		t.Fatalf("expected ContentCalendarEntryFailed event after retry budget exhausted")
	}
}

func TestCalendar_PausesOnRateLimit(t *testing.T) {
	t.Parallel()
	rl := &stubRateLimiter{denyList: map[string]bool{"tiktok": true}}
	cal, _ := newCalendarHarness(t, func(cfg *ContentCalendarConfig) {
		cfg.RateLimiter = rl
	})
	entry := ContentCalendarEntry{
		ID:          "e-1",
		ScheduledAt: time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC),
		Channel:     "tiktok",
		ContentType: "video",
		PayloadRef:  "v1",
	}
	_, err := cal.Schedule(context.Background(), entry)
	if !errors.Is(err, ErrCalendarRateLimited) {
		t.Fatalf("expected ErrCalendarRateLimited, got %v", err)
	}
}

func TestCalendar_ListsUpcomingByChannel(t *testing.T) {
	t.Parallel()
	cal, _ := newCalendarHarness(t)
	scheduledTimes := []time.Time{
		time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 11, 14, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
	}
	for i, ts := range scheduledTimes {
		entry := ContentCalendarEntry{
			ID:          string(rune('a' + i)),
			ScheduledAt: ts,
			Channel:     "tiktok",
			ContentType: "video",
			PayloadRef:  "v",
		}
		if _, err := cal.Schedule(context.Background(), entry); err != nil {
			t.Fatalf("Schedule[%d]: %v", i, err)
		}
	}
	upcoming, err := cal.ListUpcoming(context.Background(), "tiktok", 10)
	if err != nil {
		t.Fatalf("ListUpcoming: %v", err)
	}
	if len(upcoming) != 3 {
		t.Fatalf("expected 3 upcoming, got %d", len(upcoming))
	}
	// Verify sort order: ascending by scheduled_at.
	for i := 1; i < len(upcoming); i++ {
		if upcoming[i].ScheduledAt.Before(upcoming[i-1].ScheduledAt) {
			t.Fatalf("upcoming not ascending: %v", upcoming)
		}
	}
}

func TestCalendar_PublishUpdatesStatusAndEmits(t *testing.T) {
	t.Parallel()
	cal, pub := newCalendarHarness(t)
	entry := ContentCalendarEntry{
		ID:          "e-1",
		ScheduledAt: time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC),
		Channel:     "tiktok",
		ContentType: "video",
		PayloadRef:  "v1",
	}
	if _, err := cal.Schedule(context.Background(), entry); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if _, err := cal.MarkPublished(context.Background(), "e-1"); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}
	got, ok := cal.Lookup("e-1")
	if !ok || got.Status != ContentCalendarStatusPublished {
		t.Fatalf("expected published, got %v ok=%v", got, ok)
	}
	if !calendarHasEvent(pub.snapshot(), eventbus.ContentCalendarEntryPublished) {
		t.Fatalf("expected ContentCalendarEntryPublished event")
	}
}

func TestCalendar_CancelTransitionsState(t *testing.T) {
	t.Parallel()
	cal, _ := newCalendarHarness(t)
	entry := ContentCalendarEntry{
		ID: "e-1", ScheduledAt: time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC),
		Channel: "tiktok", ContentType: "video", PayloadRef: "v1",
	}
	if _, err := cal.Schedule(context.Background(), entry); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if _, err := cal.Cancel(context.Background(), "e-1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got, _ := cal.Lookup("e-1")
	if got.Status != ContentCalendarStatusCancelled {
		t.Fatalf("expected cancelled, got %s", got.Status)
	}
}

func TestCalendar_CancelMissingEntry(t *testing.T) {
	t.Parallel()
	cal, _ := newCalendarHarness(t)
	_, err := cal.Cancel(context.Background(), "missing")
	if !errors.Is(err, ErrCalendarEntryNotFound) {
		t.Fatalf("expected ErrCalendarEntryNotFound, got %v", err)
	}
}

func TestCalendar_ValidateRejectsMissingFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry ContentCalendarEntry
	}{
		{"missing_id", ContentCalendarEntry{ScheduledAt: time.Now(), Channel: "tiktok", ContentType: "video", PayloadRef: "v"}},
		{"missing_scheduled_at", ContentCalendarEntry{ID: "e-1", Channel: "tiktok", ContentType: "video", PayloadRef: "v"}},
		{"missing_channel", ContentCalendarEntry{ID: "e-1", ScheduledAt: time.Now(), ContentType: "video", PayloadRef: "v"}},
		{"missing_content_type", ContentCalendarEntry{ID: "e-1", ScheduledAt: time.Now(), Channel: "tiktok", PayloadRef: "v"}},
		{"missing_payload_ref", ContentCalendarEntry{ID: "e-1", ScheduledAt: time.Now(), Channel: "tiktok", ContentType: "video"}},
	}
	cal, _ := newCalendarHarness(t)
	for _, tc := range cases {
		_, err := cal.Schedule(context.Background(), tc.entry)
		if !errors.Is(err, ErrCalendarInvalidEntry) {
			t.Errorf("%s: expected ErrCalendarInvalidEntry, got %v", tc.name, err)
		}
	}
}

func TestCalendar_RejectsMissingTenantOrPublisher(t *testing.T) {
	t.Parallel()
	if _, err := NewContentCalendar(nil, ContentCalendarConfig{Publisher: &stubCalendarPublisher{}}); !errors.Is(err, ErrCalendarUnconfigured) {
		t.Fatalf("expected ErrCalendarUnconfigured for missing tenant, got %v", err)
	}
	if _, err := NewContentCalendar(nil, ContentCalendarConfig{TenantID: "t"}); !errors.Is(err, ErrCalendarUnconfigured) {
		t.Fatalf("expected ErrCalendarUnconfigured for missing publisher, got %v", err)
	}
}

func TestCalendar_ClosedReturnsError(t *testing.T) {
	t.Parallel()
	cal, _ := newCalendarHarness(t)
	if err := cal.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := cal.Schedule(context.Background(), ContentCalendarEntry{
		ID: "e", ScheduledAt: time.Now(), Channel: "tiktok", ContentType: "video", PayloadRef: "v",
	})
	if !errors.Is(err, ErrCalendarClosed) {
		t.Fatalf("expected ErrCalendarClosed, got %v", err)
	}
}

func calendarHasEvent(events []eventbus.Event, t eventbus.EventType) bool {
	for _, e := range events {
		if e.Type == t {
			return true
		}
	}
	return false
}
