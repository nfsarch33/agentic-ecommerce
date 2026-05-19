package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	orchestrator "github.com/nfsarch33/helixon-ec/internal/agent"
)

type agentsListResponse struct {
	Agents []orchestrator.Descriptor `json:"agents"`
}

type agentRunRequest struct {
	Priority int            `json:"priority,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
}

type agentRunResponse struct {
	ID         string                `json:"id"`
	TaskID     string                `json:"task_id"`
	AgentID    string                `json:"agent_id"`
	State      string                `json:"state"`
	Priority   int                   `json:"priority"`
	Input      map[string]any        `json:"input,omitempty"`
	Result     map[string]any        `json:"result,omitempty"`
	Error      orchestrator.RunError `json:"error,omitempty"`
	CreatedAt  time.Time             `json:"created_at"`
	StartedAt  *time.Time            `json:"started_at,omitempty"`
	FinishedAt *time.Time            `json:"finished_at,omitempty"`
}

type agentHistoryResponse struct {
	Runs []agentRunResponse `json:"runs"`
}

func (s *server) agentsHandler(w http.ResponseWriter, r *http.Request) {
	s.ensureAgentScheduler()
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agents"), "/")
	switch {
	case path == "" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, agentsListResponse{Agents: s.agentRegistry.List()})
	case strings.HasSuffix(path, "/run") && r.Method == http.MethodPost:
		s.runAgent(w, r, strings.TrimSuffix(path, "/run"))
	case strings.HasSuffix(path, "/history") && r.Method == http.MethodGet:
		s.agentHistory(w, r, strings.TrimSuffix(path, "/history"))
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) runAgent(w http.ResponseWriter, r *http.Request, agentID string) {
	agentID = strings.Trim(agentID, "/")
	var req agentRunRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
	}
	run, err := s.agentScheduler.Submit(r.Context(), orchestrator.SubmitRequest{
		AgentID:  agentID,
		Priority: req.Priority,
		Payload:  req.Payload,
	})
	if err != nil {
		if errors.Is(err, orchestrator.ErrAgentNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent_not_found"})
			return
		}
		s.log.Error("submit agent run", "agent_id", agentID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusAccepted, toAgentRunResponse(run))
}

func (s *server) agentHistory(w http.ResponseWriter, r *http.Request, agentID string) {
	agentID = strings.Trim(agentID, "/")
	if _, ok := s.agentRegistry.Get(agentID); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent_not_found"})
		return
	}
	runs, err := s.agentSchedulerRunsByAgent(r, agentID)
	if err != nil {
		s.log.Error("list agent history", "agent_id", agentID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, agentHistoryResponse{Runs: toAgentRunResponses(runs)})
}

func (s *server) agentRunsHandler(w http.ResponseWriter, r *http.Request) {
	s.ensureAgentScheduler()
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	runID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agent-runs/"), "/")
	run, err := s.agentSchedulerRun(r, runID)
	if err != nil {
		if errors.Is(err, orchestrator.ErrRunNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "run_not_found"})
			return
		}
		s.log.Error("get agent run", "run_id", runID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, toAgentRunResponse(run))
}

func (s *server) agentSchedulerRun(r *http.Request, runID string) (orchestrator.Run, error) {
	return s.agentScheduler.GetRun(r.Context(), runID)
}

func (s *server) agentSchedulerRunsByAgent(r *http.Request, agentID string) ([]orchestrator.Run, error) {
	return s.agentScheduler.ListRunsByAgent(r.Context(), agentID)
}

func toAgentRunResponses(runs []orchestrator.Run) []agentRunResponse {
	out := make([]agentRunResponse, len(runs))
	for i, run := range runs {
		out[i] = toAgentRunResponse(run)
	}
	return out
}

func toAgentRunResponse(run orchestrator.Run) agentRunResponse {
	return agentRunResponse{
		ID:         run.ID,
		TaskID:     run.TaskID,
		AgentID:    run.AgentID,
		State:      string(run.State),
		Priority:   run.Priority,
		Input:      run.Input,
		Result:     run.Result,
		Error:      run.Error,
		CreatedAt:  run.CreatedAt,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
	}
}
