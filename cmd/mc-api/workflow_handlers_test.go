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
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/nfsarch33/agentic-ecommerce/internal/security"
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

func TestStartContentGenerationWorkflowStartsTemporalExecution(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "CW-START", "Content Workflow Product", 1295)
	fake := &fakeTemporalWorkflowClient{run: fakeWorkflowRun{id: "content-generation-" + product.ID().String(), runID: "run-content-123"}}
	srv.workflowClient = fake

	body := bytes.NewBufferString(`{"product_id":"` + product.ID().String() + `","requested_by":"operator@example.com","style":"technical","max_words":90}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/content-generation", body)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if fake.startedOptions.TaskQueue != ecworkflow.TaskQueue {
		t.Fatalf("task queue = %q, want %q", fake.startedOptions.TaskQueue, ecworkflow.TaskQueue)
	}
	input, ok := fake.startedArgs[0].(ecworkflow.ContentGenerationInput)
	if !ok {
		t.Fatalf("workflow arg = %#v, want ContentGenerationInput", fake.startedArgs)
	}
	if input.Product.ID != product.ID().String() || input.Request.Style != "technical" || input.Request.MaxWords != 90 {
		t.Fatalf("input = %+v", input)
	}
}

func TestStartMediaProcessingWorkflowStartsTemporalExecution(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	fake := &fakeTemporalWorkflowClient{run: fakeWorkflowRun{id: "media-processing-product-123", runID: "run-media-123"}}
	srv.workflowClient = fake

	body := bytes.NewBufferString(`{"product_id":"product-123","source_url":"https://supplier.example/images/lamp.png","alt_text":"Matte black desk lamp on white background","requested_by":"operator@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/media-processing", body)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if fake.startedOptions.TaskQueue != ecworkflow.TaskQueue {
		t.Fatalf("task queue = %q, want %q", fake.startedOptions.TaskQueue, ecworkflow.TaskQueue)
	}
	input, ok := fake.startedArgs[0].(ecworkflow.MediaProcessingInput)
	if !ok {
		t.Fatalf("workflow arg = %#v, want MediaProcessingInput", fake.startedArgs)
	}
	if input.ProductID != "product-123" || input.SourceURL != "https://supplier.example/images/lamp.png" || input.AltText == "" {
		t.Fatalf("input = %+v", input)
	}
}

func TestStartSourcingWorkflowStartsTemporalExecution(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	fake := &fakeTemporalWorkflowClient{run: fakeWorkflowRun{id: "sourcing-RB-SET", runID: "run-sourcing-123"}}
	srv.workflowClient = fake

	body := bytes.NewBufferString(`{
		"sku":"RB-SET",
		"estimated_sell_price_cents":4995,
		"minimum_margin_pct":0.32,
		"requested_by":"operator@example.com",
		"candidates":[
			{"supplier_id":"balanced","sku":"RB-SET","unit_cost_cents":1500,"shipping_cents":250,"estimated_sell_price_cents":4995,"lead_time_days":7,"reliability_score":0.92,"demand_score":0.82,"competition_score":0.35}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/sourcing", body)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if fake.startedOptions.TaskQueue != ecworkflow.TaskQueue {
		t.Fatalf("task queue = %q, want %q", fake.startedOptions.TaskQueue, ecworkflow.TaskQueue)
	}
	input, ok := fake.startedArgs[0].(ecworkflow.SourcingWorkflowInput)
	if !ok {
		t.Fatalf("workflow arg = %#v, want SourcingWorkflowInput", fake.startedArgs)
	}
	if input.Search.SKU != "RB-SET" || input.MinimumMarginPct != 0.32 || len(input.Search.Candidates) != 1 {
		t.Fatalf("input = %+v", input)
	}
	if input.Search.Candidates[0].SupplierID != "balanced" {
		t.Fatalf("candidate = %+v", input.Search.Candidates[0])
	}
}

func TestStartMarketplaceSyncWorkflowStartsTemporalExecution(t *testing.T) {
	t.Parallel()

	srv, _ := testServerWithCfg(t, workflowAuthServerConfig())
	srv.configureSecurity()
	fake := &fakeTemporalWorkflowClient{run: fakeWorkflowRun{id: "marketplace-sync-shopify-sku-123", runID: "run-marketplace-sync-123"}}
	srv.workflowClient = fake

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/marketplace-sync", bytes.NewBufferString(`{
		"event":{
			"tenant_id":"tenant-a",
			"provider":"shopify",
			"entity_type":"product",
			"entity_id":"sku-123",
			"external_id":"gid://shopify/Product/1",
			"operation":"upsert",
			"version":"v1",
			"payload":{"title":"Resistance Band Set","description":"Five resistance levels","price":49.95,"stock":12}
		}
	}`))
	req.Header.Set("Authorization", "Bearer "+mintTestAccessToken(t, srv, "operator@example.com", security.RoleOperator))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if fake.startedOptions.TaskQueue != ecworkflow.TaskQueue {
		t.Fatalf("task queue = %q, want %q", fake.startedOptions.TaskQueue, ecworkflow.TaskQueue)
	}
	input, ok := fake.startedArgs[0].(ecworkflow.MarketplaceSyncInput)
	if !ok {
		t.Fatalf("workflow arg = %#v, want MarketplaceSyncInput", fake.startedArgs)
	}
	if input.Event.Provider != "shopify" || input.Event.EntityID != "sku-123" || input.Event.Version != "v1" {
		t.Fatalf("input = %+v", input)
	}
}

func TestStartMarketplaceReplayWorkflowStartsTemporalExecution(t *testing.T) {
	t.Parallel()

	srv, _ := testServerWithCfg(t, workflowAuthServerConfig())
	srv.configureSecurity()
	fake := &fakeTemporalWorkflowClient{run: fakeWorkflowRun{id: "marketplace-replay-dlq-123", runID: "run-marketplace-replay-123"}}
	srv.workflowClient = fake

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/marketplace-replay", bytes.NewBufferString(`{
		"record":{
			"id":"dlq-123",
			"attempts":3,
			"reason":"transient timeout",
			"event":{
				"tenant_id":"tenant-a",
				"provider":"shopify",
				"entity_type":"product",
				"entity_id":"sku-123",
				"external_id":"gid://shopify/Product/1",
				"operation":"upsert",
				"version":"v1",
				"payload":{"title":"Resistance Band Set","description":"Five resistance levels","price":49.95,"stock":12}
			}
		}
	}`))
	req.Header.Set("Authorization", "Bearer "+mintTestAccessToken(t, srv, "operator@example.com", security.RoleOperator))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	input, ok := fake.startedArgs[0].(ecworkflow.MarketplaceReplayInput)
	if !ok {
		t.Fatalf("workflow arg = %#v, want MarketplaceReplayInput", fake.startedArgs)
	}
	if input.Record.ID != "dlq-123" || input.Record.Event.Provider != "shopify" {
		t.Fatalf("input = %+v", input)
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

func TestMarketplaceWorkflowRoutesRequireOperatorRole(t *testing.T) {
	t.Parallel()

	srv, _ := testServerWithCfg(t, workflowAuthServerConfig())
	srv.configureSecurity()
	srv.workflowClient = &fakeTemporalWorkflowClient{run: fakeWorkflowRun{id: "marketplace-sync", runID: "run-1"}}

	body := bytes.NewBufferString(`{"event":{"tenant_id":"tenant-a","provider":"shopify","entity_type":"product","entity_id":"sku-123","operation":"upsert","version":"v1"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/marketplace-sync", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}

	viewerReq := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/marketplace-sync", bytes.NewBufferString(`{"event":{"tenant_id":"tenant-a","provider":"shopify","entity_type":"product","entity_id":"sku-123","operation":"upsert","version":"v1"}}`))
	viewerReq.Header.Set("Content-Type", "application/json")
	viewerReq.Header.Set("Authorization", "Bearer "+mintTestAccessToken(t, srv, "viewer@example.com", security.RoleViewer))
	viewerRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(viewerRec, viewerReq)
	if viewerRec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", viewerRec.Code, viewerRec.Body.String())
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
	var got workflowDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != "wf-123" || got.RunID != "run-123" || got.Status != "running" {
		t.Fatalf("response = %+v", got)
	}
	if got.Type != "workflow" {
		t.Fatalf("type = %q, want workflow", got.Type)
	}
	if got.StartedAt != start.Format(time.RFC3339) {
		t.Fatalf("started_at = %q, want %s", got.StartedAt, start.Format(time.RFC3339))
	}
	if len(got.Activities) != 1 || got.Activities[0].Name != "Temporal execution" {
		t.Fatalf("activities = %+v, want one temporal execution activity", got.Activities)
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
	marketplaceStart := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/marketplace-sync", nil)
	if got := workflowAuditAction(marketplaceStart); got.Action != "workflow.marketplace_sync.start" || got.Resource != "marketplace-sync" || !got.Mutates {
		t.Fatalf("marketplace start audit = %+v", got)
	}
	marketplaceReplay := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/marketplace-replay", nil)
	if got := workflowAuditAction(marketplaceReplay); got.Action != "workflow.marketplace_replay.start" || got.Resource != "marketplace-replay" || !got.Mutates {
		t.Fatalf("marketplace replay audit = %+v", got)
	}
	read := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/wf-123", nil)
	if got := workflowAuditAction(read); got.Action != "" || got.Mutates {
		t.Fatalf("read audit = %+v, want empty", got)
	}
}

func workflowAuthServerConfig() serverConfig {
	return serverConfig{
		jwtSecret:    "test-secret-at-least-32-bytes-long",
		jwtIssuer:    "agentic-ecommerce",
		jwtAudience:  "mc-api",
		jwtAccessTTL: 15 * time.Minute,
		refreshTTL:   24 * time.Hour,
	}
}

type fakeTemporalWorkflowClient struct {
	run               fakeWorkflowRun
	describe          *workflowservicepb.DescribeWorkflowExecutionResponse
	describeErr       error
	list              *workflowservicepb.ListWorkflowExecutionsResponse
	listErr           error
	queryByWorkflowID map[string]converter.EncodedValue
	queryErr          error
	startErr          error
	signalErr         error
	startedOptions    client.StartWorkflowOptions
	startedWorkflow   any
	startedArgs       []any
	signalWorkflowID  string
	signalRunID       string
	signalName        string
	signalArg         any
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

func (f *fakeTemporalWorkflowClient) ListWorkflow(context.Context, *workflowservicepb.ListWorkflowExecutionsRequest) (*workflowservicepb.ListWorkflowExecutionsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeTemporalWorkflowClient) QueryWorkflow(_ context.Context, workflowID, _ string, _ string, _ ...interface{}) (converter.EncodedValue, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.queryByWorkflowID == nil {
		return nil, nil
	}
	return f.queryByWorkflowID[workflowID], nil
}

type fakeWorkflowRun struct {
	id    string
	runID string
}

func (r fakeWorkflowRun) GetID() string    { return r.id }
func (r fakeWorkflowRun) GetRunID() string { return r.runID }
