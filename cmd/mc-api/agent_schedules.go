package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	orchestrator "github.com/nfsarch33/helixon-ec/internal/agent"
)

type agentSchedule struct {
	ID              string         `json:"id"`
	AgentID         string         `json:"agent_id"`
	Cron            string         `json:"cron,omitempty"`
	IntervalSeconds int64          `json:"interval_seconds,omitempty"`
	Enabled         bool           `json:"enabled"`
	Priority        int            `json:"priority"`
	Payload         map[string]any `json:"payload"`
	NextRunAt       *time.Time     `json:"next_run_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type agentSchedulesResponse struct {
	Schedules []agentSchedule `json:"schedules"`
}

type agentScheduleResponse struct {
	Schedule agentSchedule `json:"schedule"`
}

func defaultAgentScheduleManager() *orchestrator.ScheduleManager {
	return orchestrator.NewScheduleManager([]orchestrator.ScheduleDefinition{
		{
			ID:       "pricing-competition-hourly",
			AgentID:  "pricing",
			Interval: time.Hour,
			Priority: 2,
			Payload: map[string]any{
				"strategy":            "competition_based",
				"sku":                 "RB-SET",
				"cost_cents":          1800,
				"shipping_cents":      250,
				"current_price_cents": 4995,
			},
		},
		{
			ID:       "sourcing-recommendations-daily",
			AgentID:  "sourcing",
			Interval: 24 * time.Hour,
			Priority: 1,
			Payload: map[string]any{
				"sku": "RB-SET",
			},
		},
	}, nil)
}

func (s *server) agentSchedulesHandler(w http.ResponseWriter, r *http.Request) {
	s.ensureAgentScheduler()
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agent-schedules"), "/")
	switch {
	case path == "" && r.Method == http.MethodGet:
		schedules := s.agentSchedules.List(r.Context())
		writeJSON(w, http.StatusOK, agentSchedulesResponse{Schedules: toAgentSchedules(schedules)})
	case strings.HasSuffix(path, "/enable") && r.Method == http.MethodPost:
		s.updateAgentSchedule(w, r, strings.TrimSuffix(path, "/enable"), true)
	case strings.HasSuffix(path, "/disable") && r.Method == http.MethodPost:
		s.updateAgentSchedule(w, r, strings.TrimSuffix(path, "/disable"), false)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) updateAgentSchedule(w http.ResponseWriter, r *http.Request, rawID string, enabled bool) {
	id := strings.Trim(rawID, "/")
	var (
		schedule orchestrator.Schedule
		err      error
	)
	if enabled {
		schedule, err = s.agentSchedules.Enable(r.Context(), id)
	} else {
		schedule, err = s.agentSchedules.Disable(r.Context(), id)
	}
	if err != nil {
		if errors.Is(err, orchestrator.ErrScheduleNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "schedule_not_found"})
			return
		}
		s.log.Error("update agent schedule", "schedule_id", id, "enabled", enabled, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, agentScheduleResponse{Schedule: toAgentSchedule(schedule)})
}

func toAgentSchedules(schedules []orchestrator.Schedule) []agentSchedule {
	out := make([]agentSchedule, len(schedules))
	for i, schedule := range schedules {
		out[i] = toAgentSchedule(schedule)
	}
	return out
}

func toAgentSchedule(schedule orchestrator.Schedule) agentSchedule {
	return agentSchedule{
		ID:              schedule.ID,
		AgentID:         schedule.AgentID,
		Cron:            schedule.Cron,
		IntervalSeconds: int64(schedule.Interval.Seconds()),
		Enabled:         schedule.Enabled,
		Priority:        schedule.Priority,
		Payload:         schedule.Payload,
		NextRunAt:       schedule.NextRunAt,
		CreatedAt:       schedule.CreatedAt,
		UpdatedAt:       schedule.UpdatedAt,
	}
}
