package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/minimax"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/objectstore"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	orchestrator "github.com/nfsarch33/agentic-ecommerce/internal/agent"
	complianceagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/compliance"
	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	pricingagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/pricing"
	sourcingagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/sourcing"
	compliancedomain "github.com/nfsarch33/agentic-ecommerce/internal/compliance"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/nfsarch33/agentic-ecommerce/internal/rag"
	"github.com/nfsarch33/agentic-ecommerce/internal/security"
	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
	"github.com/nfsarch33/agentic-ecommerce/internal/tenant"
	"github.com/nfsarch33/agentic-ecommerce/internal/webhook/outbound"
)

var (
	version = "dev"
	commit  = "unknown"

	httpRequestsTotal              atomic.Int64
	httpRequestDurationMicros      atomic.Int64
	httpRequestDurationBucketLe01  atomic.Int64
	httpRequestDurationBucketLe025 atomic.Int64
	httpRequestDurationBucketLe05  atomic.Int64
	httpRequestDurationBucketLe1   atomic.Int64
	httpRequestDurationBucketInf   atomic.Int64
	httpResponses2xx               atomic.Int64
	httpResponses3xx               atomic.Int64
	httpResponses4xx               atomic.Int64
	httpResponses5xx               atomic.Int64
)

type moneyResponse struct {
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}

type productResponse struct {
	ID          string        `json:"id"`
	SKU         string        `json:"sku"`
	Title       string        `json:"title"`
	Slug        string        `json:"slug"`
	Price       moneyResponse `json:"price"`
	Stock       int           `json:"stock"`
	Status      string        `json:"status"`
	Description string        `json:"description,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type createProductRequest struct {
	SKU         string        `json:"sku"`
	Title       string        `json:"title"`
	Slug        string        `json:"slug,omitempty"`
	Description string        `json:"description,omitempty"`
	Price       moneyResponse `json:"price"`
	Stock       int           `json:"stock"`
	Status      string        `json:"status,omitempty"`
}

type listResponse struct {
	Products []productResponse `json:"products"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PerPage  int               `json:"per_page"`
}

type serverConfig struct {
	allowedOrigin     string
	apiToken          string
	webhookSecret     string
	jwtSecret         string
	jwtIssuer         string
	jwtAudience       string
	jwtAccessTTL      time.Duration
	refreshTTL        time.Duration
	adminUsername     string
	adminPassword     string
	adminRole         security.Role
	rateLimitCapacity int
	rateLimitRefill   time.Duration
	readinessTimeout  time.Duration
	redisTimeout      time.Duration
	shutdownTimeout   time.Duration
	otelEnabled       bool
}

type server struct {
	cfg               serverConfig
	repo              port.ProductRepository
	orderRepo         port.OrderRepository
	cartRepo          port.CartRepository
	eventBus          eventHistory
	syncEngine        *enginesync.Engine
	contentAgent      contentGenerator
	rag               *rag.Service
	factChecker       *contentagent.FactChecker
	factChecksMu      sync.RWMutex
	factChecks        map[string]contentagent.FactCheckResult
	mediaService      *intelligence.Service
	workflowClient    temporalWorkflowClient
	agentRegistry     *orchestrator.Registry
	agentScheduler    *orchestrator.Scheduler
	agentSchedules    *orchestrator.ScheduleManager
	schedulerMu       sync.Mutex
	webhookService    *outbound.Service
	webhookSecret     string
	tenantService     *tenant.Service
	customRuleStore   compliancedomain.CustomRuleStore
	complianceHistory compliancedomain.HistoryStore
	tokenManager      *security.TokenManager
	sessions          security.RefreshSessionStore
	rateLimiter       security.RateLimiter
	rateLimitFallback security.RateLimiter
	readiness         []readinessProbe
	cleanup           []func()
	log               *slog.Logger
}

type contentGenerator interface {
	Generate(ctx context.Context, req contentagent.GenerateRequest) (contentagent.GenerateResult, error)
}

type eventHistory interface {
	eventbus.Publisher
	Delivered() []eventbus.Event
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if isHealthcheckArgs(os.Args) {
		if err := runHealthcheck(getenv("ECOMMERCE_HTTP_ADDR", "127.0.0.1:8080")); err != nil {
			logger.Error("mc-api.healthcheck_failed", "error", err)
			os.Exit(1)
		}
		return
	}

	repo := inmemory.NewProductRepository()
	seedDefaultProducts(repo)
	orderRepo := inmemory.NewOrderRepository()
	cartRepo := inmemory.NewCartRepository()

	addr := getenv("ECOMMERCE_HTTP_ADDR", "127.0.0.1:8080")
	logger.Info("mc-api.start", "addr", addr)

	srv := newServer(logger, repo, orderRepo, cartRepo)
	defer srv.Close()
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("mc-api.stop", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("mc-api.shutdown")
		shutdownTimeout := srv.cfg.shutdownTimeout
		if shutdownTimeout <= 0 {
			shutdownTimeout = 10 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("mc-api.shutdown_failed", "error", err)
			os.Exit(1)
		}
	}
}

func isHealthcheckArgs(args []string) bool {
	if len(args) < 2 {
		return false
	}
	return args[1] == "healthcheck" || args[1] == "--healthcheck"
}

func runHealthcheck(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		host = "127.0.0.1"
		port = "8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz status %d", resp.StatusCode)
	}
	return nil
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(w, `# HELP agentic_ecommerce_build_info Build metadata for the running mc-api binary.
# TYPE agentic_ecommerce_build_info gauge
agentic_ecommerce_build_info{version=%q,commit=%q} 1
# HELP agentic_ecommerce_http_requests_total HTTP request count for RED dashboards.
# TYPE agentic_ecommerce_http_requests_total counter
agentic_ecommerce_http_requests_total{code_class="2xx"} %d
agentic_ecommerce_http_requests_total{code_class="3xx"} %d
agentic_ecommerce_http_requests_total{code_class="4xx"} %d
agentic_ecommerce_http_requests_total{code_class="5xx"} %d
# HELP agentic_ecommerce_http_request_duration_seconds HTTP request duration for RED dashboards.
# TYPE agentic_ecommerce_http_request_duration_seconds histogram
agentic_ecommerce_http_request_duration_seconds_bucket{le="0.1"} %d
agentic_ecommerce_http_request_duration_seconds_bucket{le="0.25"} %d
agentic_ecommerce_http_request_duration_seconds_bucket{le="0.5"} %d
agentic_ecommerce_http_request_duration_seconds_bucket{le="1"} %d
agentic_ecommerce_http_request_duration_seconds_bucket{le="+Inf"} %d
agentic_ecommerce_http_request_duration_seconds_sum %.6f
agentic_ecommerce_http_request_duration_seconds_count %d
# HELP agentic_ecommerce_sync_lag_seconds WooCommerce sync lag placeholder.
# TYPE agentic_ecommerce_sync_lag_seconds gauge
agentic_ecommerce_sync_lag_seconds 0
# HELP agentic_ecommerce_sync_conflicts_total WooCommerce sync conflicts detected by the backend.
# TYPE agentic_ecommerce_sync_conflicts_total counter
agentic_ecommerce_sync_conflicts_total{resolution="pending"} 0
# HELP agentic_ecommerce_agent_success_total Agent success count placeholder.
# TYPE agentic_ecommerce_agent_success_total counter
agentic_ecommerce_agent_success_total{agent="content"} 0
# HELP agentic_ecommerce_compliance_checks_total Compliance checks evaluated by the backend.
# TYPE agentic_ecommerce_compliance_checks_total counter
agentic_ecommerce_compliance_checks_total{source="stub"} 0
# HELP agentic_ecommerce_compliance_failures_total Compliance checks that failed the publish gate.
# TYPE agentic_ecommerce_compliance_failures_total counter
agentic_ecommerce_compliance_failures_total{source="stub"} 0
# HELP agentic_ecommerce_media_validation_failures_total Media uploads rejected by validation.
# TYPE agentic_ecommerce_media_validation_failures_total counter
agentic_ecommerce_media_validation_failures_total{reason="stub"} 0
# HELP agentic_ecommerce_rag_search_duration_seconds RAG vector search latency for content grounding.
# TYPE agentic_ecommerce_rag_search_duration_seconds histogram
agentic_ecommerce_rag_search_duration_seconds_bucket{le="0.1"} 0
agentic_ecommerce_rag_search_duration_seconds_bucket{le="0.25"} 0
agentic_ecommerce_rag_search_duration_seconds_bucket{le="0.5"} 0
agentic_ecommerce_rag_search_duration_seconds_bucket{le="1"} 0
agentic_ecommerce_rag_search_duration_seconds_bucket{le="+Inf"} 0
agentic_ecommerce_rag_search_duration_seconds_sum 0
agentic_ecommerce_rag_search_duration_seconds_count 0
# HELP agentic_ecommerce_embedding_failures_total Embedding generation failures returned by the approved bridge.
# TYPE agentic_ecommerce_embedding_failures_total counter
agentic_ecommerce_embedding_failures_total{provider="bridge",reason="stub"} 0
`,
		version,
		commit,
		httpResponses2xx.Load(),
		httpResponses3xx.Load(),
		httpResponses4xx.Load(),
		httpResponses5xx.Load(),
		httpRequestDurationBucketLe01.Load(),
		httpRequestDurationBucketLe025.Load(),
		httpRequestDurationBucketLe05.Load(),
		httpRequestDurationBucketLe1.Load(),
		httpRequestDurationBucketInf.Load(),
		float64(httpRequestDurationMicros.Load())/1_000_000,
		httpRequestsTotal.Load(),
	)
}

func newServer(logger *slog.Logger, repo port.ProductRepository, orderRepo port.OrderRepository, cartRepo port.CartRepository) *server {
	wcClient := woocommerce.NewClient(woocommerce.Config{
		BaseURL:        getenv("ECOMMERCE_WC_STORE_URL", ""),
		ConsumerKey:    getenv("ECOMMERCE_WC_CONSUMER_KEY", ""),
		ConsumerSecret: getenv("ECOMMERCE_WC_CONSUMER_SECRET", ""),
	}, nil)
	var generator contentGenerator
	if bridgeURL := firstNonEmpty(getenv("ECOMMERCE_AI_BRIDGE_URL", ""), getenv("MINIMAX_BRIDGE_URL", "")); bridgeURL != "" {
		bridge, err := minimax.NewClient(minimax.Config{
			BridgeURL:          bridgeURL,
			Model:              getenv("ECOMMERCE_AI_MODEL", ""),
			EmbeddingModel:     getenv("ECOMMERCE_AI_EMBEDDING_MODEL", ""),
			AllowTestLocalhost: getenv("ECOMMERCE_AI_BRIDGE_TEST_MODE", "") == "true",
		}, nil)
		if err != nil {
			logger.Warn("content agent disabled", "error", err)
		} else {
			generator = contentagent.NewAgent(bridge)
		}
	}
	var ragService *rag.Service
	if parseBoolEnv("ECOMMERCE_RAG_ENABLED", false) {
		dimensions := queryDefaultEmbeddingDimensions()
		ragService = rag.NewService(rag.NewHashEmbedder(dimensions), rag.NewInMemoryVectorStore(dimensions), rag.ChunkOptions{MaxWords: 180, OverlapWords: 24})
	}
	webhookSecret := getenv("ECOMMERCE_WC_WEBHOOK_SECRET", "")
	readinessTimeout := parseDurationEnv("ECOMMERCE_READINESS_TIMEOUT", 2*time.Second)
	redisTimeout := parseDurationEnv("ECOMMERCE_REDIS_TIMEOUT", 500*time.Millisecond)
	shutdownTimeout := parseDurationEnv("ECOMMERCE_SHUTDOWN_TIMEOUT", 10*time.Second)
	jwtSecret, jwtIssuer, jwtAudience, jwtAccessTTL, refreshTTL, adminUsername, adminPassword, adminRole, rateLimitCapacity, rateLimitRefill := securityConfigFromEnv()
	readinessChecks, cleanup := newReadinessChecksFromEnv(readinessTimeout)
	otelEnabled := parseBoolEnv("ECOMMERCE_OTEL_ENABLED", false)
	if otelEnabled {
		configureTelemetry()
	}
	temporalAddr := getenv("ECOMMERCE_TEMPORAL_ADDR", "")
	workflowClient, workflowCleanup := newTemporalWorkflowClient(logger, temporalAddr)
	if workflowCleanup != nil {
		cleanup = append(cleanup, workflowCleanup)
	}
	mediaStore, err := objectstore.New(objectstore.Config{
		Provider:      objectstore.Provider(getenv("ECOMMERCE_MEDIA_STORE_PROVIDER", "local")),
		RootDir:       getenv("ECOMMERCE_MEDIA_STORE_ROOT", ".local/media-uploads"),
		PublicBaseURL: getenv("ECOMMERCE_MEDIA_PUBLIC_BASE_URL", "/media"),
		Bucket:        getenv("ECOMMERCE_MEDIA_BUCKET", ""),
		Region:        getenv("ECOMMERCE_MEDIA_REGION", ""),
		Endpoint:      getenv("ECOMMERCE_MEDIA_ENDPOINT", ""),
		Prefix:        getenv("ECOMMERCE_MEDIA_PREFIX", ""),
	})
	if err != nil {
		logger.Warn("media object store disabled", "error", err)
	}
	registry := defaultAgentRegistry()
	bus := eventbus.NewInMemoryBus()
	webhookService := outbound.NewService(outbound.ServiceConfig{Logger: logger})
	if err := webhookService.Subscribe(context.Background(), bus, "webhook-bridge"); err != nil {
		logger.Warn("webhook bridge disabled", "error", err)
	}
	srv := &server{
		cfg: serverConfig{
			allowedOrigin:     getenv("ECOMMERCE_ALLOWED_ORIGIN", ""),
			apiToken:          getenv("ECOMMERCE_API_TOKEN", ""),
			webhookSecret:     webhookSecret,
			jwtSecret:         jwtSecret,
			jwtIssuer:         jwtIssuer,
			jwtAudience:       jwtAudience,
			jwtAccessTTL:      jwtAccessTTL,
			refreshTTL:        refreshTTL,
			adminUsername:     adminUsername,
			adminPassword:     adminPassword,
			adminRole:         adminRole,
			rateLimitCapacity: rateLimitCapacity,
			rateLimitRefill:   rateLimitRefill,
			readinessTimeout:  readinessTimeout,
			redisTimeout:      redisTimeout,
			shutdownTimeout:   shutdownTimeout,
			otelEnabled:       otelEnabled,
		},
		repo:              repo,
		orderRepo:         orderRepo,
		cartRepo:          cartRepo,
		eventBus:          bus,
		syncEngine:        enginesync.NewEngine(enginesync.Config{ProductRepository: repo, WooCommerce: wcClient, DefaultCurrency: "AUD"}),
		contentAgent:      generator,
		rag:               ragService,
		factChecks:        map[string]contentagent.FactCheckResult{},
		mediaService:      intelligence.NewService(intelligence.ServiceConfig{HTTPClient: &http.Client{Timeout: 15 * time.Second}, Store: mediaStore}),
		workflowClient:    workflowClient,
		agentRegistry:     registry,
		agentScheduler:    orchestrator.NewScheduler(registry, orchestrator.NewInMemoryStore(), eventbus.NewEventBusAdapter(bus, "mc-api.agent"), nil, orchestrator.SchedulerOptions{MaxConcurrent: 2}),
		agentSchedules:    defaultAgentScheduleManager(),
		webhookService:    webhookService,
		webhookSecret:     webhookSecret,
		tenantService:     tenant.NewService(tenant.NewInMemoryRepository()),
		customRuleStore:   compliancedomain.NewInMemoryCustomRuleStore(),
		complianceHistory: compliancedomain.NewInMemoryHistoryStore(),
		readiness:         readinessChecks,
		cleanup:           cleanup,
		log:               logger,
	}
	srv.configureSecurity()
	return srv
}

func (s *server) Close() {
	for _, cleanup := range s.cleanup {
		if cleanup != nil {
			cleanup()
		}
	}
}

func (s *server) ensureAgentScheduler() {
	s.schedulerMu.Lock()
	defer s.schedulerMu.Unlock()
	if s.eventBus == nil {
		s.eventBus = eventbus.NewInMemoryBus()
	}
	if s.agentRegistry == nil {
		s.agentRegistry = defaultAgentRegistry()
	}
	if s.agentScheduler == nil {
		var sink orchestrator.EventSink = orchestrator.NewEventRecorder()
		if s.eventBus != nil {
			sink = eventbus.NewEventBusAdapter(s.eventBus, "mc-api.agent")
		}
		s.agentScheduler = orchestrator.NewScheduler(
			s.agentRegistry,
			orchestrator.NewInMemoryStore(),
			sink,
			nil,
			orchestrator.SchedulerOptions{MaxConcurrent: 2},
		)
	}
	if s.agentSchedules == nil {
		s.agentSchedules = defaultAgentScheduleManager()
	}
}

func defaultAgentRegistry() *orchestrator.Registry {
	registry := orchestrator.NewRegistry()
	for _, candidate := range []orchestrator.Agent{
		complianceagent.NewAgent(),
		pricingagent.NewAgent(),
		sourcingagent.NewAgent(),
	} {
		_ = registry.Register(candidate)
	}
	return registry
}

func (s *server) mux() http.Handler {
	s.ensureAgentScheduler()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", s.readyzHandler)
	mux.HandleFunc("/metrics", metricsHandler)

	authAPI := s.withCORS(s.withRateLimit(s.authHandler))
	mux.HandleFunc("/api/v1/auth/", authAPI)
	api := s.withCORS(s.withRateLimit(s.withRBAC(productsRole, s.withAudit(productsAuditAction, s.productsHandler))))
	mux.HandleFunc("/api/v1/products", api)
	mux.HandleFunc("/api/v1/products/", api)
	ordersAPI := s.withCORS(s.withRateLimit(s.withRBAC(ordersRole, s.withAudit(ordersAuditAction, s.ordersHandler))))
	mux.HandleFunc("/api/v1/orders", ordersAPI)
	mux.HandleFunc("/api/v1/orders/", ordersAPI)
	cartAPI := s.withCORS(s.withRateLimit(s.withRBAC(publicRole, s.withAudit(cartAuditAction, s.cartHandler))))
	mux.HandleFunc("/api/v1/cart/", cartAPI)
	syncAPI := s.withCORS(s.withRateLimit(s.withRBAC(syncRole, s.withAudit(syncAuditAction, s.syncHandler))))
	mux.HandleFunc("/api/v1/sync/status", syncAPI)
	mux.HandleFunc("/api/v1/sync/conflicts", syncAPI)
	mux.HandleFunc("/api/v1/sync/conflicts/", syncAPI)
	mux.HandleFunc("/api/v1/sync/products/", syncAPI)
	mux.HandleFunc("/api/v1/webhooks/woocommerce/orders", s.withAudit(webhookAuditAction("webhook.woocommerce.orders"), s.woocommerceOrderWebhookHandler))
	mux.HandleFunc("/api/v1/webhooks/woocommerce/products", s.withAudit(webhookAuditAction("webhook.woocommerce.products"), s.woocommerceProductWebhookHandler))
	webhooksAPI := s.withCORS(s.withRateLimit(s.withRBAC(webhooksRole, s.withAudit(webhooksAuditAction, s.webhooksHandler))))
	mux.HandleFunc("/api/v1/webhooks", webhooksAPI)
	mux.HandleFunc("/api/v1/webhooks/", webhooksAPI)
	complianceAPI := s.withCORS(s.withRateLimit(s.withRBAC(viewerRole, s.complianceRulesHandler)))
	mux.HandleFunc("/api/v1/compliance/rules", complianceAPI)
	tenantSettingsAPI := s.withCORS(s.withRateLimit(s.withRBAC(tenantSettingsRole, s.withTenantRequired(s.tenantSettingsHandler))))
	mux.HandleFunc("/api/v1/tenant/settings", tenantSettingsAPI)
	customRulesAPI := s.withCORS(s.withRateLimit(s.withRBAC(customRulesRole, s.withTenantRequired(s.customRulesHandler))))
	mux.HandleFunc("/api/v1/compliance/custom-rules", customRulesAPI)
	mux.HandleFunc("/api/v1/compliance/custom-rules/", customRulesAPI)
	reportsAPI := s.withCORS(s.withRateLimit(s.withRBAC(viewerRole, s.withTenantRequired(s.complianceReportsHandler))))
	mux.HandleFunc("/api/v1/compliance/reports/summary", reportsAPI)
	mux.HandleFunc("/api/v1/compliance/reports/export", reportsAPI)
	agentsAPI := s.withCORS(s.withRateLimit(s.withRBAC(agentsRole, s.withAudit(agentAuditAction, s.agentsHandler))))
	mux.HandleFunc("/api/v1/agents", agentsAPI)
	mux.HandleFunc("/api/v1/agents/", agentsAPI)
	agentRunsAPI := s.withCORS(s.withRateLimit(s.withRBAC(viewerRole, s.agentRunsHandler)))
	mux.HandleFunc("/api/v1/agent-runs/", agentRunsAPI)
	agentSchedulesAPI := s.withCORS(s.withRateLimit(s.withRBAC(agentsRole, s.withAudit(agentAuditAction, s.agentSchedulesHandler))))
	mux.HandleFunc("/api/v1/agent-schedules", agentSchedulesAPI)
	mux.HandleFunc("/api/v1/agent-schedules/", agentSchedulesAPI)
	eventsAPI := s.withCORS(s.withRateLimit(s.withRBAC(viewerRole, s.recentEventsHandler)))
	mux.HandleFunc("/api/v1/events/recent", eventsAPI)
	workflowsAPI := s.withCORS(s.withRateLimit(s.withRBAC(agentsRole, s.withAudit(workflowAuditAction, s.workflowsHandler))))
	mux.HandleFunc("/api/v1/workflows/product-publish", workflowsAPI)
	mux.HandleFunc("/api/v1/workflows/content-generation", workflowsAPI)
	mux.HandleFunc("/api/v1/workflows/media-processing", workflowsAPI)
	mux.HandleFunc("/api/v1/workflows/sourcing", workflowsAPI)
	mux.HandleFunc("/api/v1/workflows/", workflowsAPI)
	ragAPI := s.withCORS(s.withRateLimit(s.withRBAC(agentsRole, s.ragHandler)))
	mux.HandleFunc("/api/v1/rag/documents", ragAPI)
	mux.HandleFunc("/api/v1/rag/search", ragAPI)
	contentAPI := s.withCORS(s.withRateLimit(s.withRBAC(agentsRole, s.contentFactCheckHandler)))
	mux.HandleFunc("/api/v1/content/generate", contentAPI)
	mux.HandleFunc("/api/v1/content/fact-checks/", contentAPI)
	mediaAPI := s.withCORS(s.withRateLimit(s.withRBAC(agentsRole, s.mediaHandler)))
	mux.HandleFunc("/api/v1/media/source", mediaAPI)
	mux.HandleFunc("/api/v1/media/process", mediaAPI)
	mux.HandleFunc("/api/v1/media/", mediaAPI)

	return s.withSecurityHeaders(s.withTelemetry(s.withRequestLogging(mux)))
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "agentic-ecommerce-mc-api",
	})
}

func (s *server) readyzHandler(w http.ResponseWriter, r *http.Request) {
	s.ensureAgentScheduler()
	agents := s.agentRegistry.List()
	resp := readyzResponse{
		Status:  "ready",
		Service: "agentic-ecommerce-mc-api",
		Agents:  len(agents),
		AgentWorker: agentWorkerReadiness{
			Ready:            s.agentScheduler != nil && len(agents) > 0,
			Scheduler:        "in_process",
			RegisteredAgents: len(agents),
		},
		Checks: s.runReadinessChecks(r.Context()),
	}
	status := http.StatusOK
	if !resp.AgentWorker.Ready || hasFailedReadinessCheck(resp.Checks) {
		resp.Status = "not_ready"
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, resp)
}

func (s *server) productsHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/products")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" && r.Method == http.MethodGet:
		s.listProducts(w, r)
	case path == "" && r.Method == http.MethodPost:
		s.createProduct(w, r)
	case strings.HasSuffix(path, "/generate-description") && r.Method == http.MethodPost:
		s.generateDescription(w, r, path)
	case strings.HasSuffix(path, "/ai-suggestions") && r.Method == http.MethodGet:
		s.aiSuggestions(w, r, path)
	case strings.HasSuffix(path, "/compliance-check") && r.Method == http.MethodPost:
		s.complianceCheck(w, r, path)
	case strings.HasSuffix(path, "/seo-suggestions") && r.Method == http.MethodPost:
		s.seoSuggestions(w, r, path)
	case path != "" && r.Method == http.MethodGet:
		s.getProduct(w, r, path)
	case path != "" && r.Method == http.MethodPut:
		s.updateProduct(w, r, path)
	case path != "" && r.Method == http.MethodDelete:
		s.deleteProduct(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) listProducts(w http.ResponseWriter, r *http.Request) {
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	result, err := s.repo.List(r.Context(), page, perPage)
	if err != nil {
		s.log.Error("list products", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	products := make([]productResponse, len(result.Products))
	for i, p := range result.Products {
		products[i] = toProductResponse(p)
	}

	writeJSON(w, http.StatusOK, listResponse{
		Products: products,
		Total:    result.Total,
		Page:     page,
		PerPage:  perPage,
	})
}

func (s *server) createProduct(w http.ResponseWriter, r *http.Request) {
	var req createProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	price, err := catalog.NewMoney(req.Price.Amount, req.Price.Currency)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	status := catalog.StatusDraft
	if req.Status != "" {
		status, err = catalog.ParseProductStatus(req.Status)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
	}

	product, err := catalog.NewProduct(catalog.ProductInput{
		SKU:         req.SKU,
		Title:       req.Title,
		Slug:        req.Slug,
		Description: req.Description,
		Price:       price,
		Stock:       req.Stock,
		Status:      status,
	})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	if err := s.repo.Create(r.Context(), product); err != nil {
		s.log.Error("create product", "error", err)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "duplicate_product"})
		return
	}

	writeJSON(w, http.StatusCreated, toProductResponse(product))
}

func (s *server) getProduct(w http.ResponseWriter, r *http.Request, idOrSlug string) {
	var (
		product catalog.Product
		err     error
	)

	if id, parseErr := uuid.Parse(idOrSlug); parseErr == nil {
		product, err = s.repo.GetByID(r.Context(), id)
	} else {
		product, err = s.repo.GetBySlug(r.Context(), idOrSlug)
	}

	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get product", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	writeJSON(w, http.StatusOK, toProductResponse(product))
}

func (s *server) updateProduct(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}

	existing, err := s.repo.GetByID(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get product for update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	var req createProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	price, err := catalog.NewMoney(req.Price.Amount, req.Price.Currency)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	status := existing.Status()
	if req.Status != "" {
		status, err = catalog.ParseProductStatus(req.Status)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
	}

	slug := req.Slug
	if slug == "" {
		slug = existing.Slug()
	}

	updated := catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          id,
		SKU:         req.SKU,
		Title:       req.Title,
		Slug:        slug,
		Description: req.Description,
		Price:       price,
		Stock:       req.Stock,
		Status:      status,
		CreatedAt:   existing.CreatedAt(),
		UpdatedAt:   existing.UpdatedAt(),
	})

	if err := s.repo.Update(r.Context(), updated); err != nil {
		s.log.Error("update product", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	writeJSON(w, http.StatusOK, toProductResponse(updated))
}

func (s *server) deleteProduct(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}

	if err := s.repo.Delete(r.Context(), id); err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("delete product", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if s.cfg.allowedOrigin == "" || origin != s.cfg.allowedOrigin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin_not_allowed"})
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, X-Tenant-ID, Traceparent")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *server) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		corr := &requestCorrelation{
			RequestID: requestID,
			TenantID:  strings.TrimSpace(r.Header.Get("X-Tenant-ID")),
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		ctx = context.WithValue(ctx, requestCorrelationContextKey{}, corr)
		r = r.WithContext(ctx)

		rec := &responseStatusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(start)
		recordHTTPRequest(status, duration)

		logger := s.log
		if logger == nil {
			logger = slog.Default()
		}
		attrs := []any{
			"service", "agentic-ecommerce-mc-api",
			"request_id", requestID,
			"route", routePattern(r),
			"method", r.Method,
			"http_method", r.Method,
			"path", r.URL.Path,
			"http_path", r.URL.Path,
			"status", status,
			"http_status", status,
			"duration_ms", duration.Milliseconds(),
			"duration_seconds", duration.Seconds(),
			"client_ip", clientIPFromRequest(r),
		}
		if corr.TenantID != "" {
			attrs = append(attrs, "tenant_id", corr.TenantID)
		}
		if corr.ActorID != "" {
			attrs = append(attrs, "actor_id", corr.ActorID)
		}
		if userAgent := strings.TrimSpace(r.UserAgent()); userAgent != "" {
			attrs = append(attrs, "user_agent", userAgent)
		}
		if traceID := traceIDFromRequest(r); traceID != "" {
			attrs = append(attrs, "trace_id", traceID)
		}
		logger.Info("http.request", attrs...)
	})
}

func recordHTTPRequest(status int, duration time.Duration) {
	httpRequestsTotal.Add(1)
	httpRequestDurationMicros.Add(duration.Microseconds())
	seconds := duration.Seconds()
	if seconds <= 0.1 {
		httpRequestDurationBucketLe01.Add(1)
	}
	if seconds <= 0.25 {
		httpRequestDurationBucketLe025.Add(1)
	}
	if seconds <= 0.5 {
		httpRequestDurationBucketLe05.Add(1)
	}
	if seconds <= 1 {
		httpRequestDurationBucketLe1.Add(1)
	}
	httpRequestDurationBucketInf.Add(1)

	switch {
	case status >= 200 && status < 300:
		httpResponses2xx.Add(1)
	case status >= 300 && status < 400:
		httpResponses3xx.Add(1)
	case status >= 400 && status < 500:
		httpResponses4xx.Add(1)
	case status >= 500:
		httpResponses5xx.Add(1)
	}
}

type responseStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseStatusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseStatusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(body)
}

func toProductResponse(p catalog.Product) productResponse {
	return productResponse{
		ID:          p.ID().String(),
		SKU:         p.SKU(),
		Title:       p.Title(),
		Slug:        p.Slug(),
		Price:       moneyResponse{Amount: p.Price().Amount(), Currency: p.Price().Currency()},
		Stock:       p.Stock(),
		Status:      p.Status().String(),
		Description: p.Description(),
		CreatedAt:   p.CreatedAt(),
		UpdatedAt:   p.UpdatedAt(),
	}
}

func seedDefaultProducts(repo *inmemory.ProductRepository) {
	price1, _ := catalog.NewMoney(4995, "AUD")
	p1, _ := catalog.NewProduct(catalog.ProductInput{
		SKU:         "RESISTANCE-BAND-SET",
		Title:       "Resistance band set",
		Slug:        "resistance-band-set",
		Description: "Starter strength kit for home training.",
		Price:       price1,
		Stock:       12,
		Status:      catalog.StatusActive,
	})
	_ = repo.Create(nil, p1)

	price2, _ := catalog.NewMoney(3500, "AUD")
	p2, _ := catalog.NewProduct(catalog.ProductInput{
		SKU:         "FOAM-ROLLER",
		Title:       "Foam roller",
		Slug:        "foam-roller",
		Description: "Dense recovery roller for mobility work.",
		Price:       price2,
		Stock:       5,
		Status:      catalog.StatusActive,
	})
	_ = repo.Create(nil, p2)
}

func isNotFound(err error) bool {
	return errors.Is(err, inmemory.ErrProductNotFound) || errors.Is(err, inmemory.ErrOrderNotFound)
}

func queryInt(r *http.Request, key string, fallback int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func queryDefaultEmbeddingDimensions() int {
	value := strings.TrimSpace(os.Getenv("ECOMMERCE_RAG_EMBEDDING_DIMENSIONS"))
	if value == "" {
		return rag.DefaultEmbeddingDimensions
	}
	dimensions, err := strconv.Atoi(value)
	if err != nil || dimensions <= 0 {
		return rag.DefaultEmbeddingDimensions
	}
	return dimensions
}
