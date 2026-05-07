package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	workflowservicepb "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/protobuf/types/known/timestamppb"

	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
)

func TestStartProductPublishWorkflowStartsTemporalExecution(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "WF-START", "Workflow Start Product", 1295)
	fake := &fakeTemporalWorkflowClient{run: fakeWorkflowRun{id: "product-publish-" + product.ID().String(), runID: "run-123"}}
	srv.workflowClient = fake

	body := bytes.NewBufferString(`{"product_id":"` + product.ID().String() + `","requested_by":"operator@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/product-publish", body)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if fake.startedOptions.TaskQueue != ecworkflow.TaskQueue {
		t.Fatalf("task queue = %q, want %q", fake.startedOptions.TaskQueue, ecworkflow.TaskQueue)
	}
	input, ok := fake.startedArgs[0].(ecworkflow.ProductPublishInput)
	if !ok {
		t.Fatalf("workflow arg = %#v, want ProductPublishInput", fake.startedArgs)
	}
	if input.ProductID != product.ID().String() || input.RequestedBy != "operator@example.com" {
		t.Fatalf("input = %+v", input)
	}
	var got workflowStartResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.WorkflowID != fake.run.id || got.RunID != fake.run.runID || got.Status != "started" {
		t.Fatalf("response = %+v", got)
	}
}

func TestStartProductPublishWorkflowRequiresConfiguredTemporalClient(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "WF-NOCLIENT", "Workflow No Client", 1295)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/product-publish", bytes.NewBufferString(`{"product_id":"`+product.ID().String()+`"}`))
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWorkflowHandlersRejectUnsupportedRoutes(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	srv.workflowClient = &fakeTemporalWorkflowClient{}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workflows/wf-123", nil)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
}

func TestNewTemporalWorkflowClientSkipsEmptyAddress(t *testing.T) {
	t.Parallel()

	client, cleanup := newTemporalWorkflowClient(nil, " ")
	if client != nil || cleanup != nil {
		t.Fatal("client and cleanup should be nil when address is empty")
	}
}

func TestStartProductPublishWorkflowValidatesRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "invalid json", body: `{`, want: http.StatusBadRequest},
		{name: "invalid product id", body: `{"product_id":"not-a-uuid"}`, want: http.StatusBadRequest},
		{name: "missing product", body: `{"product_id":"11111111-1111-1111-1111-111111111111"}`, want: http.StatusNotFound},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := testServer(t)
			srv.workflowClient = &fakeTemporalWorkflowClient{}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/product-publish", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			srv.mux().ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestStartProductPublishWorkflowMapsTemporalStartFailure(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "WF-START-FAIL", "Workflow Start Failure", 1295)
	srv.workflowClient = &fakeTemporalWorkflowClient{startErr: errors.New("temporal unavailable")}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/product-publish", bytes.NewBufferString(`{"product_id":"`+product.ID().String()+`"}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetWorkflowStatusDescribesTemporalExecution(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	srv, _ := testServer(t)
	srv.workflowClient = &fakeTemporalWorkflowClient{describe: &workflowservicepb.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Execution: &commonpb.WorkflowExecution{WorkflowId: "wf-123", RunId: "run-123"},
			Status:    enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
			StartTime: timestamppb.New(start),
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/wf-123", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got workflowStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.WorkflowID != "wf-123" || got.RunID != "run-123" || got.Status != "running" {
		t.Fatalf("response = %+v", got)
	}
	if got.StartTime == nil || !got.StartTime.Equal(start) {
		t.Fatalf("start time = %v, want %v", got.StartTime, start)
	}
}

func TestGetWorkflowStatusRequiresTemporalClient(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/wf-123", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetWorkflowStatusHandlesEmptyTemporalInfo(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	srv.workflowClient = &fakeTemporalWorkflowClient{describe: &workflowservicepb.DescribeWorkflowExecutionResponse{}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/wf-123", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSignalWorkflowReviewSendsTemporalSignal(t *testing.T) {
	t.Parallel()

	fake := &fakeTemporalWorkflowClient{}
	srv, _ := testServer(t)
	srv.workflowClient = fake

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wf-123/signals/review", bytes.NewBufferString(`{"approved":true,"reviewer":"lead@example.com","note":"approved"}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if fake.signalWorkflowID != "wf-123" || fake.signalName != ecworkflow.ProductPublishReviewSignal {
		t.Fatalf("signal target/name = %q/%q", fake.signalWorkflowID, fake.signalName)
	}
	payload, ok := fake.signalArg.(ecworkflow.ReviewSignal)
	if !ok {
		t.Fatalf("signal arg = %#v, want ReviewSignal", fake.signalArg)
	}
	if !payload.Approved || payload.Reviewer != "lead@example.com" {
		t.Fatalf("signal payload = %+v", payload)
	}
}

func TestSignalWorkflowReviewRequiresTemporalClient(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wf-123/signals/review", bytes.NewBufferString(`{"approved":true}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSignalWorkflowReviewValidatesRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		body string
		want int
	}{
		{name: "invalid json", path: "/api/v1/workflows/wf-123/signals/review", body: `{`, want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := testServer(t)
			srv.workflowClient = &fakeTemporalWorkflowClient{}
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			srv.mux().ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestSignalWorkflowReviewMapsTemporalFailure(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	srv.workflowClient = &fakeTemporalWorkflowClient{signalErr: errors.New("signal rejected")}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wf-123/signals/review", bytes.NewBufferString(`{"approved":true}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWorkflowStatusMapsTemporalNotFound(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	srv.workflowClient = &fakeTemporalWorkflowClient{describeErr: errors.New("workflow not found")}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/missing", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for Temporal describe failure", rec.Code)
	}
}

func TestWorkflowStatusNormalizesTemporalEnums(t *testing.T) {
	t.Parallel()

	cases := map[enumspb.WorkflowExecutionStatus]string{
		enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:          "running",
		enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:        "completed",
		enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:           "failed",
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:         "canceled",
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:       "terminated",
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW: "continued_as_new",
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:        "timed_out",
		enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED:      "unspecified",
	}
	for status, want := range cases {
		if got := workflowStatus(status); got != want {
			t.Fatalf("workflowStatus(%s) = %q, want %q", status, got, want)
		}
	}
}

func TestWorkflowAuditActionClassifiesMutations(t *testing.T) {
	t.Parallel()

	start := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/product-publish", nil)
	if got := workflowAuditAction(start); got.Action != "workflow.product_publish.start" || !got.Mutates {
		t.Fatalf("start audit = %+v", got)
	}
	signal := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wf-123/signals/review", nil)
	if got := workflowAuditAction(signal); got.Action != "workflow.product_publish.review_signal" || got.Resource != "wf-123" || !got.Mutates {
		t.Fatalf("signal audit = %+v", got)
	}
	read := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/wf-123", nil)
	if got := workflowAuditAction(read); got.Action != "" || got.Mutates {
		t.Fatalf("read audit = %+v, want empty", got)
	}
}

type fakeTemporalWorkflowClient struct {
	run              fakeWorkflowRun
	describe         *workflowservicepb.DescribeWorkflowExecutionResponse
	describeErr      error
	startErr         error
	signalErr        error
	startedOptions   client.StartWorkflowOptions
	startedWorkflow  any
	startedArgs      []any
	signalWorkflowID string
	signalRunID      string
	signalName       string
	signalArg        any
}

func (f *fakeTemporalWorkflowClient) ExecuteWorkflow(_ context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (workflowRun, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.startedOptions = options
	f.startedWorkflow = workflow
	f.startedArgs = append([]any(nil), args...)
	return f.run, nil
}

func (f *fakeTemporalWorkflowClient) DescribeWorkflowExecution(_ context.Context, workflowID, runID string) (*workflowservicepb.DescribeWorkflowExecutionResponse, error) {
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	return f.describe, nil
}

func (f *fakeTemporalWorkflowClient) SignalWorkflow(_ context.Context, workflowID, runID, signalName string, arg any) error {
	if f.signalErr != nil {
		return f.signalErr
	}
	f.signalWorkflowID = workflowID
	f.signalRunID = runID
	f.signalName = signalName
	f.signalArg = arg
	return nil
}

type fakeWorkflowRun struct {
	id    string
	runID string
}

func (r fakeWorkflowRun) GetID() string    { return r.id }
func (r fakeWorkflowRun) GetRunID() string { return r.runID }
