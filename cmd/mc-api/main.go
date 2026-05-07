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
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/minimax"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	orchestrator "github.com/nfsarch33/agentic-ecommerce/internal/agent"
	complianceagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/compliance"
	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	pricingagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/pricing"
	sourcingagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/sourcing"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
)

var (
	version = "dev"
	commit  = "unknown"
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
	allowedOrigin string
	apiToken      string
	webhookSecret string
}

type server struct {
	cfg            serverConfig
	repo           port.ProductRepository
	orderRepo      port.OrderRepository
	cartRepo       port.CartRepository
	syncEngine     *enginesync.Engine
	contentAgent   contentGenerator
	agentRegistry  *orchestrator.Registry
	agentScheduler *orchestrator.Scheduler
	webhookSecret  string
	log            *slog.Logger
}

type contentGenerator interface {
	Generate(ctx context.Context, req contentagent.GenerateRequest) (contentagent.GenerateResult, error)
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
# HELP agentic_ecommerce_http_requests_total HTTP request count placeholder for RED dashboards.
# TYPE agentic_ecommerce_http_requests_total counter
agentic_ecommerce_http_requests_total{handler="stub",method="GET",code="200"} 0
# HELP agentic_ecommerce_http_request_duration_seconds HTTP request duration placeholder for RED dashboards.
# TYPE agentic_ecommerce_http_request_duration_seconds histogram
agentic_ecommerce_http_request_duration_seconds_bucket{le="0.1"} 0
agentic_ecommerce_http_request_duration_seconds_bucket{le="0.25"} 0
agentic_ecommerce_http_request_duration_seconds_bucket{le="0.5"} 0
agentic_ecommerce_http_request_duration_seconds_bucket{le="1"} 0
agentic_ecommerce_http_request_duration_seconds_bucket{le="+Inf"} 0
agentic_ecommerce_http_request_duration_seconds_sum 0
agentic_ecommerce_http_request_duration_seconds_count 0
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
`, version, commit)
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
			AllowTestLocalhost: getenv("ECOMMERCE_AI_BRIDGE_TEST_MODE", "") == "true",
		}, nil)
		if err != nil {
			logger.Warn("content agent disabled", "error", err)
		} else {
			generator = contentagent.NewAgent(bridge)
		}
	}
	webhookSecret := getenv("ECOMMERCE_WC_WEBHOOK_SECRET", "")
	registry := defaultAgentRegistry()
	return &server{
		cfg: serverConfig{
			allowedOrigin: getenv("ECOMMERCE_ALLOWED_ORIGIN", ""),
			apiToken:      getenv("ECOMMERCE_API_TOKEN", ""),
			webhookSecret: webhookSecret,
		},
		repo:           repo,
		orderRepo:      orderRepo,
		cartRepo:       cartRepo,
		syncEngine:     enginesync.NewEngine(enginesync.Config{ProductRepository: repo, WooCommerce: wcClient, DefaultCurrency: "AUD"}),
		contentAgent:   generator,
		agentRegistry:  registry,
		agentScheduler: orchestrator.NewScheduler(registry, orchestrator.NewInMemoryStore(), orchestrator.NewEventRecorder(), nil, orchestrator.SchedulerOptions{MaxConcurrent: 2}),
		webhookSecret:  webhookSecret,
		log:            logger,
	}
}

func (s *server) ensureAgentScheduler() {
	if s.agentRegistry == nil {
		s.agentRegistry = defaultAgentRegistry()
	}
	if s.agentScheduler == nil {
		s.agentScheduler = orchestrator.NewScheduler(
			s.agentRegistry,
			orchestrator.NewInMemoryStore(),
			orchestrator.NewEventRecorder(),
			nil,
			orchestrator.SchedulerOptions{MaxConcurrent: 2},
		)
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

	api := s.withCORS(s.withBearerAuth(s.productsHandler))
	mux.HandleFunc("/api/v1/products", api)
	mux.HandleFunc("/api/v1/products/", api)
	ordersAPI := s.withCORS(s.withBearerAuth(s.ordersHandler))
	mux.HandleFunc("/api/v1/orders", ordersAPI)
	mux.HandleFunc("/api/v1/orders/", ordersAPI)
	cartAPI := s.withCORS(s.withBearerAuth(s.cartHandler))
	mux.HandleFunc("/api/v1/cart/", cartAPI)
	syncAPI := s.withCORS(s.withBearerAuth(s.syncHandler))
	mux.HandleFunc("/api/v1/sync/status", syncAPI)
	mux.HandleFunc("/api/v1/sync/conflicts", syncAPI)
	mux.HandleFunc("/api/v1/sync/conflicts/", syncAPI)
	mux.HandleFunc("/api/v1/sync/products/", syncAPI)
	mux.HandleFunc("/api/v1/webhooks/woocommerce/orders", s.woocommerceOrderWebhookHandler)
	mux.HandleFunc("/api/v1/webhooks/woocommerce/products", s.woocommerceProductWebhookHandler)
	complianceAPI := s.withCORS(s.withBearerAuth(s.complianceRulesHandler))
	mux.HandleFunc("/api/v1/compliance/rules", complianceAPI)
	agentsAPI := s.withCORS(s.withBearerAuth(s.agentsHandler))
	mux.HandleFunc("/api/v1/agents", agentsAPI)
	mux.HandleFunc("/api/v1/agents/", agentsAPI)
	agentRunsAPI := s.withCORS(s.withBearerAuth(s.agentRunsHandler))
	mux.HandleFunc("/api/v1/agent-runs/", agentRunsAPI)

	return s.withRequestLogging(mux)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ready",
		"service": "agentic-ecommerce-mc-api",
		"agents":  len(agents),
		"agent_worker": map[string]any{
			"ready":             s.agentScheduler != nil && len(agents) > 0,
			"scheduler":         "in_process",
			"registered_agents": len(agents),
		},
	})
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
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
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
		w.Header().Set("X-Request-ID", requestID)

		rec := &responseStatusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(start)

		logger := s.log
		if logger == nil {
			logger = slog.Default()
		}
		logger.Info("http.request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", duration.Milliseconds(),
		)
	})
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

func (s *server) withBearerAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.apiToken == "" {
			next(w, r)
			return
		}
		got := strings.TrimSpace(r.Header.Get("Authorization"))
		want := "Bearer " + s.cfg.apiToken
		if got != want {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorised"})
			return
		}
		next(w, r)
	}
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
