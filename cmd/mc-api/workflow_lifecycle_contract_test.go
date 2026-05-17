package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	workflowservicepb "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/types/known/timestamppb"

	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
)

func TestListWorkflowsReturnsAuthoritativeWorkflowSummaries(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 17, 10, 3, 0, 0, time.UTC)
	srv, _ := testServer(t)
	srv.workflowClient = &workflowLifecycleClientStub{
		list: &workflowservicepb.ListWorkflowExecutionsResponse{
			Executions: []*workflowpb.WorkflowExecutionInfo{
				{
					Execution: &commonpb.WorkflowExecution{WorkflowId: "wf-123", RunId: "run-123"},
					Type:      &commonpb.WorkflowType{Name: "ProductPublishWorkflow"},
					Status:    enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
					StartTime: timestamppb.New(start),
				},
			},
		},
		queryByWorkflowID: map[string]converter.EncodedValue{
			"wf-123": fakeEncodedValue{value: map[string]any{
				"product_id":       "product-123",
				"status":           ecworkflow.ProductPublishStatusAwaitingReview,
				"current_activity": "Awaiting human review",
				"updated_at":       updated.Format(time.RFC3339),
			}},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows", nil)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		Workflows []map[string]any `json:"workflows"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Workflows) != 1 {
		t.Fatalf("workflows = %d, want 1", len(got.Workflows))
	}
	workflow := got.Workflows[0]
	if workflow["id"] != "wf-123" {
		t.Fatalf("workflow id = %#v, want wf-123", workflow["id"])
	}
	if workflow["type"] != "product_publish" {
		t.Fatalf("workflow type = %#v, want product_publish", workflow["type"])
	}
	if workflow["status"] != "waiting_review" {
		t.Fatalf("workflow status = %#v, want waiting_review", workflow["status"])
	}
	if workflow["product_id"] != "product-123" {
		t.Fatalf("workflow product_id = %#v, want product-123", workflow["product_id"])
	}
	if workflow["current_activity"] != "Awaiting human review" {
		t.Fatalf("workflow current_activity = %#v, want Awaiting human review", workflow["current_activity"])
	}
	if workflow["started_at"] != start.Format(time.RFC3339) {
		t.Fatalf("workflow started_at = %#v, want %s", workflow["started_at"], start.Format(time.RFC3339))
	}
	if workflow["updated_at"] != updated.Format(time.RFC3339) {
		t.Fatalf("workflow updated_at = %#v, want %s", workflow["updated_at"], updated.Format(time.RFC3339))
	}
}

func TestGetWorkflowStatusReturnsAuthoritativeLifecycleDetail(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 17, 11, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 17, 11, 2, 0, 0, time.UTC)
	srv, _ := testServer(t)
	srv.workflowClient = &workflowLifecycleClientStub{
		describe: &workflowservicepb.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{WorkflowId: "wf-123", RunId: "run-123"},
				Type:      &commonpb.WorkflowType{Name: "ProductPublishWorkflow"},
				Status:    enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
				StartTime: timestamppb.New(start),
			},
		},
		queryByWorkflowID: map[string]converter.EncodedValue{
			"wf-123": fakeEncodedValue{value: map[string]any{
				"product_id":       "product-123",
				"status":           ecworkflow.ProductPublishStatusAwaitingReview,
				"current_activity": "Awaiting human review",
				"updated_at":       updated.Format(time.RFC3339),
				"activities": []map[string]any{
					{
						"id":           "review",
						"name":         "Human review",
						"status":       "waiting_review",
						"started_at":   updated.Format(time.RFC3339),
						"message":      "Awaiting operator decision",
						"attempt":      1,
						"completed_at": "",
					},
				},
			}},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/wf-123", nil)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["id"] != "wf-123" {
		t.Fatalf("detail id = %#v, want wf-123", got["id"])
	}
	if got["type"] != "product_publish" {
		t.Fatalf("detail type = %#v, want product_publish", got["type"])
	}
	if got["status"] != "waiting_review" {
		t.Fatalf("detail status = %#v, want waiting_review", got["status"])
	}
	if got["product_id"] != "product-123" {
		t.Fatalf("detail product_id = %#v, want product-123", got["product_id"])
	}
	if got["current_activity"] != "Awaiting human review" {
		t.Fatalf("detail current_activity = %#v, want Awaiting human review", got["current_activity"])
	}
	activities, ok := got["activities"].([]any)
	if !ok || len(activities) != 1 {
		t.Fatalf("detail activities = %#v, want 1 activity", got["activities"])
	}
}

func TestSignalWorkflowReviewReturnsObservedWorkflowSnapshot(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 17, 12, 4, 0, 0, time.UTC)
	srv, _ := testServer(t)
	srv.workflowClient = &workflowLifecycleClientStub{
		describe: &workflowservicepb.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{WorkflowId: "wf-123", RunId: "run-123"},
				Type:      &commonpb.WorkflowType{Name: "ProductPublishWorkflow"},
				Status:    enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
				StartTime: timestamppb.New(start),
			},
		},
		queryByWorkflowID: map[string]converter.EncodedValue{
			"wf-123": fakeEncodedValue{value: map[string]any{
				"product_id":       "product-123",
				"status":           ecworkflow.ProductPublishStatusPublishing,
				"current_activity": "Publish to WooCommerce",
				"updated_at":       updated.Format(time.RFC3339),
				"activities": []map[string]any{
					{
						"id":         "publish",
						"name":       "Publish to WooCommerce",
						"status":     "running",
						"started_at": updated.Format(time.RFC3339),
						"attempt":    1,
					},
				},
			}},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wf-123/signals/review", bytes.NewBufferString(`{"approved":true,"reviewer":"lead@example.com","note":"ready"}`))
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["status"] != "signaled" {
		t.Fatalf("status = %#v, want signaled", got["status"])
	}
	workflow, ok := got["workflow"].(map[string]any)
	if !ok {
		t.Fatalf("workflow snapshot = %#v, want object", got["workflow"])
	}
	if workflow["id"] != "wf-123" {
		t.Fatalf("workflow id = %#v, want wf-123", workflow["id"])
	}
	if workflow["current_activity"] != "Publish to WooCommerce" {
		t.Fatalf("workflow current_activity = %#v, want Publish to WooCommerce", workflow["current_activity"])
	}
}

type workflowLifecycleClientStub struct {
	describe          *workflowservicepb.DescribeWorkflowExecutionResponse
	describeErr       error
	list              *workflowservicepb.ListWorkflowExecutionsResponse
	listErr           error
	queryByWorkflowID map[string]converter.EncodedValue
	queryErr          error
	signalErr         error
}

func (f *workflowLifecycleClientStub) ExecuteWorkflow(context.Context, client.StartWorkflowOptions, any, ...any) (workflowRun, error) {
	return fakeWorkflowRun{}, nil
}

func (f *workflowLifecycleClientStub) DescribeWorkflowExecution(context.Context, string, string) (*workflowservicepb.DescribeWorkflowExecutionResponse, error) {
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	return f.describe, nil
}

func (f *workflowLifecycleClientStub) SignalWorkflow(context.Context, string, string, string, any) error {
	return f.signalErr
}

func (f *workflowLifecycleClientStub) ListWorkflow(context.Context, *workflowservicepb.ListWorkflowExecutionsRequest) (*workflowservicepb.ListWorkflowExecutionsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *workflowLifecycleClientStub) QueryWorkflow(_ context.Context, workflowID string, _ string, _ string, _ ...interface{}) (converter.EncodedValue, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.queryByWorkflowID == nil {
		return nil, nil
	}
	return f.queryByWorkflowID[workflowID], nil
}

type fakeEncodedValue struct {
	value any
}

func (f fakeEncodedValue) HasValue() bool { return f.value != nil }

func (f fakeEncodedValue) Get(valuePtr interface{}) error {
	bytes, err := json.Marshal(f.value)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, valuePtr)
}
