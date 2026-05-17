package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	workflowservicepb "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"

	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	sourcingagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/sourcing"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync"
	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
)

type temporalWorkflowClient interface {
	ExecuteWorkflow(context.Context, client.StartWorkflowOptions, any, ...any) (workflowRun, error)
	DescribeWorkflowExecution(context.Context, string, string) (*workflowservicepb.DescribeWorkflowExecutionResponse, error)
	ListWorkflow(context.Context, *workflowservicepb.ListWorkflowExecutionsRequest) (*workflowservicepb.ListWorkflowExecutionsResponse, error)
	QueryWorkflow(context.Context, string, string, string, ...interface{}) (converter.EncodedValue, error)
	SignalWorkflow(context.Context, string, string, string, any) error
}

type workflowRun interface {
	GetID() string
	GetRunID() string
}

type startProductPublishWorkflowRequest struct {
	ProductID   string `json:"product_id"`
	RequestedBy string `json:"requested_by,omitempty"`
}

type startContentGenerationWorkflowRequest struct {
	ProductID   string   `json:"product_id"`
	RequestedBy string   `json:"requested_by,omitempty"`
	Style       string   `json:"style,omitempty"`
	Language    string   `json:"language,omitempty"`
	MaxWords    int      `json:"max_words,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
}

type startMediaProcessingWorkflowRequest struct {
	ProductID        string                     `json:"product_id"`
	SourceURL        string                     `json:"source_url"`
	AltText          string                     `json:"alt_text,omitempty"`
	RequestedBy      string                     `json:"requested_by,omitempty"`
	Resize           intelligence.ResizeOptions `json:"resize,omitempty"`
	Format           string                     `json:"format,omitempty"`
	RemoveBackground bool                       `json:"remove_background,omitempty"`
}

type startSourcingWorkflowRequest struct {
	SKU                     string                    `json:"sku"`
	Query                   string                    `json:"query,omitempty"`
	EstimatedSellPriceCents int                       `json:"estimated_sell_price_cents"`
	MinimumMarginPct        float64                   `json:"minimum_margin_pct"`
	RequestedBy             string                    `json:"requested_by,omitempty"`
	CandidateLimit          int                       `json:"candidate_limit,omitempty"`
	Candidates              []sourcingagent.Candidate `json:"candidates,omitempty"`
}

type marketplaceSyncEventPayload struct {
	TenantID   string         `json:"tenant_id"`
	Provider   string         `json:"provider"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id"`
	ExternalID string         `json:"external_id,omitempty"`
	Operation  string         `json:"operation"`
	Version    string         `json:"version"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type marketplaceDLQRecordPayload struct {
	ID       string                      `json:"id,omitempty"`
	Event    marketplaceSyncEventPayload `json:"event"`
	Attempts int                         `json:"attempts,omitempty"`
	Reason   string                      `json:"reason,omitempty"`
}

type startMarketplaceSyncWorkflowRequest struct {
	Event marketplaceSyncEventPayload `json:"event"`
}

type startMarketplaceReplayWorkflowRequest struct {
	Record marketplaceDLQRecordPayload `json:"record"`
}

type workflowStartResponse struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	TaskQueue  string `json:"task_queue"`
}

type workflowStatusResponse struct {
	WorkflowID string     `json:"workflow_id"`
	RunID      string     `json:"run_id,omitempty"`
	Status     string     `json:"status"`
	StartTime  *time.Time `json:"start_time,omitempty"`
	CloseTime  *time.Time `json:"close_time,omitempty"`
}

func newTemporalWorkflowClient(logger *slog.Logger, hostPort string) (temporalWorkflowClient, func()) {
	hostPort = strings.TrimSpace(hostPort)
	if hostPort == "" {
		return nil, nil
	}
	c, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		if logger != nil {
			logger.Warn("temporal client not configured", "host_port", hostPort, "error", err)
		}
		return nil, nil
	}
	return temporalClientAdapter{Client: c}, c.Close
}

type temporalClientAdapter struct {
	client.Client
}

func (a temporalClientAdapter) ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (workflowRun, error) {
	return a.Client.ExecuteWorkflow(ctx, options, workflow, args...)
}

var workflowStartHandlers = map[string]func(*server, http.ResponseWriter, *http.Request){
	"product-publish":    (*server).startProductPublishWorkflow,
	"content-generation": (*server).startContentGenerationWorkflow,
	"media-processing":   (*server).startMediaProcessingWorkflow,
	"sourcing":           (*server).startSourcingWorkflow,
	"marketplace-sync":   (*server).startMarketplaceSyncWorkflow,
	"marketplace-replay": (*server).startMarketplaceReplayWorkflow,
}

func (s *server) workflowsHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/workflows")
	path = strings.Trim(path, "/")

	if r.Method == http.MethodGet {
		if path == "" {
			s.listWorkflows(w, r)
			return
		}
		s.getWorkflowStatus(w, r, path)
		return
	}

	if r.Method == http.MethodPost {
		if strings.HasSuffix(path, "/signals/review") {
			s.signalProductPublishReview(w, r, path)
			return
		}
		if handler, ok := workflowStartHandlers[path]; ok {
			handler(s, w, r)
			return
		}
	}

	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
}

func (s *server) startSourcingWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}
	var req startSourcingWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	sku := strings.TrimSpace(req.SKU)
	if sku == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "sku_required"})
		return
	}
	if len(req.Candidates) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "candidates_required"})
		return
	}
	input := ecworkflow.SourcingWorkflowInput{
		Search: ecworkflow.SourcingSearchInput{
			SKU:                     sku,
			Query:                   strings.TrimSpace(req.Query),
			EstimatedSellPriceCents: req.EstimatedSellPriceCents,
			CandidateLimit:          req.CandidateLimit,
			Candidates:              req.Candidates,
		},
		MinimumMarginPct: req.MinimumMarginPct,
		RequestedBy:      req.RequestedBy,
	}
	workflowID := fmt.Sprintf("sourcing-%s-%s", sanitizeWorkflowIDPart(input.Search.SKU), uuid.NewString())
	run, err := s.workflowClient.ExecuteWorkflow(
		r.Context(),
		client.StartWorkflowOptions{ID: workflowID, TaskQueue: ecworkflow.TaskQueue},
		ecworkflow.SourcingWorkflow,
		input,
	)
	if err != nil {
		s.log.Error("start sourcing workflow", "sku", input.Search.SKU, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_start_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, workflowStartResponse{
		WorkflowID: run.GetID(),
		RunID:      run.GetRunID(),
		Status:     "started",
		TaskQueue:  ecworkflow.TaskQueue,
	})
}

func (s *server) startMediaProcessingWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}
	var req startMediaProcessingWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(req.ProductID) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "product_id_required"})
		return
	}
	if strings.TrimSpace(req.SourceURL) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "source_url_required"})
		return
	}
	input := ecworkflow.MediaProcessingInput{
		ProductID:        strings.TrimSpace(req.ProductID),
		SourceURL:        strings.TrimSpace(req.SourceURL),
		AltText:          req.AltText,
		RequestedBy:      req.RequestedBy,
		Resize:           req.Resize,
		Format:           req.Format,
		RemoveBackground: req.RemoveBackground,
	}
	workflowID := fmt.Sprintf("media-processing-%s-%s", sanitizeWorkflowIDPart(input.ProductID), uuid.NewString())
	run, err := s.workflowClient.ExecuteWorkflow(
		r.Context(),
		client.StartWorkflowOptions{ID: workflowID, TaskQueue: ecworkflow.TaskQueue},
		ecworkflow.MediaProcessingWorkflow,
		input,
	)
	if err != nil {
		s.log.Error("start media processing workflow", "product_id", input.ProductID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_start_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, workflowStartResponse{
		WorkflowID: run.GetID(),
		RunID:      run.GetRunID(),
		Status:     "started",
		TaskQueue:  ecworkflow.TaskQueue,
	})
}

func (s *server) startContentGenerationWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}
	var req startContentGenerationWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	product, productID, ok := s.loadProductForWorkflow(w, r, req.ProductID, "get product for content workflow")
	if !ok {
		return
	}
	productInfo := toContentProductInfo(product)
	agentReq := toContentGenerationRequest(productInfo, req)
	workflowID := fmt.Sprintf("content-generation-%s-%s", productID.String(), uuid.NewString())
	run, err := s.workflowClient.ExecuteWorkflow(
		r.Context(),
		client.StartWorkflowOptions{ID: workflowID, TaskQueue: ecworkflow.TaskQueue},
		ecworkflow.ContentGenerationWorkflow,
		ecworkflow.ContentGenerationInput{Product: productInfo, Request: agentReq, RequestedBy: req.RequestedBy},
	)
	if err != nil {
		s.log.Error("start content generation workflow", "product_id", productID.String(), "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_start_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, workflowStartResponse{
		WorkflowID: run.GetID(),
		RunID:      run.GetRunID(),
		Status:     "started",
		TaskQueue:  ecworkflow.TaskQueue,
	})
}

func sanitizeWorkflowIDPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, " ", "-")
	if value == "" {
		return "media"
	}
	return value
}

func (s *server) startMarketplaceSyncWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}
	var req startMarketplaceSyncWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	event, errCode := req.Event.toProductEvent()
	if errCode != "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": errCode})
		return
	}
	input := ecworkflow.MarketplaceSyncInput{Event: event}
	workflowID := fmt.Sprintf("marketplace-sync-%s-%s-%s", sanitizeWorkflowIDPart(event.Provider), sanitizeWorkflowIDPart(event.EntityID), uuid.NewString())
	run, err := s.workflowClient.ExecuteWorkflow(
		r.Context(),
		client.StartWorkflowOptions{ID: workflowID, TaskQueue: ecworkflow.TaskQueue},
		ecworkflow.MarketplaceSyncWorkflow,
		input,
	)
	if err != nil {
		s.log.Error("start marketplace sync workflow", "provider", event.Provider, "entity_id", event.EntityID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_start_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, workflowStartResponse{
		WorkflowID: run.GetID(),
		RunID:      run.GetRunID(),
		Status:     "started",
		TaskQueue:  ecworkflow.TaskQueue,
	})
}

func (s *server) startMarketplaceReplayWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}
	var req startMarketplaceReplayWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	record, errCode := req.Record.toDLQRecord()
	if errCode != "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": errCode})
		return
	}
	input := ecworkflow.MarketplaceReplayInput{Record: record}
	workflowID := fmt.Sprintf("marketplace-replay-%s-%s-%s", sanitizeWorkflowIDPart(record.Event.Provider), sanitizeWorkflowIDPart(firstNonEmpty(record.ID, record.Event.EntityID)), uuid.NewString())
	run, err := s.workflowClient.ExecuteWorkflow(
		r.Context(),
		client.StartWorkflowOptions{ID: workflowID, TaskQueue: ecworkflow.TaskQueue},
		ecworkflow.MarketplaceReplayWorkflow,
		input,
	)
	if err != nil {
		s.log.Error("start marketplace replay workflow", "record_id", record.ID, "provider", record.Event.Provider, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_start_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, workflowStartResponse{
		WorkflowID: run.GetID(),
		RunID:      run.GetRunID(),
		Status:     "started",
		TaskQueue:  ecworkflow.TaskQueue,
	})
}

func (s *server) startProductPublishWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}
	var req startProductPublishWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	_, productID, ok := s.loadProductForWorkflow(w, r, req.ProductID, "get product for workflow")
	if !ok {
		return
	}
	workflowID := fmt.Sprintf("product-publish-%s-%s", productID.String(), uuid.NewString())
	run, err := s.workflowClient.ExecuteWorkflow(
		r.Context(),
		client.StartWorkflowOptions{ID: workflowID, TaskQueue: ecworkflow.TaskQueue},
		ecworkflow.ProductPublishWorkflow,
		ecworkflow.ProductPublishInput{ProductID: productID.String(), RequestedBy: req.RequestedBy},
	)
	if err != nil {
		s.log.Error("start product publish workflow", "product_id", productID.String(), "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_start_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, workflowStartResponse{
		WorkflowID: run.GetID(),
		RunID:      run.GetRunID(),
		Status:     "started",
		TaskQueue:  ecworkflow.TaskQueue,
	})
}

var workflowStatuses = map[enumspb.WorkflowExecutionStatus]string{
	enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:          "running",
	enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:        "completed",
	enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:           "failed",
	enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:         "canceled",
	enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:       "terminated",
	enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW: "continued_as_new",
	enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:        "timed_out",
}

func workflowStatus(status enumspb.WorkflowExecutionStatus) string {
	if normalized, ok := workflowStatuses[status]; ok {
		return normalized
	}
	return "unspecified"
}

func (p marketplaceSyncEventPayload) toProductEvent() (marketplacesync.ProductEvent, string) {
	event := marketplacesync.ProductEvent{
		TenantID:   strings.TrimSpace(p.TenantID),
		Provider:   strings.TrimSpace(p.Provider),
		EntityType: marketplacesync.EntityType(strings.TrimSpace(p.EntityType)),
		EntityID:   strings.TrimSpace(p.EntityID),
		ExternalID: strings.TrimSpace(p.ExternalID),
		Operation:  marketplacesync.Operation(strings.TrimSpace(p.Operation)),
		Version:    strings.TrimSpace(p.Version),
		Payload:    p.Payload,
	}
	if errCode := validateMarketplaceEvent(event); errCode != "" {
		return marketplacesync.ProductEvent{}, errCode
	}
	return event, ""
}

func (p marketplaceDLQRecordPayload) toDLQRecord() (marketplacesync.DLQRecord, string) {
	event, errCode := p.Event.toProductEvent()
	if errCode != "" {
		return marketplacesync.DLQRecord{}, errCode
	}
	attempts := p.Attempts
	if attempts < 1 {
		attempts = 1
	}
	return marketplacesync.DLQRecord{
		ID:       strings.TrimSpace(p.ID),
		Event:    event,
		Attempts: attempts,
		Reason:   strings.TrimSpace(p.Reason),
	}, ""
}

func marketplaceSyncEventFromDomain(event marketplacesync.ProductEvent) marketplaceSyncEventPayload {
	return marketplaceSyncEventPayload{
		TenantID:   event.TenantID,
		Provider:   event.Provider,
		EntityType: string(event.EntityType),
		EntityID:   event.EntityID,
		ExternalID: event.ExternalID,
		Operation:  string(event.Operation),
		Version:    event.Version,
		Payload:    event.Payload,
	}
}

func marketplaceDLQRecordFromDomain(record marketplacesync.DLQRecord) marketplaceDLQRecordPayload {
	return marketplaceDLQRecordPayload{
		ID:       record.ID,
		Event:    marketplaceSyncEventFromDomain(record.Event),
		Attempts: record.Attempts,
		Reason:   record.Reason,
	}
}

func (s *server) loadProductForWorkflow(w http.ResponseWriter, r *http.Request, rawProductID, logMessage string) (catalog.Product, uuid.UUID, bool) {
	productID, err := uuid.Parse(rawProductID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_product_id"})
		return catalog.Product{}, uuid.Nil, false
	}
	product, err := s.repo.GetByID(r.Context(), productID)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return catalog.Product{}, uuid.Nil, false
		}
		s.log.Error(logMessage, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return catalog.Product{}, uuid.Nil, false
	}
	return product, productID, true
}

func toContentGenerationRequest(product contentagent.ProductInfo, req startContentGenerationWorkflowRequest) contentagent.GenerateRequest {
	generateRequest := contentagent.GenerateRequest{
		Product:  product,
		Style:    normalizeContentStyle(req.Style),
		Language: req.Language,
		MaxWords: req.MaxWords,
		Keywords: req.Keywords,
	}
	if generateRequest.MaxWords == 0 {
		generateRequest.MaxWords = 120
	}
	return generateRequest
}

func validateMarketplaceEvent(event marketplacesync.ProductEvent) string {
	checks := []struct {
		valid bool
		code  string
	}{
		{valid: event.TenantID != "", code: "tenant_id_required"},
		{valid: event.Provider != "", code: "provider_required"},
		{valid: event.EntityType != "", code: "entity_type_required"},
		{valid: event.EntityID != "", code: "entity_id_required"},
		{valid: event.Operation != "", code: "operation_required"},
		{valid: event.Version != "", code: "version_required"},
	}
	for _, check := range checks {
		if !check.valid {
			return check.code
		}
	}
	return ""
}
