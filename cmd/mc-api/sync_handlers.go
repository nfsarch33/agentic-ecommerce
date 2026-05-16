package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	wcwebhook "github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce/webhook"
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync"
	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
	"go.temporal.io/sdk/client"
)

type syncStatusResponse struct {
	TotalEvents               int                                  `json:"total_events"`
	PendingConflicts          int                                  `json:"pending_conflicts"`
	LastEvent                 *enginesync.Event                    `json:"last_event,omitempty"`
	LastError                 string                               `json:"last_error,omitempty"`
	UpdatedAt                 time.Time                            `json:"updated_at"`
	DLQDepth                  int                                  `json:"dlq_depth"`
	MarketplaceReplay         marketplaceReplayStateResponse       `json:"marketplace_replay"`
	MarketplaceReconciliation marketplaceReconciliationStateResponse `json:"marketplace_reconciliation"`
}

type conflictsResponse struct {
	Conflicts []enginesync.Conflict `json:"conflicts"`
}

type marketplaceReplayStateResponse struct {
	State     string     `json:"state"`
	RecordID  string     `json:"record_id,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type marketplaceReconciliationStateResponse struct {
	TotalLocal    int `json:"total_local"`
	TotalRemote   int `json:"total_remote"`
	MismatchCount int `json:"mismatch_count"`
}

type marketplaceDLQListResponse struct {
	Records []marketplaceDLQRecordPayload `json:"records"`
	Total   int                           `json:"total"`
}

type resolveConflictRequest struct {
	Resolution string `json:"resolution"`
	Note       string `json:"note,omitempty"`
}

func (s *server) syncHandler(w http.ResponseWriter, r *http.Request) {
	if s.syncEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "sync_not_configured"})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sync")
	path = strings.Trim(path, "/")
	switch {
	case path == "status" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, s.currentSyncStatus())
	case path == "dlq" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, s.marketplaceDLQList())
	case path == "conflicts" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, conflictsResponse{Conflicts: s.syncEngine.Conflicts()})
	case strings.HasPrefix(path, "conflicts/") && strings.HasSuffix(path, "/resolve") && r.Method == http.MethodPost:
		s.resolveConflict(w, r, path)
	case strings.HasPrefix(path, "dlq/") && strings.HasSuffix(path, "/replay") && r.Method == http.MethodPost:
		s.replayMarketplaceDLQ(w, r, path)
	case strings.HasPrefix(path, "products/") && strings.HasSuffix(path, "/publish") && r.Method == http.MethodPost:
		s.publishProduct(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) currentSyncStatus() syncStatusResponse {
	status := s.syncEngine.Status()
	response := syncStatusResponse{
		TotalEvents:      status.TotalEvents,
		PendingConflicts: status.PendingConflicts,
		LastEvent:        status.LastEvent,
		LastError:        status.LastError,
		UpdatedAt:        status.UpdatedAt,
		DLQDepth:         0,
		MarketplaceReplay: marketplaceReplayStateResponse{
			State: marketplacesync.ReplayStateIdle,
		},
		MarketplaceReconciliation: marketplaceReconciliationStateResponse{},
	}
	if s.marketplaceSync == nil {
		return response
	}
	snapshot := s.marketplaceSync.Snapshot()
	response.DLQDepth = snapshot.DLQDepth
	response.MarketplaceReplay = marketplaceReplayStateResponse{
		State:    snapshot.Replay.State,
		RecordID: snapshot.Replay.RecordID,
	}
	if !snapshot.Replay.UpdatedAt.IsZero() {
		replayUpdatedAt := snapshot.Replay.UpdatedAt
		response.MarketplaceReplay.UpdatedAt = &replayUpdatedAt
	}
	response.MarketplaceReconciliation = marketplaceReconciliationStateResponse{
		TotalLocal:    snapshot.Reconciliation.TotalLocal,
		TotalRemote:   snapshot.Reconciliation.TotalRemote,
		MismatchCount: len(snapshot.Reconciliation.Mismatches),
	}
	return response
}

func (s *server) marketplaceDLQList() marketplaceDLQListResponse {
	if s.marketplaceSync == nil {
		return marketplaceDLQListResponse{Records: []marketplaceDLQRecordPayload{}, Total: 0}
	}
	records := s.marketplaceSync.Records()
	payload := make([]marketplaceDLQRecordPayload, 0, len(records))
	for _, record := range records {
		payload = append(payload, marketplaceDLQRecordFromDomain(record))
	}
	return marketplaceDLQListResponse{Records: payload, Total: len(payload)}
}

func (s *server) resolveConflict(w http.ResponseWriter, r *http.Request, path string) {
	id := strings.TrimPrefix(path, "conflicts/")
	id = strings.TrimSuffix(id, "/resolve")
	id = strings.Trim(id, "/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_conflict_id"})
		return
	}
	var req resolveConflictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	conflict, err := s.syncEngine.ResolveConflict(id, req.Resolution, req.Note)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, conflict)
}

func (s *server) replayMarketplaceDLQ(w http.ResponseWriter, r *http.Request, path string) {
	recordID := strings.TrimPrefix(path, "dlq/")
	recordID = strings.TrimSuffix(recordID, "/replay")
	recordID = strings.Trim(recordID, "/")
	if recordID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_record_id"})
		return
	}
	if s.marketplaceSync == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	record, ok := s.marketplaceSync.Record(recordID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}
	workflowID := fmt.Sprintf("marketplace-replay-%s-%s-%s", sanitizeWorkflowIDPart(record.Event.Provider), sanitizeWorkflowIDPart(firstNonEmpty(record.ID, record.Event.EntityID)), uuid.NewString())
	run, err := s.workflowClient.ExecuteWorkflow(
		r.Context(),
		client.StartWorkflowOptions{ID: workflowID, TaskQueue: ecworkflow.TaskQueue},
		ecworkflow.MarketplaceReplayWorkflow,
		ecworkflow.MarketplaceReplayInput{Record: record},
	)
	if err != nil {
		s.log.Error("start marketplace replay workflow", "record_id", recordID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_start_failed"})
		return
	}
	s.marketplaceSync.MarkReplayQueued(recordID)
	writeJSON(w, http.StatusAccepted, workflowStartResponse{
		WorkflowID: run.GetID(),
		RunID:      run.GetRunID(),
		Status:     "started",
		TaskQueue:  ecworkflow.TaskQueue,
	})
}

func (s *server) publishProduct(w http.ResponseWriter, r *http.Request, path string) {
	idPart := strings.TrimPrefix(path, "products/")
	idPart = strings.TrimSuffix(idPart, "/publish")
	idPart = strings.Trim(idPart, "/")
	id, err := uuid.Parse(idPart)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	if err := s.syncEngine.PublishToWooCommerce(r.Context(), id); err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("publish product", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "publish_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

func (s *server) woocommerceOrderWebhookHandler(w http.ResponseWriter, r *http.Request) {
	wcwebhook.NewHandler(wcwebhook.Config{
		Secret:   s.webhookSecret,
		Resource: wcwebhook.ResourceOrders,
		Recorder: s.syncEngine,
	}).ServeHTTP(w, r)
}

func (s *server) woocommerceProductWebhookHandler(w http.ResponseWriter, r *http.Request) {
	wcwebhook.NewHandler(wcwebhook.Config{
		Secret:   s.webhookSecret,
		Resource: wcwebhook.ResourceProducts,
		Recorder: s.syncEngine,
	}).ServeHTTP(w, r)
}
