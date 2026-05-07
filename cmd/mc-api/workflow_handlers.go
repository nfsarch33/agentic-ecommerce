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

	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
)

type temporalWorkflowClient interface {
	ExecuteWorkflow(context.Context, client.StartWorkflowOptions, any, ...any) (workflowRun, error)
	DescribeWorkflowExecution(context.Context, string, string) (*workflowservicepb.DescribeWorkflowExecutionResponse, error)
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

func (s *server) workflowsHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/workflows")
	path = strings.Trim(path, "/")

	switch {
	case path == "product-publish" && r.Method == http.MethodPost:
		s.startProductPublishWorkflow(w, r)
	case path == "content-generation" && r.Method == http.MethodPost:
		s.startContentGenerationWorkflow(w, r)
	case strings.HasSuffix(path, "/signals/review") && r.Method == http.MethodPost:
		s.signalProductPublishReview(w, r, path)
	case path != "" && r.Method == http.MethodGet:
		s.getWorkflowStatus(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
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
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_product_id"})
		return
	}
	product, err := s.repo.GetByID(r.Context(), productID)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get product for content workflow", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	productInfo := toContentProductInfo(product)
	agentReq := contentagent.GenerateRequest{
		Product:  productInfo,
		Style:    normalizeContentStyle(req.Style),
		Language: req.Language,
		MaxWords: req.MaxWords,
		Keywords: req.Keywords,
	}
	if agentReq.MaxWords == 0 {
		agentReq.MaxWords = 120
	}
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
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_product_id"})
		return
	}
	if _, err := s.repo.GetByID(r.Context(), productID); err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get product for workflow", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
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

func (s *server) getWorkflowStatus(w http.ResponseWriter, r *http.Request, workflowID string) {
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}
	resp, err := s.workflowClient.DescribeWorkflowExecution(r.Context(), workflowID, "")
	if err != nil {
		s.log.Error("describe workflow", "workflow_id", workflowID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_describe_failed"})
		return
	}
	info := resp.GetWorkflowExecutionInfo()
	if info == nil || info.GetExecution() == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	var startTime *time.Time
	if ts := info.GetStartTime(); ts != nil {
		t := ts.AsTime()
		startTime = &t
	}
	var closeTime *time.Time
	if ts := info.GetCloseTime(); ts != nil {
		t := ts.AsTime()
		closeTime = &t
	}
	writeJSON(w, http.StatusOK, workflowStatusResponse{
		WorkflowID: info.GetExecution().GetWorkflowId(),
		RunID:      info.GetExecution().GetRunId(),
		Status:     workflowStatus(info.GetStatus()),
		StartTime:  startTime,
		CloseTime:  closeTime,
	})
}

func (s *server) signalProductPublishReview(w http.ResponseWriter, r *http.Request, path string) {
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}
	workflowID := strings.TrimSuffix(path, "/signals/review")
	workflowID = strings.Trim(workflowID, "/")
	if workflowID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_workflow_id"})
		return
	}
	var signal ecworkflow.ReviewSignal
	if err := json.NewDecoder(r.Body).Decode(&signal); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if err := s.workflowClient.SignalWorkflow(r.Context(), workflowID, "", ecworkflow.ProductPublishReviewSignal, signal); err != nil {
		s.log.Error("signal workflow review", "workflow_id", workflowID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_signal_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "signaled"})
}

func workflowStatus(status enumspb.WorkflowExecutionStatus) string {
	switch status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		return "running"
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "completed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "canceled"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "terminated"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return "continued_as_new"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "timed_out"
	default:
		return "unspecified"
	}
}
