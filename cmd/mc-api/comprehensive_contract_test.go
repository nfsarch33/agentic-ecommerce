package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	"github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync"
	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
	"github.com/nfsarch33/agentic-ecommerce/internal/rag"
	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
	"github.com/nfsarch33/agentic-ecommerce/internal/webhook/outbound"
	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	workflowservicepb "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestOpenAPIOperationsHaveComprehensiveContractCoverage(t *testing.T) {
	t.Parallel()

	spec := loadOpenAPISpec(t)
	paths := specMap(t, spec, "paths")
	covered := map[string]bool{
		"checkProductCompliance":           true,
		"createOrder":                      true,
		"createCustomComplianceRule":       true,
		"createProduct":                    true,
		"createWebhook":                    true,
		"deleteCustomComplianceRule":       true,
		"deleteProduct":                    true,
		"deleteWebhook":                    true,
		"disableAgentSchedule":             true,
		"enableAgentSchedule":              true,
		"exportComplianceReport":           true,
		"generateFactCheckedContent":       true,
		"generateProductDescription":       true,
		"getCart":                          true,
		"getComplianceReportSummary":       true,
		"getCurrentSession":                true,
		"getFactCheckResult":               true,
		"getAgentRun":                      true,
		"getHealthz":                       true,
		"getMedia":                         true,
		"getMetrics":                       true,
		"getOrder":                         true,
		"getProduct":                       true,
		"getProductAISuggestions":          true,
		"getReadyz":                        true,
		"getSyncStatus":                    true,
		"getTenantSettings":                true,
		"listWorkflows":                    true,
		"getWorkflowStatus":                true,
		"ingestRAGDocument":                true,
		"listAgentHistory":                 true,
		"listAgentSchedules":               true,
		"listAgents":                       true,
		"listComplianceRules":              true,
		"listCustomComplianceRules":        true,
		"listMedia":                        true,
		"listMarketplaceDLQ":               true,
		"listProducts":                     true,
		"listRecentEvents":                 true,
		"listSyncConflicts":                true,
		"listWebhooks":                     true,
		"login":                            true,
		"logout":                           true,
		"approveMedia":                     true,
		"processMedia":                     true,
		"productsPreflight":                true,
		"publishProductToWooCommerce":      true,
		"putCart":                          true,
		"receiveWooCommerceOrderWebhook":   true,
		"receiveWooCommerceProductWebhook": true,
		"refreshAccessToken":               true,
		"resolveOperatorAlert":             true,
		"rejectMedia":                      true,
		"resolveSyncConflict":              true,
		"replayMarketplaceDLQ":             true,
		"runAgent":                         true,
		"searchRAGEvidence":                true,
		"signalProductPublishReview":       true,
		"sourceMedia":                      true,
		"startContentGenerationWorkflow":   true,
		"startMarketplaceReplayWorkflow":   true,
		"startMarketplaceSyncWorkflow":     true,
		"startMediaProcessingWorkflow":     true,
		"startProductPublishWorkflow":      true,
		"startSourcingWorkflow":            true,
		"suggestProductSEO":                true,
		"testWebhook":                      true,
		"updateOrderStatus":                true,
		"updateCustomComplianceRule":       true,
		"updateProduct":                    true,
		"updateTenantSettings":             true,
		"validateMedia":                    true,
		"cancelMembership":                 true,
		"createMembership":                 true,
		"createMembershipPlan":             true,
		"deleteMembershipPlan":             true,
		"getMembership":                    true,
		"getMembershipPlan":                true,
		"listMemberships":                  true,
		"listMembershipPlans":              true,
		"listOperatorAlerts":               true,
		"pauseMembership":                  true,
		"resumeMembership":                 true,
		"updateMembership":                 true,
		"updateMembershipPlan":             true,
		// v2.3.0 Digital goods bounded context.
		"adminDigitalProductDownload": true,
		"createDigitalProduct":        true,
		"createLicense":               true,
		"customerDigitalDownload":     true,
		"deleteDigitalProduct":        true,
		"getDigitalProduct":           true,
		"getLicense":                  true,
		"listDigitalProducts":         true,
		"listLicenses":                true,
		"listMyLicenses":              true,
		"revokeLicense":               true,
		"updateDigitalProduct":        true,
		// v2.4.0 Marketplace plugin framework + tenant aggregate.
		"listMarketplacePlugins":                true,
		"getMarketplacePlugin":                  true,
		"installMarketplacePlugin":              true,
		"activateMarketplacePlugin":             true,
		"acknowledgeOperatorAlert":              true,
		"deactivateMarketplacePlugin":           true,
		"uninstallMarketplacePlugin":            true,
		"submitMarketplacePlugin":               true,
		"listMarketplaceSubmissions":            true,
		"getMarketplaceSubmission":              true,
		"approveMarketplaceSubmission":          true,
		"rejectMarketplaceSubmission":           true,
		"getMarketplaceInstallationSettings":    true,
		"updateMarketplaceInstallationSettings": true,
		"listTenants":                           true,
		"createTenant":                          true,
		"getTenant":                             true,
		"updateTenant":                          true,
		"suspendTenant":                         true,
		"activateTenant":                        true,
		"archiveTenant":                         true,
		// v2.5.0 Tenant self-service registration + billing.
		"submitRegistration":             true,
		"verifyRegistration":             true,
		"completeRegistrationOnboarding": true,
		"listBillingSubscriptions":       true,
		"getBillingSubscription":         true,
		"cancelBillingSubscription":      true,
		"pauseBillingSubscription":       true,
		"resumeBillingSubscription":      true,
		"listBillingInvoices":            true,
		"getBillingInvoice":              true,
		"getBillingUsage":                true,
		"stripeWebhook":                  true,
		// v4.2.0 Payment system.
		"listPayments": true,
		// v3.6.0 EC-9 analytics + agent activity surfaces.
		"getGMVDailyRollup":   true,
		"getGMVByChannel":     true,
		"getGMVByProduct":     true,
		"streamAgentActivity": true,
	}

	var seen []string
	for path, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			t.Fatalf("path item %s is not an object", path)
		}
		for _, method := range []string{"get", "post", "put", "patch", "delete", "options"} {
			rawOperation, ok := pathItem[method]
			if !ok {
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				t.Fatalf("%s %s operation is not an object", method, path)
			}
			operationID, ok := operation["operationId"].(string)
			if !ok || operationID == "" {
				t.Fatalf("%s %s missing operationId", method, path)
			}
			seen = append(seen, operationID)
			if !covered[operationID] {
				t.Fatalf("%s %s operationId %q has no contract coverage entry", method, path, operationID)
			}
		}
	}
	sort.Strings(seen)
	if len(seen) != len(covered) {
		t.Fatalf("OpenAPI operation coverage count = %d, want %d; seen=%v", len(seen), len(covered), seen)
	}
}

func TestRepresentativeGoldenJSONResponseShapes(t *testing.T) {
	spec := loadOpenAPIContract(t)
	srv, repo := testServer(t)
	product := fixedProduct(t)
	if err := repo.Create(context.Background(), product); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	seedContractDependencies(t, srv, product.ID().String())

	for _, tc := range []struct {
		name       string
		method     string
		path       string
		body       string
		statusCode int
		specPath   string
		specStatus string
		golden     string
	}{
		{name: "healthz", method: http.MethodGet, path: "/healthz", statusCode: http.StatusOK, specPath: "/healthz", specStatus: "200", golden: "healthz.golden.json"},
		{name: "readyz", method: http.MethodGet, path: "/readyz", statusCode: http.StatusOK, specPath: "/readyz", specStatus: "200", golden: "readyz.golden.json"},
		{name: "product_list", method: http.MethodGet, path: "/api/v1/products", statusCode: http.StatusOK, specPath: "/api/v1/products", specStatus: "200", golden: "product_list.golden.json"},
		{name: "product_detail", method: http.MethodGet, path: "/api/v1/products/" + product.Slug(), statusCode: http.StatusOK, specPath: "/api/v1/products/{id}", specStatus: "200", golden: "product_detail.golden.json"},
		{name: "cart", method: http.MethodPut, path: "/api/v1/cart/session-contract", body: `{"items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":2,"unit_price":{"amount":2495,"currency":"AUD"}}]}`, statusCode: http.StatusOK, specPath: "/api/v1/cart/{session_id}", specStatus: "200", golden: "cart.golden.json"},
		{name: "compliance_rules", method: http.MethodGet, path: "/api/v1/compliance/rules", statusCode: http.StatusOK, specPath: "/api/v1/compliance/rules", specStatus: "200", golden: "compliance_rules.golden.json"},
		{name: "agents", method: http.MethodGet, path: "/api/v1/agents", statusCode: http.StatusOK, specPath: "/api/v1/agents", specStatus: "200", golden: "agents.golden.json"},
		{name: "agent_schedules", method: http.MethodGet, path: "/api/v1/agent-schedules", statusCode: http.StatusOK, specPath: "/api/v1/agent-schedules", specStatus: "200", golden: "agent_schedules.golden.json"},
		{name: "events", method: http.MethodGet, path: "/api/v1/events/recent?limit=1", statusCode: http.StatusOK, specPath: "/api/v1/events/recent", specStatus: "200", golden: "events.golden.json"},
		{name: "rag_ingest", method: http.MethodPost, path: "/api/v1/rag/documents", body: `{"id":"doc-contract","title":"Contract Doc","source":"contract","content":"Resistance Band Set includes five resistance levels.","metadata":{"sku":"RB-SET"}}`, statusCode: http.StatusCreated, specPath: "/api/v1/rag/documents", specStatus: "201", golden: "rag_ingest.golden.json"},
		{name: "rag_search", method: http.MethodGet, path: "/api/v1/rag/search?q=five%20resistance%20levels&top_k=1", statusCode: http.StatusOK, specPath: "/api/v1/rag/search", specStatus: "200", golden: "rag_search.golden.json"},
		{name: "content_generate", method: http.MethodPost, path: "/api/v1/content/generate", body: `{"product_id":"` + product.ID().String() + `","max_words":80}`, statusCode: http.StatusOK, specPath: "/api/v1/content/generate", specStatus: "200", golden: "content_generate.golden.json"},
		{name: "workflow_start", method: http.MethodPost, path: "/api/v1/workflows/product-publish", body: `{"product_id":"` + product.ID().String() + `","requested_by":"operator@example.com"}`, statusCode: http.StatusAccepted, specPath: "/api/v1/workflows/product-publish", specStatus: "202", golden: "workflow_start.golden.json"},
		{name: "workflow_list", method: http.MethodGet, path: "/api/v1/workflows", statusCode: http.StatusOK, specPath: "/api/v1/workflows", specStatus: "200", golden: "workflow_list.golden.json"},
		{name: "workflow_marketplace_sync_start", method: http.MethodPost, path: "/api/v1/workflows/marketplace-sync", body: `{"event":{"tenant_id":"tenant-contract","provider":"shopify","entity_type":"product","entity_id":"sku-contract","external_id":"gid://shopify/Product/1","operation":"upsert","version":"v1","payload":{"title":"Resistance Band Set","description":"Five resistance levels","price":49.95,"stock":12}}}`, statusCode: http.StatusAccepted, specPath: "/api/v1/workflows/marketplace-sync", specStatus: "202", golden: "workflow_marketplace_sync_start.golden.json"},
		{name: "workflow_marketplace_replay_start", method: http.MethodPost, path: "/api/v1/workflows/marketplace-replay", body: `{"record":{"id":"dlq-contract-1","attempts":3,"reason":"transient timeout","event":{"tenant_id":"tenant-contract","provider":"shopify","entity_type":"product","entity_id":"sku-contract","external_id":"gid://shopify/Product/1","operation":"upsert","version":"v1","payload":{"title":"Resistance Band Set","description":"Five resistance levels","price":49.95,"stock":12}}}}`, statusCode: http.StatusAccepted, specPath: "/api/v1/workflows/marketplace-replay", specStatus: "202", golden: "workflow_marketplace_replay_start.golden.json"},
		{name: "workflow_status", method: http.MethodGet, path: "/api/v1/workflows/wf-contract", statusCode: http.StatusOK, specPath: "/api/v1/workflows/{id}", specStatus: "200", golden: "workflow_status.golden.json"},
		{name: "workflow_signal", method: http.MethodPost, path: "/api/v1/workflows/wf-contract/signals/review", body: `{"approved":true,"reviewer":"lead@example.com"}`, statusCode: http.StatusAccepted, specPath: "/api/v1/workflows/{id}/signals/review", specStatus: "202", golden: "workflow_signal.golden.json"},
		{name: "sync_dlq_list", method: http.MethodGet, path: "/api/v1/sync/dlq", statusCode: http.StatusOK, specPath: "/api/v1/sync/dlq", specStatus: "200", golden: "sync_dlq_list.golden.json"},
		{name: "sync_dlq_replay", method: http.MethodPost, path: "/api/v1/sync/dlq/dlq-contract-1/replay", statusCode: http.StatusAccepted, specPath: "/api/v1/sync/dlq/{id}/replay", specStatus: "202", golden: "sync_dlq_replay.golden.json"},
		{name: "webhook_registration", method: http.MethodPost, path: "/api/v1/webhooks", body: `{"url":"https://hooks.example.test/product","event_types":["product.created"],"secret":"super-secret-webhook-key","secret_ref":"local:test"}`, statusCode: http.StatusCreated, specPath: "/api/v1/webhooks", specStatus: "201", golden: "webhook_registration.golden.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			srv.mux().ServeHTTP(rec, req)
			if rec.Code != tc.statusCode {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.statusCode, rec.Body.String())
			}
			var payload map[string]any
			decodeJSONPayload(t, rec.Body.Bytes(), &payload)
			assertSchemaRequiredFields(t, spec, responseSchema(t, spec, tc.specPath, tc.method, tc.specStatus), payload)
			assertOrUpdateContractGolden(t, filepath.Join("testdata", "contracts", tc.golden), normalizeContractPayload(payload))
		})
	}
}

func assertOrUpdateContractGolden(t *testing.T, goldenPath string, payload any) {
	t.Helper()
	if os.Getenv("UPDATE_CONTRACT_GOLDENS") == "1" {
		raw, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			t.Fatalf("marshal golden JSON: %v", err)
		}
		raw = append(raw, '\n')
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, raw, 0o644); err != nil {
			t.Fatalf("write golden JSON: %v", err)
		}
		return
	}
	assertGoldenJSONPayload(t, goldenPath, payload)
}

func seedContractDependencies(t *testing.T, srv *server, productID string) {
	t.Helper()
	srv.eventBus = eventbus.NewInMemoryBus()
	_ = srv.eventBus.Publish(context.Background(), eventbus.Event{
		ID:        "evt-contract",
		Type:      eventbus.ProductCreated,
		TenantID:  "default",
		Source:    "contract-test",
		Timestamp: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"sku": "RB-SET-5"},
	})
	webhookStore := outbound.NewInMemoryStore()
	srv.webhookService = outbound.NewService(outbound.ServiceConfig{
		Store:  webhookStore,
		Client: outbound.NewClient(outbound.ClientConfig{MaxAttempts: 1}),
	})
	srv.rag = rag.NewService(rag.NewHashEmbedder(16), rag.NewInMemoryVectorStore(16), rag.ChunkOptions{MaxWords: 24})
	if _, err := srv.rag.Ingest(context.Background(), rag.Document{
		ID:      "doc-seed",
		Title:   "Seed Doc",
		Source:  "contract",
		Content: "Resistance Band Set includes five resistance levels for progressive workouts.",
	}); err != nil {
		t.Fatalf("seed rag: %v", err)
	}
	srv.contentAgent = &fakeContentAgent{result: content.GenerateResult{
		GeneratedContent: content.GeneratedContent{
			Description:     "Resistance Band Set includes five resistance levels.",
			SEOTitle:        "Resistance Band Set",
			MetaDescription: "Resistance Band Set includes five resistance levels.",
		},
		Evaluation: content.Evaluation{Score: 90, Pass: true},
		TokensUsed: 42,
	}}
	srv.factChecker = content.NewFactChecker(srv.rag, content.FactCheckOptions{MinConfidence: 0.6, TopK: 1})
	srv.mediaService = intelligence.NewService(intelligence.ServiceConfig{HTTPClient: mediaRoundTripClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       ioNopCloserString(mediaOnePixelPNGString()),
		}, nil
	})})
	srv.syncEngine = enginesync.NewEngine(enginesync.Config{
		ProductRepository: srv.repo,
		WooCommerce:       &syncFakeWooCommerce{products: []woocommerce.Product{}},
		DefaultCurrency:   "AUD",
	})
	dlq := marketplacesync.NewInMemoryDLQ()
	router, err := marketplacesync.NewRouter(marketplacesync.RouterConfig{
		Connectors:  map[string]marketplacesync.Connector{},
		Ledger:      marketplacesync.NewInMemoryLedger(),
		DLQ:         dlq,
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("marketplace sync router: %v", err)
	}
	if err := dlq.Enqueue(context.Background(), marketplacesync.DLQRecord{
		ID:       "dlq-contract-1",
		Attempts: 3,
		Reason:   "transient timeout",
		Event: marketplacesync.ProductEvent{
			TenantID:   "tenant-contract",
			Provider:   "shopify",
			EntityType: marketplacesync.EntityProduct,
			EntityID:   "sku-contract",
			ExternalID: "gid://shopify/Product/1",
			Operation:  marketplacesync.OperationUpsert,
			Version:    "v1",
			Payload: map[string]any{
				"title":       "Resistance Band Set",
				"description": "Five resistance levels",
				"price":       49.95,
				"stock":       12,
			},
		},
	}); err != nil {
		t.Fatalf("seed marketplace dlq: %v", err)
	}
	srv.marketplaceSync = router
	srv.workflowClient = &fakeTemporalWorkflowClient{
		run: fakeWorkflowRun{id: "product-publish-" + productID, runID: "run-contract"},
		list: &workflowservicepb.ListWorkflowExecutionsResponse{
			Executions: []*workflowpb.WorkflowExecutionInfo{
				{
					Execution: &commonpb.WorkflowExecution{WorkflowId: "wf-contract", RunId: "run-contract"},
					Type:      &commonpb.WorkflowType{Name: "ProductPublishWorkflow"},
					Status:    enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
					StartTime: timestamppb.New(time.Date(2026, 5, 7, 12, 30, 0, 0, time.UTC)),
				},
			},
		},
		describe: &workflowservicepb.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{WorkflowId: "wf-contract", RunId: "run-contract"},
				Type:      &commonpb.WorkflowType{Name: "ProductPublishWorkflow"},
				Status:    enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
				StartTime: timestamppb.New(time.Date(2026, 5, 7, 12, 30, 0, 0, time.UTC)),
			},
		},
		queryByWorkflowID: map[string]converter.EncodedValue{
			"wf-contract": fakeEncodedValue{value: map[string]any{
				"product_id":       productID,
				"status":           ecworkflow.ProductPublishStatusAwaitingReview,
				"current_activity": "Awaiting human review",
				"started_at":       "2026-05-07T12:30:00Z",
				"updated_at":       "2026-05-07T12:32:00Z",
				"activities": []map[string]any{
					{
						"id":           "check_compliance",
						"name":         "Check compliance",
						"status":       "completed",
						"started_at":   "2026-05-07T12:30:00Z",
						"completed_at": "2026-05-07T12:30:30Z",
						"message":      "Compliance check passed",
						"attempt":      1,
					},
					{
						"id":           "validate_media",
						"name":         "Validate media",
						"status":       "completed",
						"started_at":   "2026-05-07T12:30:30Z",
						"completed_at": "2026-05-07T12:31:00Z",
						"message":      "Media validation passed",
						"attempt":      1,
					},
					{
						"id":         "human_review",
						"name":       "Human review",
						"status":     "waiting_review",
						"started_at": "2026-05-07T12:31:00Z",
						"message":    "Awaiting human review",
						"attempt":    1,
					},
					{
						"id":      "publish",
						"name":    "Publish to WooCommerce",
						"status":  "pending",
						"message": "Pending",
					},
				},
			}},
		},
	}
}

func ioNopCloserString(value string) *nopReadCloser {
	return &nopReadCloser{strings.NewReader(value)}
}

type nopReadCloser struct {
	*strings.Reader
}

func (n *nopReadCloser) Close() error { return nil }

func normalizeContractPayload(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			switch {
			case isContractTimeField(key):
				out[key] = "2026-05-07T00:00:00Z"
			case isContractIDField(key):
				out[key] = normalizeContractID(item)
			case key == "access_token" || key == "refresh_token":
				out[key] = "<token>"
			case key == "secret_hash":
				out[key] = "<sha256>"
			case key == "checksum_sha256":
				out[key] = "<sha256>"
			case key == "score" && isFloat(item):
				out[key] = 0.99
			case key == "confidence":
				out[key] = 0.99
			default:
				out[key] = normalizeContractPayload(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeContractPayload(item)
		}
		return out
	default:
		return value
	}
}

func isContractTimeField(key string) bool {
	return strings.HasSuffix(key, "_at") || strings.HasSuffix(key, "_time") || key == "expires_at"
}

func isContractIDField(key string) bool {
	return key == "id" || strings.HasSuffix(key, "_id") || key == "run_id" || key == "workflow_id" || key == "task_id" || key == "fact_check_id"
}

func normalizeContractID(value any) any {
	s, ok := value.(string)
	if !ok || s == "" {
		return value
	}
	if uuidLike.MatchString(s) {
		return "00000000-0000-0000-0000-000000000000"
	}
	if strings.HasPrefix(s, "product-publish-") {
		return "product-publish-00000000-0000-0000-0000-000000000000"
	}
	if strings.HasPrefix(s, "run-") {
		return "run-contract"
	}
	return s
}

func isFloat(value any) bool {
	_, ok := value.(float64)
	return ok
}

var uuidLike = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
