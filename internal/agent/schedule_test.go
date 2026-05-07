package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestScheduleManagerListEnableDisable(t *testing.T) {
	t.Parallel()

	manager := NewScheduleManager([]ScheduleDefinition{
		{
			ID:       "pricing-hourly",
			AgentID:  "pricing",
			Interval: time.Hour,
			Priority: 2,
			Payload:  map[string]any{"strategy": "competition_based"},
		},
	}, fixedClock{now: time.Date(2026, 5, 8, 1, 0, 0, 0, time.UTC)})

	schedules := manager.List(context.Background())
	if len(schedules) != 1 {
		t.Fatalf("schedules len = %d, want 1", len(schedules))
	}
	if schedules[0].Enabled {
		t.Fatalf("default schedule should start disabled: %+v", schedules[0])
	}
	if schedules[0].NextRunAt == nil || !schedules[0].NextRunAt.Equal(time.Date(2026, 5, 8, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("next run = %v, want one interval from fixed clock", schedules[0].NextRunAt)
	}

	enabled, err := manager.Enable(context.Background(), "pricing-hourly")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !enabled.Enabled || enabled.UpdatedAt.IsZero() {
		t.Fatalf("enabled schedule = %+v", enabled)
	}

	disabled, err := manager.Disable(context.Background(), "pricing-hourly")
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("disabled schedule = %+v", disabled)
	}
}

func TestScheduleManagerRejectsMissingSchedule(t *testing.T) {
	t.Parallel()

	manager := NewScheduleManager(nil, fixedClock{now: time.Date(2026, 5, 8, 1, 0, 0, 0, time.UTC)})
	if _, err := manager.Enable(context.Background(), "missing"); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("enable missing err = %v, want ErrScheduleNotFound", err)
	}
	if _, err := manager.Disable(context.Background(), "missing"); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("disable missing err = %v, want ErrScheduleNotFound", err)
	}
}

func TestValidateScheduleDefinitionsRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		definitions []ScheduleDefinition
	}{
		{
			name: "missing id",
			definitions: []ScheduleDefinition{{
				AgentID:  "pricing",
				Interval: time.Hour,
			}},
		},
		{
			name: "missing agent",
			definitions: []ScheduleDefinition{{
				ID:       "pricing-hourly",
				Interval: time.Hour,
			}},
		},
		{
			name: "missing cadence",
			definitions: []ScheduleDefinition{{
				ID:      "pricing-hourly",
				AgentID: "pricing",
			}},
		},
		{
			name: "duplicate id",
			definitions: []ScheduleDefinition{
				{ID: "pricing-hourly", AgentID: "pricing", Interval: time.Hour},
				{ID: "pricing-hourly", AgentID: "pricing", Interval: time.Hour},
			},
		},
		{
			name: "negative priority",
			definitions: []ScheduleDefinition{{
				ID:       "pricing-hourly",
				AgentID:  "pricing",
				Interval: time.Hour,
				Priority: -1,
			}},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := ValidateScheduleDefinitions(tt.definitions); err == nil {
				t.Fatalf("ValidateScheduleDefinitions(%s) returned nil error", tt.name)
			}
		})
	}
}
