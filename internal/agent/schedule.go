package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrScheduleNotFound = errors.New("agent schedule not found")
	ErrInvalidSchedule  = errors.New("invalid agent schedule")
)

type ScheduleDefinition struct {
	ID       string         `json:"id"`
	AgentID  string         `json:"agent_id"`
	Cron     string         `json:"cron,omitempty"`
	Interval time.Duration  `json:"interval,omitempty"`
	Priority int            `json:"priority"`
	Payload  map[string]any `json:"payload"`
}

type Schedule struct {
	ID        string         `json:"id"`
	AgentID   string         `json:"agent_id"`
	Cron      string         `json:"cron,omitempty"`
	Interval  time.Duration  `json:"interval,omitempty"`
	Enabled   bool           `json:"enabled"`
	Priority  int            `json:"priority"`
	Payload   map[string]any `json:"payload"`
	NextRunAt *time.Time     `json:"next_run_at,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type ScheduleManager struct {
	mu        sync.RWMutex
	clock     Clock
	schedules map[string]Schedule
}

func ValidateScheduleDefinitions(definitions []ScheduleDefinition) error {
	seen := make(map[string]struct{}, len(definitions))
	for _, def := range definitions {
		id := strings.TrimSpace(def.ID)
		if id == "" {
			return fmt.Errorf("%w: id is required", ErrInvalidSchedule)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: duplicate id %q", ErrInvalidSchedule, id)
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(def.AgentID) == "" {
			return fmt.Errorf("%w: agent_id is required for %q", ErrInvalidSchedule, id)
		}
		if strings.TrimSpace(def.Cron) == "" && def.Interval <= 0 {
			return fmt.Errorf("%w: cadence is required for %q", ErrInvalidSchedule, id)
		}
		if def.Priority < 0 {
			return fmt.Errorf("%w: priority must be >= 0 for %q", ErrInvalidSchedule, id)
		}
	}
	return nil
}

func NewScheduleManager(definitions []ScheduleDefinition, clock Clock) *ScheduleManager {
	if clock == nil {
		clock = realClock{}
	}
	now := clock.Now()
	manager := &ScheduleManager{clock: clock, schedules: make(map[string]Schedule, len(definitions))}
	for _, def := range definitions {
		schedule := Schedule{
			ID:        def.ID,
			AgentID:   def.AgentID,
			Cron:      def.Cron,
			Interval:  def.Interval,
			Priority:  def.Priority,
			Payload:   cloneMap(def.Payload),
			CreatedAt: now,
			UpdatedAt: now,
		}
		schedule.NextRunAt = nextRun(now, def.Interval)
		manager.schedules[schedule.ID] = schedule
	}
	return manager
}

func (m *ScheduleManager) List(_ context.Context) []Schedule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Schedule, 0, len(m.schedules))
	for _, schedule := range m.schedules {
		out = append(out, cloneSchedule(schedule))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (m *ScheduleManager) Enable(_ context.Context, id string) (Schedule, error) {
	return m.setEnabled(id, true)
}

func (m *ScheduleManager) Disable(_ context.Context, id string) (Schedule, error) {
	return m.setEnabled(id, false)
}

func (m *ScheduleManager) setEnabled(id string, enabled bool) (Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	schedule, ok := m.schedules[id]
	if !ok {
		return Schedule{}, ErrScheduleNotFound
	}
	schedule.Enabled = enabled
	schedule.UpdatedAt = m.clock.Now()
	schedule.NextRunAt = nextRun(schedule.UpdatedAt, schedule.Interval)
	m.schedules[id] = schedule
	return cloneSchedule(schedule), nil
}

func nextRun(now time.Time, interval time.Duration) *time.Time {
	if interval <= 0 {
		return nil
	}
	next := now.Add(interval)
	return &next
}

func cloneSchedule(schedule Schedule) Schedule {
	schedule.Payload = cloneMap(schedule.Payload)
	if schedule.NextRunAt != nil {
		next := *schedule.NextRunAt
		schedule.NextRunAt = &next
	}
	return schedule
}
