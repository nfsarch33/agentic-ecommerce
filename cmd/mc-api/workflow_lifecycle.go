package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/publishgate"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	workflowservicepb "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/types/known/timestamppb"

	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
)

type workflowListResponse struct {
	Workflows []workflowSummaryResponse `json:"workflows"`
}

type workflowSummaryResponse struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	ProductID       string `json:"product_id,omitempty"`
	ProductTitle    string `json:"product_title,omitempty"`
	CurrentActivity string `json:"current_activity,omitempty"`
	StartedAt       string `json:"started_at"`
	UpdatedAt       string `json:"updated_at"`
	CompletedAt     string `json:"completed_at,omitempty"`
	Error           string `json:"error,omitempty"`
}

type workflowActivityResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Message     string `json:"message,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`
	Error       string `json:"error,omitempty"`
}

type workflowReviewResponse struct {
	Approved bool   `json:"approved"`
	Reviewer string `json:"reviewer,omitempty"`
	Note     string `json:"note,omitempty"`
}

type workflowDetailResponse struct {
	workflowSummaryResponse
	Review     *workflowReviewResponse    `json:"review,omitempty"`
	RunID      string                     `json:"run_id,omitempty"`
	Activities []workflowActivityResponse `json:"activities"`
}

type workflowSignalResponse struct {
	Status   string                  `json:"status"`
	Workflow *workflowDetailResponse `json:"workflow,omitempty"`
}

func (s *server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}

	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	listResp, err := s.workflowClient.ListWorkflow(r.Context(), workflowListRequest(r))
	if err != nil {
		s.log.Error("list workflows", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_list_failed"})
		return
	}
	writeJSON(w, http.StatusOK, workflowListResponse{
		Workflows: s.collectWorkflowSummaries(r.Context(), listResp.GetExecutions(), statusFilter),
	})
}

func (s *server) getWorkflowStatus(w http.ResponseWriter, r *http.Request, workflowID string) {
	if s.workflowClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal_not_configured"})
		return
	}
	detail, err := s.workflowDetail(r.Context(), workflowID)
	if err != nil {
		if errors.Is(err, errWorkflowNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("describe workflow", "workflow_id", workflowID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_describe_failed"})
		return
	}
	writeJSON(w, http.StatusOK, detail)
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

	// The decision rides the Update path (exactly-once): the UpdateID is the
	// caller's Idempotency-Key when present, minted otherwise. The deprecated
	// signal remains as a fallback for workflows the Update cannot reach and
	// records the approval row API-side so the publish gate still sees it.
	updateID := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if updateID == "" {
		updateID = fmt.Sprintf("review-%s-%d", workflowID, time.Now().UnixNano())
	}
	handle, err := s.workflowClient.UpdateWorkflow(r.Context(), client.UpdateWorkflowOptions{
		WorkflowID:   workflowID,
		UpdateName:   "product-publish-review",
		UpdateID:     updateID,
		Args:         []interface{}{ecworkflow.ReviewUpdateInput{Approved: signal.Approved, Reviewer: signal.Reviewer, Reason: signal.Note, UpdateID: updateID}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err == nil {
		var res ecworkflow.ReviewUpdateResult
		if gErr := handle.Get(r.Context(), &res); gErr == nil && res.Accepted {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":   "decided",
				"decision": decisionString(res.Review.Approved),
				"reviewer": res.Review.Reviewer,
			})
			return
		} else if gErr != nil {
			// Rejections surface at Get (the update ran and the validator or
			// handler refused): map to the already-decided contract.
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":    "already_decided",
				"decision": decisionStringFromErr(gErr.Error()),
			})
			return
		}
	}
	// Temporal unreachable or update unsupported for this workflow: fall back
	// to the deprecated signal, recording the approval row first (the gate
	// fails closed without it).
	if s.approvals != nil {
		_, _, _ = s.approvals.RecordApproval(r.Context(), publishgate.Decision{
			WorkflowID: workflowID,
			TenantID:   s.tenantOf(r),
			ProductID:  workflowIDProductID(workflowID),
			Approved:   signal.Approved,
			Actor:      signal.Reviewer,
			Reason:     signal.Note,
			UpdateID:   updateID,
		})
	}
	if err := s.workflowClient.SignalWorkflow(r.Context(), workflowID, "", ecworkflow.ProductPublishReviewSignal, signal); err != nil {
		s.log.Error("signal workflow review", "workflow_id", workflowID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "workflow_signal_failed"})
		return
	}

	resp := workflowSignalResponse{Status: "signaled"}
	if detail, err := s.workflowDetail(r.Context(), workflowID); err == nil {
		resp.Workflow = &detail
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func decisionString(approved bool) string {
	if approved {
		return "approved"
	}
	return "rejected"
}

func decisionStringFromErr(msg string) string {
	if strings.Contains(msg, "approved") {
		return "approved"
	}
	return "rejected"
}

var errWorkflowNotFound = errors.New("workflow not found")

func (s *server) workflowDetail(ctx context.Context, workflowID string) (workflowDetailResponse, error) {
	resp, err := s.workflowClient.DescribeWorkflowExecution(ctx, workflowID, "")
	if err != nil {
		return workflowDetailResponse{}, err
	}
	info := resp.GetWorkflowExecutionInfo()
	if info == nil || info.GetExecution() == nil {
		return workflowDetailResponse{}, errWorkflowNotFound
	}

	detail := workflowDetailResponse{
		workflowSummaryResponse: workflowSummaryResponse{
			ID:              info.GetExecution().GetWorkflowId(),
			Type:            workflowTypeSlug(info.GetType().GetName()),
			Status:          workflowStatus(info.GetStatus()),
			ProductID:       memoString(info.GetMemo(), "product_id"),
			ProductTitle:    memoString(info.GetMemo(), "product_title"),
			CurrentActivity: "Temporal execution",
			StartedAt:       formatProtoTimestamp(info.GetStartTime()),
			UpdatedAt:       firstNonEmpty(formatProtoTimestamp(info.GetCloseTime()), formatProtoTimestamp(info.GetStartTime())),
			CompletedAt:     formatProtoTimestamp(info.GetCloseTime()),
		},
		RunID: info.GetExecution().GetRunId(),
		Activities: []workflowActivityResponse{
			{
				ID:          "temporal_execution",
				Name:        "Temporal execution",
				Status:      workflowActivityStatus(info.GetStatus()),
				StartedAt:   formatProtoTimestamp(info.GetStartTime()),
				CompletedAt: formatProtoTimestamp(info.GetCloseTime()),
				Message:     fmt.Sprintf("Temporal status: %s", strings.ReplaceAll(workflowStatus(info.GetStatus()), "_", " ")),
			},
		},
	}

	if detail.Type == "product_publish" {
		s.applyProductPublishSnapshot(ctx, &detail, info)
	}
	return detail, nil
}

func (s *server) workflowSummaryFromExecution(ctx context.Context, info *workflowpb.WorkflowExecutionInfo) (workflowSummaryResponse, error) {
	if info == nil || info.GetExecution() == nil {
		return workflowSummaryResponse{}, errWorkflowNotFound
	}
	detail := workflowDetailResponse{
		workflowSummaryResponse: workflowSummaryResponse{
			ID:              info.GetExecution().GetWorkflowId(),
			Type:            workflowTypeSlug(info.GetType().GetName()),
			Status:          workflowStatus(info.GetStatus()),
			ProductID:       memoString(info.GetMemo(), "product_id"),
			ProductTitle:    memoString(info.GetMemo(), "product_title"),
			CurrentActivity: "Temporal execution",
			StartedAt:       formatProtoTimestamp(info.GetStartTime()),
			UpdatedAt:       firstNonEmpty(formatProtoTimestamp(info.GetCloseTime()), formatProtoTimestamp(info.GetStartTime())),
			CompletedAt:     formatProtoTimestamp(info.GetCloseTime()),
		},
		RunID: info.GetExecution().GetRunId(),
	}
	if detail.Type == "product_publish" {
		s.applyProductPublishSnapshot(ctx, &detail, info)
	}
	return detail.workflowSummaryResponse, nil
}

func (s *server) applyProductPublishSnapshot(ctx context.Context, detail *workflowDetailResponse, info *workflowpb.WorkflowExecutionInfo) {
	snapshot, err := s.productPublishSnapshot(ctx, info.GetExecution().GetWorkflowId(), info.GetExecution().GetRunId())
	if err != nil || snapshot == nil {
		return
	}

	detail.Status = productPublishHTTPStatus(snapshot.Status, info.GetStatus())
	detail.ProductID = firstNonEmpty(snapshot.ProductID, detail.ProductID)
	detail.CurrentActivity = firstNonEmpty(snapshot.CurrentActivity, detail.CurrentActivity)
	detail.StartedAt = firstNonEmpty(snapshot.StartedAt, detail.StartedAt)
	detail.UpdatedAt = firstNonEmpty(snapshot.UpdatedAt, detail.UpdatedAt, detail.StartedAt)
	detail.CompletedAt = firstNonEmpty(snapshot.CompletedAt, detail.CompletedAt)
	detail.Activities = mapLifecycleStages(snapshot.Activities)
	detail.Review = workflowReview(snapshot.Review)
	if detail.Review == nil && snapshot.Status == ecworkflow.ProductPublishStatusRejected {
		detail.Review = &workflowReviewResponse{Approved: false}
	}

	for _, stage := range snapshot.Activities {
		if stage.Error != "" {
			detail.Error = stage.Error
			break
		}
	}
	if _, failed := failedLifecycleError(detail.Activities); failed {
		detail.Status = "failed"
	}
}

func (s *server) productPublishSnapshot(ctx context.Context, workflowID, runID string) (*ecworkflow.ProductPublishResult, error) {
	value, err := s.workflowClient.QueryWorkflow(ctx, workflowID, runID, ecworkflow.ProductPublishStatusQuery)
	if err != nil || value == nil || !value.HasValue() {
		return nil, err
	}
	var snapshot ecworkflow.ProductPublishResult
	if err := value.Get(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func mapLifecycleStages(stages []ecworkflow.ProductPublishLifecycleStage) []workflowActivityResponse {
	if len(stages) == 0 {
		return nil
	}
	activities := make([]workflowActivityResponse, 0, len(stages))
	for _, stage := range stages {
		activities = append(activities, workflowActivityResponse{
			ID:          stage.ID,
			Name:        stage.Name,
			Status:      stage.Status,
			StartedAt:   stage.StartedAt,
			CompletedAt: stage.CompletedAt,
			Message:     stage.Message,
			Attempt:     stage.Attempt,
			Error:       stage.Error,
		})
	}
	return activities
}

func (s *server) collectWorkflowSummaries(ctx context.Context, executions []*workflowpb.WorkflowExecutionInfo, statusFilter string) []workflowSummaryResponse {
	workflows := make([]workflowSummaryResponse, 0, len(executions))
	for _, execution := range executions {
		summary, err := s.workflowSummaryFromExecution(ctx, execution)
		if err != nil {
			s.log.Warn("skip workflow summary", "error", err)
			continue
		}
		if statusFilter != "" && summary.Status != statusFilter {
			continue
		}
		workflows = append(workflows, summary)
	}
	return workflows
}

func workflowListRequest(r *http.Request) *workflowservicepb.ListWorkflowExecutionsRequest {
	return &workflowservicepb.ListWorkflowExecutionsRequest{
		PageSize: int32(parseWorkflowLimit(r.URL.Query().Get("limit"))),
		Query:    buildWorkflowVisibilityQuery(r.URL.Query().Get("status")),
	}
}

var workflowTypeSlugs = map[string]string{
	"ProductPublishWorkflow":    "product_publish",
	"ContentGenerationWorkflow": "content_generation",
	"MediaProcessingWorkflow":   "media_processing",
	"SourcingWorkflow":          "sourcing",
	"MarketplaceSyncWorkflow":   "marketplace_sync",
	"MarketplaceReplayWorkflow": "marketplace_replay",
}

func workflowTypeSlug(name string) string {
	if slug, ok := workflowTypeSlugs[name]; ok {
		return slug
	}
	if name == "" {
		return "workflow"
	}
	return strings.ToLower(strings.TrimSuffix(name, "Workflow"))
}

func productPublishHTTPStatus(status string, fallback enumspb.WorkflowExecutionStatus) string {
	switch status {
	case ecworkflow.ProductPublishStatusAwaitingReview:
		return "waiting_review"
	case ecworkflow.ProductPublishStatusPublished:
		return "completed"
	case ecworkflow.ProductPublishStatusComplianceFailed, ecworkflow.ProductPublishStatusMediaFailed, ecworkflow.ProductPublishStatusRejected, ecworkflow.ProductPublishStatusFailed:
		return "failed"
	case ecworkflow.ProductPublishStatusPublishing, ecworkflow.ProductPublishStatusDraft:
		return "running"
	case "":
		return workflowStatus(fallback)
	default:
		return status
	}
}

func workflowActivityStatus(status enumspb.WorkflowExecutionStatus) string {
	switch status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "completed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED, enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED, enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		return "running"
	default:
		return "pending"
	}
}

func parseWorkflowLimit(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 50
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func buildWorkflowVisibilityQuery(status string) string {
	switch strings.TrimSpace(status) {
	case "running", "waiting_review":
		return "ExecutionStatus='Running'"
	case "completed":
		return "ExecutionStatus='Completed'"
	case "failed":
		return "ExecutionStatus='Failed'"
	default:
		return ""
	}
}

func formatProtoTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}

func memoString(memo *commonpb.Memo, key string) string {
	if memo == nil || memo.GetFields() == nil {
		return ""
	}
	payload := memo.GetFields()[key]
	if payload == nil {
		return ""
	}
	var value string
	if err := converter.GetDefaultDataConverter().FromPayload(payload, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func workflowReview(review ecworkflow.ReviewSignal) *workflowReviewResponse {
	reviewer := strings.TrimSpace(review.Reviewer)
	note := strings.TrimSpace(review.Note)
	if !review.Approved && reviewer == "" && note == "" {
		return nil
	}
	return &workflowReviewResponse{
		Approved: review.Approved,
		Reviewer: reviewer,
		Note:     note,
	}
}

func failedLifecycleError(activities []workflowActivityResponse) (string, bool) {
	for _, activity := range activities {
		if activity.Status != "failed" {
			continue
		}
		return firstNonEmpty(activity.Error, activity.Message), true
	}
	return "", false
}

// tenantOf resolves the tenant for the fallback approval row (empty for the
// platform tenant).
func (s *server) tenantOf(r *http.Request) string { return "" }

// workflowIDProductID extracts the product segment from a publish workflow
// id (the workflow-id contract carries it); empty when unparseable.
func workflowIDProductID(workflowID string) string {
	// Publish workflow ids are product-publish-<productID>-<fingerprint>.
	const prefix = "product-publish-"
	if !strings.HasPrefix(workflowID, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(workflowID, prefix)
	if i := strings.LastIndex(rest, "-"); i > 0 {
		return rest[:i]
	}
	return rest
}
