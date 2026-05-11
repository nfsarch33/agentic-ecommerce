// File scope: v6.1.0 coverage backfill -- payloads_content was at 0%
// after v6.0.0 because the v5.4.0 consolidation moved the calendar /
// EMA payloads here without bringing the original tests along.
package eventbus

import (
	"errors"
	"testing"
	"time"
)

func validCalendarPayload() ContentCalendarPayload {
	return ContentCalendarPayload{
		Version:     ContentCalendarPayloadVersion,
		TenantID:    "tenant-c",
		EntryID:     "entry-1",
		Channel:     "tiktok",
		ContentType: "video",
		PayloadRef:  "asset-1",
		Status:      "scheduled",
		ScheduledAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		OccurredAt:  time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
}

func TestContentCalendarPayloadValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*ContentCalendarPayload)
		wantErr bool
	}{
		{name: "ok", mutate: func(*ContentCalendarPayload) {}},
		{name: "version zero", mutate: func(p *ContentCalendarPayload) { p.Version = 0 }, wantErr: true},
		{name: "missing tenant", mutate: func(p *ContentCalendarPayload) { p.TenantID = "" }, wantErr: true},
		{name: "missing entry", mutate: func(p *ContentCalendarPayload) { p.EntryID = "" }, wantErr: true},
		{name: "missing channel", mutate: func(p *ContentCalendarPayload) { p.Channel = "" }, wantErr: true},
		{name: "missing status", mutate: func(p *ContentCalendarPayload) { p.Status = "" }, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validCalendarPayload()
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && !errors.Is(err, ErrContentCalendarPayloadInvalid) {
				t.Fatalf("err = %v, want ErrContentCalendarPayloadInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestContentCalendarConstructorsDefault(t *testing.T) {
	t.Parallel()
	p := validCalendarPayload()
	p.Version = 0 // exercise default-fill
	for _, name := range []string{"scheduled", "published", "failed"} {
		var (
			ev  Event
			err error
		)
		switch name {
		case "scheduled":
			ev, err = NewContentCalendarEntryScheduledEvent("", time.Time{}, p)
		case "published":
			ev, err = NewContentCalendarEntryPublishedEvent("", time.Time{}, p)
		case "failed":
			ev, err = NewContentCalendarEntryFailedEvent("", time.Time{}, p)
		}
		if err != nil {
			t.Fatalf("%s constructor: %v", name, err)
		}
		if ev.TenantID != p.TenantID {
			t.Fatalf("%s: TenantID = %q, want %q", name, ev.TenantID, p.TenantID)
		}
		if ev.Source == "" {
			t.Fatalf("%s: default source not set", name)
		}
		if ev.Timestamp.IsZero() {
			t.Fatalf("%s: default timestamp not set", name)
		}
	}
}

func TestContentCalendarConstructorRejectsInvalidPayload(t *testing.T) {
	t.Parallel()
	p := validCalendarPayload()
	p.TenantID = ""
	if _, err := NewContentCalendarEntryScheduledEvent("src", time.Now(), p); err == nil {
		t.Fatal("expected error from invalid payload")
	}
}

func validEMAPayload() ContentEMAUpdatedPayload {
	return ContentEMAUpdatedPayload{
		Version:             ContentEMAUpdatedPayloadVersion,
		TenantID:            "tenant-e",
		ContentID:           "content-1",
		Channel:             "tiktok",
		ContentType:         "video",
		EMAScore:            0.42,
		LastEngagementScore: 0.55,
		SampleCount:         12,
		Alpha:               0.3,
		OccurredAt:          time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
}

func TestContentEMAUpdatedPayloadValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*ContentEMAUpdatedPayload)
		wantErr bool
	}{
		{name: "ok", mutate: func(*ContentEMAUpdatedPayload) {}},
		{name: "version zero", mutate: func(p *ContentEMAUpdatedPayload) { p.Version = 0 }, wantErr: true},
		{name: "missing tenant", mutate: func(p *ContentEMAUpdatedPayload) { p.TenantID = "" }, wantErr: true},
		{name: "missing content", mutate: func(p *ContentEMAUpdatedPayload) { p.ContentID = "" }, wantErr: true},
		{name: "missing channel", mutate: func(p *ContentEMAUpdatedPayload) { p.Channel = "" }, wantErr: true},
		{name: "alpha zero", mutate: func(p *ContentEMAUpdatedPayload) { p.Alpha = 0 }, wantErr: true},
		{name: "alpha above one", mutate: func(p *ContentEMAUpdatedPayload) { p.Alpha = 1.5 }, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validEMAPayload()
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && !errors.Is(err, ErrContentEMAUpdatedInvalid) {
				t.Fatalf("err = %v, want ErrContentEMAUpdatedInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestNewContentEMAUpdatedEventDefaults(t *testing.T) {
	t.Parallel()
	p := validEMAPayload()
	p.Version = 0
	ev, err := NewContentEMAUpdatedEvent("", time.Time{}, p)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if ev.Source != "agent.content.performance_loop" {
		t.Fatalf("Source = %q, want default", ev.Source)
	}
	if ev.Timestamp.IsZero() {
		t.Fatal("Timestamp default not applied")
	}
	if ev.Type != ContentEMAUpdated {
		t.Fatalf("Type = %v, want ContentEMAUpdated", ev.Type)
	}
}

func TestNewContentEMAUpdatedEventRejectsInvalid(t *testing.T) {
	t.Parallel()
	p := validEMAPayload()
	p.Alpha = 0
	if _, err := NewContentEMAUpdatedEvent("src", time.Now(), p); err == nil {
		t.Fatal("expected validation error")
	}
}
