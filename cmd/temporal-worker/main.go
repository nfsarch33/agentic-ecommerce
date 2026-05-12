package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/minimax"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/objectstore"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/postgres"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	"github.com/nfsarch33/agentic-ecommerce/internal/lifecycle"
	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
	"github.com/nfsarch33/agentic-ecommerce/internal/memwatch"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/nfsarch33/agentic-ecommerce/internal/rag"
	"github.com/nfsarch33/agentic-ecommerce/internal/registration"
	"github.com/nfsarch33/agentic-ecommerce/internal/runtimeobs"
	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
	"github.com/nfsarch33/agentic-ecommerce/internal/tenant"
	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// workerRegistry is the slice of temporal worker.Worker methods that
// the v2.6.1 cmd/* DI refactor uses to register workflows and
// activities. It exists only so registerWorkflowsAndActivities can be
// driven by a fake in tests without dragging in a real Temporal
// client. Concrete worker.Worker satisfies this implicitly.
type workerRegistry interface {
	RegisterWorkflow(w any)
	RegisterActivityWithOptions(a any, options activity.RegisterOptions)
}

// temporalDialer is the constructor function for a Temporal client.
// Production wires it to client.Dial; tests inject a stub that returns
// a fake client and surfaces dial errors deterministically.
type temporalDialer func(opts client.Options) (client.Client, error)

// workerDeps groups the runtime adapters needed to register workflows
// and activities. All fields are concrete pointers to keep the
// dependency surface explicit (no interface{} bag-of-everything).
type workerDeps struct {
	Logger                *slog.Logger
	TaskQueue             string
	ScheduleCfg           agentScheduleConfig
	Repo                  port.ProductRepository
	RepoCleanup           func()
	PublishActivities     *ecworkflow.ProductPublishActivities
	ContentActivities     *ecworkflow.ContentGenerationActivities
	MediaActivities       *ecworkflow.MediaProcessingActivities
	SourcingActivities    *ecworkflow.SourcingActivities
	OnboardingActivities  *ecworkflow.TenantOnboardingActivities
	MarketplaceActivities *ecworkflow.MarketplaceSyncActivities
	ImageEditActivities   *ecworkflow.ImageEditApprovalActivities
	// v6.3.0 CF-14: GMV daily REFRESH activity. Nil when no
	// ECOMMERCE_DB_URL is configured (the workflow then refuses to
	// run because executor is unwired).
	GMVDailyRefreshActivities *ecworkflow.GMVDailyRefreshActivities
}

type agentScheduleConfig struct {
	Enabled           bool
	DefaultInterval   time.Duration
	MaxConcurrentRuns int
	TaskQueue         string
}

func main() {
	os.Exit(mainImpl(context.Background(), os.Stdout, os.Getenv, client.Dial))
}

// mainImpl is the testable entry point. The temporalDialer abstracts
// client.Dial so tests can inject a stub that returns a fake client
// (or a typed error) without standing up a real Temporal frontend.
// Returns the process exit code so main() reduces to a single
// os.Exit(...) call.
func mainImpl(ctx context.Context, stdout io.Writer, getenv func(string) string, dial temporalDialer) int {
	logger := slog.New(slog.NewJSONHandler(stdout, nil))
	temporalAddr := temporalAddressFromEnv()
	scheduleCfg, err := agentScheduleConfigFromEnv()
	if err != nil {
		logger.Error("temporal_worker.agent_schedules_config", "error", err)
		return 1
	}

	c, err := dial(client.Options{HostPort: temporalAddr})
	if err != nil {
		logger.Error("temporal_worker.client", "addr", temporalAddr, "error", err)
		return 1
	}
	defer c.Close()

	deps, err := buildWorkerDeps(ctx, logger, scheduleCfg)
	if err != nil {
		logger.Error("temporal_worker.dependencies", "error", err)
		return 1
	}
	defer deps.RepoCleanup()

	w := worker.New(c, deps.TaskQueue, worker.Options{})
	registerWorkflowsAndActivities(w, deps)

	// v6.3.0 CF-14: register the GMV daily REFRESH schedule (cron
	// 0 2 * * * Australia/Sydney). Idempotent: AlreadyExists is
	// swallowed so worker restarts do not stack schedules.
	ensureGMVDailyRefreshSchedule(ctx, logger, c, deps.TaskQueue)

	// v2.10.0 Story 1+3: bind memwatch + lifecycle Manager so heap +
	// goroutine ceilings are monitored. Temporal owns its own InterruptCh
	// signal handling so we run lifecycle in parallel via Shutdown after
	// w.Run returns.
	mgr := lifecycle.New(logger, 30*time.Second)
	rt := runtimeobs.New(logger, "temporal-worker", runtimeobs.Config{
		EvomapPath: runtimeobs.DefaultEvomapPath(os.Getenv),
		Rotate:     true,
	})
	reg := rt.Registry()
	sampler := memwatch.NewSampler(logger, memwatch.Config{
		BinaryName:        "temporal-worker",
		SampleInterval:    5 * time.Second,
		Sink:              rt,
		HeapAlarmCallback: func() { reg.OOMAlarms.Inc(metrics.Labels{}) },
	})
	go func() { _ = sampler.Run(context.Background()) }()
	mgr.Register("memwatch", sampler)
	mgr.Register("runtime-observability", rt)
	defer func() { _ = mgr.Shutdown() }()

	logger.Info(
		"temporal_worker.start",
		"task_queue", deps.TaskQueue,
		"addr", temporalAddr,
		"agent_schedules_enabled", scheduleCfg.Enabled,
		"agent_schedule_default_interval", scheduleCfg.DefaultInterval.String(),
		"agent_schedule_max_concurrent_runs", scheduleCfg.MaxConcurrentRuns,
		"agent_schedule_task_queue", scheduleCfg.TaskQueue,
	)
	if err := w.Run(worker.InterruptCh()); err != nil {
		logger.Error("temporal_worker.run", "error", err)
		return 1
	}
	return 0
}

// buildWorkerDeps wires the activity adapters used by the temporal
// worker. Touches os.Getenv, http.Client, and the postgres pool.
// Splitting this out from mainImpl gives tests a deterministic
// surface to assert configuration without driving the real Temporal
// SDK.
func buildWorkerDeps(ctx context.Context, logger *slog.Logger, scheduleCfg agentScheduleConfig) (*workerDeps, error) {
	repo, cleanupRepo, err := newProductRepositoryFromEnv(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("product repository: %w", err)
	}

	wcClient := woocommerce.NewClient(woocommerce.Config{
		BaseURL:        getenv("ECOMMERCE_WC_BASE_URL", ""),
		ConsumerKey:    getenv("ECOMMERCE_WC_CONSUMER_KEY", ""),
		ConsumerSecret: getenv("ECOMMERCE_WC_CONSUMER_SECRET", ""),
	}, &http.Client{Timeout: 10 * time.Second})
	syncEngine := enginesync.NewEngine(enginesync.Config{ProductRepository: repo, WooCommerce: wcClient, DefaultCurrency: "AUD"})
	publishActivities := ecworkflow.NewProductPublishActivities(ecworkflow.ProductPublishActivityDeps{
		Products:  repo,
		Publisher: syncPublisher{engine: syncEngine},
		Recorder:  logRecorder{logger: logger},
	})
	contentActivities := newContentGenerationActivitiesFromEnv(logger)
	mediaActivities := newMediaProcessingActivitiesFromEnv(logger, repo)
	sourcingActivities := ecworkflow.NewSourcingActivities(ecworkflow.SourcingActivityDeps{})
	onboardingActivities := newTenantOnboardingActivitiesFromEnv()
	marketplaceActivities := ecworkflow.NewMarketplaceSyncActivities(ecworkflow.MarketplaceSyncActivityDeps{})
	imageEditActivities := ecworkflow.NewImageEditApprovalActivities(ecworkflow.ImageEditApprovalActivityDeps{})
	gmvRefreshActivities := newGMVDailyRefreshActivitiesFromEnv(ctx, logger)

	return &workerDeps{
		Logger:                    logger,
		TaskQueue:                 temporalTaskQueueFromEnv(),
		ScheduleCfg:               scheduleCfg,
		Repo:                      repo,
		RepoCleanup:               cleanupRepo,
		PublishActivities:         publishActivities,
		ContentActivities:         contentActivities,
		MediaActivities:           mediaActivities,
		SourcingActivities:        sourcingActivities,
		OnboardingActivities:      onboardingActivities,
		MarketplaceActivities:     marketplaceActivities,
		ImageEditActivities:       imageEditActivities,
		GMVDailyRefreshActivities: gmvRefreshActivities,
	}, nil
}

// newGMVDailyRefreshActivitiesFromEnv wires the v6.3.0 CF-14 GMV
// daily refresh activity. When ECOMMERCE_DB_URL is unset the
// activity is wired with a nil executor so the workflow returns a
// clean "executor unwired" error instead of panicking. Production
// always has a DSN configured.
func newGMVDailyRefreshActivitiesFromEnv(ctx context.Context, logger *slog.Logger) *ecworkflow.GMVDailyRefreshActivities {
	dsn := strings.TrimSpace(os.Getenv("ECOMMERCE_DB_URL"))
	if dsn == "" {
		if logger != nil {
			logger.Warn("temporal_worker.gmv_refresh_disabled", "reason", "ECOMMERCE_DB_URL not set")
		}
		return ecworkflow.NewGMVDailyRefreshActivities(ecworkflow.GMVDailyRefreshActivityDeps{})
	}
	poolCfg, err := temporalDatabasePoolConfigFromEnv(dsn)
	if err != nil {
		if logger != nil {
			logger.Warn("temporal_worker.gmv_refresh_pool_config", "error", err)
		}
		return ecworkflow.NewGMVDailyRefreshActivities(ecworkflow.GMVDailyRefreshActivityDeps{})
	}
	connectCtx, cancel := context.WithTimeout(ctx, poolCfg.ConnConfig.ConnectTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		if logger != nil {
			logger.Warn("temporal_worker.gmv_refresh_pool", "error", err)
		}
		return ecworkflow.NewGMVDailyRefreshActivities(ecworkflow.GMVDailyRefreshActivityDeps{})
	}
	exec, err := postgres.NewRefreshExecutor(pool)
	if err != nil {
		pool.Close()
		if logger != nil {
			logger.Warn("temporal_worker.gmv_refresh_executor", "error", err)
		}
		return ecworkflow.NewGMVDailyRefreshActivities(ecworkflow.GMVDailyRefreshActivityDeps{})
	}
	return ecworkflow.NewGMVDailyRefreshActivities(ecworkflow.GMVDailyRefreshActivityDeps{Executor: exec})
}

// newTenantOnboardingActivitiesFromEnv wires the v2.9.0 tenant
// onboarding activity struct. The dependencies default to in-memory
// implementations so the worker can boot in dev without a postgres
// or notifier wired up. Production deployments swap these for
// adapter-backed implementations through the same struct.
func newTenantOnboardingActivitiesFromEnv() *ecworkflow.TenantOnboardingActivities {
	tenants := tenant.NewAggregateService(tenant.NewInMemoryAggregateRepository())
	regs := registration.NewInMemoryRepository()
	return ecworkflow.NewTenantOnboardingActivities(ecworkflow.TenantOnboardingActivityDeps{
		Tenants:       tenants,
		Registrations: regs,
	})
}

// registerWorkflowsAndActivities binds the workflow and activity
// surface to a temporal worker.Worker. Pure function of (registry,
// deps) so tests can drive every Register* call against a stub.
func registerWorkflowsAndActivities(w workerRegistry, deps *workerDeps) {
	w.RegisterWorkflow(ecworkflow.ProductPublishWorkflow)
	w.RegisterWorkflow(ecworkflow.ContentGenerationWorkflow)
	w.RegisterWorkflow(ecworkflow.MediaProcessingWorkflow)
	w.RegisterWorkflow(ecworkflow.SourcingWorkflow)
	w.RegisterWorkflow(ecworkflow.MarketplaceSyncWorkflow)
	w.RegisterWorkflow(ecworkflow.MarketplaceReplayWorkflow)
	w.RegisterWorkflow(ecworkflow.ImageEditApprovalWorkflow)

	w.RegisterActivityWithOptions(deps.PublishActivities.CheckCompliance, activity.RegisterOptions{Name: ecworkflow.CheckComplianceActivity})
	w.RegisterActivityWithOptions(deps.PublishActivities.ValidateMedia, activity.RegisterOptions{Name: ecworkflow.ValidateMediaActivity})
	w.RegisterActivityWithOptions(deps.PublishActivities.PublishToWooCommerce, activity.RegisterOptions{Name: ecworkflow.PublishToWooCommerceActivity})
	w.RegisterActivityWithOptions(deps.PublishActivities.RecordWorkflowEvent, activity.RegisterOptions{Name: ecworkflow.RecordWorkflowEventActivity})

	w.RegisterActivityWithOptions(deps.ContentActivities.GenerateContent, activity.RegisterOptions{Name: ecworkflow.ContentGenerateActivity})
	w.RegisterActivityWithOptions(deps.ContentActivities.FactCheckContent, activity.RegisterOptions{Name: ecworkflow.ContentFactCheckActivity})
	w.RegisterActivityWithOptions(deps.ContentActivities.EvaluateContent, activity.RegisterOptions{Name: ecworkflow.ContentEvaluateActivity})
	w.RegisterActivityWithOptions(deps.ContentActivities.RecordContentFactCheck, activity.RegisterOptions{Name: ecworkflow.RecordContentFactCheckActivity})

	w.RegisterActivityWithOptions(deps.MediaActivities.SourceMedia, activity.RegisterOptions{Name: ecworkflow.MediaSourceActivity})
	w.RegisterActivityWithOptions(deps.MediaActivities.ProcessMedia, activity.RegisterOptions{Name: ecworkflow.MediaProcessActivity})
	w.RegisterActivityWithOptions(deps.MediaActivities.AssessMediaQuality, activity.RegisterOptions{Name: ecworkflow.MediaQualityActivity})
	w.RegisterActivityWithOptions(deps.MediaActivities.StoreMedia, activity.RegisterOptions{Name: ecworkflow.MediaStoreActivity})
	w.RegisterActivityWithOptions(deps.MediaActivities.LinkMediaToProduct, activity.RegisterOptions{Name: ecworkflow.MediaLinkProductActivity})

	w.RegisterActivityWithOptions(deps.SourcingActivities.SearchSuppliers, activity.RegisterOptions{Name: ecworkflow.SearchSuppliersActivity})
	w.RegisterActivityWithOptions(deps.SourcingActivities.ScoreCandidates, activity.RegisterOptions{Name: ecworkflow.ScoreSourcingCandidatesActivity})
	w.RegisterActivityWithOptions(deps.SourcingActivities.ComparePrices, activity.RegisterOptions{Name: ecworkflow.CompareSourcingPricesActivity})
	w.RegisterActivityWithOptions(deps.SourcingActivities.CheckMargin, activity.RegisterOptions{Name: ecworkflow.CheckSourcingMarginActivity})
	w.RegisterActivityWithOptions(deps.SourcingActivities.RecommendCandidate, activity.RegisterOptions{Name: ecworkflow.RecommendSourcingCandidateActivity})
	w.RegisterActivityWithOptions(deps.MarketplaceActivities.Sync, activity.RegisterOptions{Name: ecworkflow.MarketplaceSyncActivity})
	w.RegisterActivityWithOptions(deps.MarketplaceActivities.Replay, activity.RegisterOptions{Name: ecworkflow.MarketplaceReplayActivity})
	w.RegisterActivityWithOptions(deps.ImageEditActivities.Request, activity.RegisterOptions{Name: ecworkflow.ImageEditRequestActivity})
	w.RegisterActivityWithOptions(deps.ImageEditActivities.Approve, activity.RegisterOptions{Name: ecworkflow.ImageEditApproveActivity})
	w.RegisterActivityWithOptions(deps.ImageEditActivities.Reject, activity.RegisterOptions{Name: ecworkflow.ImageEditRejectActivity})

	w.RegisterWorkflow(ecworkflow.TenantOnboardingWorkflow)
	w.RegisterActivityWithOptions(deps.OnboardingActivities.ValidateRegistration, activity.RegisterOptions{Name: ecworkflow.TenantValidateRegistrationActivity})
	w.RegisterActivityWithOptions(deps.OnboardingActivities.ProvisionTenant, activity.RegisterOptions{Name: ecworkflow.TenantProvisionRecordActivity})
	w.RegisterActivityWithOptions(deps.OnboardingActivities.SeedDefaultPlan, activity.RegisterOptions{Name: ecworkflow.TenantSeedDefaultPlanActivity})
	w.RegisterActivityWithOptions(deps.OnboardingActivities.IssueWelcomeNotification, activity.RegisterOptions{Name: ecworkflow.TenantIssueWelcomeActivity})
	w.RegisterActivityWithOptions(deps.OnboardingActivities.RegisterDefaultPlugins, activity.RegisterOptions{Name: ecworkflow.TenantRegisterDefaultPluginsActivity})
	w.RegisterActivityWithOptions(deps.OnboardingActivities.RollbackRecord, activity.RegisterOptions{Name: ecworkflow.TenantRollbackRecordActivity})

	// v6.3.0 CF-14: GMV daily REFRESH workflow + activity. Schedule
	// registration is performed once at boot via
	// ensureGMVDailyRefreshSchedule so re-registration on restart is
	// idempotent (Create returns AlreadyExists which we swallow).
	w.RegisterWorkflow(ecworkflow.GMVDailyRefreshWorkflow)
	w.RegisterActivityWithOptions(deps.GMVDailyRefreshActivities.Refresh, activity.RegisterOptions{Name: ecworkflow.GMVDailyRefreshActivity})
}

func newMediaProcessingActivitiesFromEnv(logger *slog.Logger, repo port.ProductRepository) *ecworkflow.MediaProcessingActivities {
	store, err := objectstore.New(objectstore.Config{
		Provider:      objectstore.Provider(getenv("ECOMMERCE_MEDIA_STORE_PROVIDER", "local")),
		RootDir:       getenv("ECOMMERCE_MEDIA_STORE_ROOT", ".local/media-uploads"),
		PublicBaseURL: getenv("ECOMMERCE_MEDIA_PUBLIC_BASE_URL", "/media"),
		Bucket:        getenv("ECOMMERCE_MEDIA_BUCKET", ""),
		Region:        getenv("ECOMMERCE_MEDIA_REGION", ""),
		Endpoint:      getenv("ECOMMERCE_MEDIA_ENDPOINT", ""),
		Prefix:        getenv("ECOMMERCE_MEDIA_PREFIX", ""),
	})
	if err != nil && logger != nil {
		logger.Warn("temporal_worker.media_store_disabled", "error", err)
	}
	service := intelligence.NewService(intelligence.ServiceConfig{
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		Store:      store,
	})
	return ecworkflow.NewMediaProcessingActivities(ecworkflow.MediaProcessingActivityDeps{
		Media:    service,
		Products: repo,
	})
}

func newContentGenerationActivitiesFromEnv(logger *slog.Logger) *ecworkflow.ContentGenerationActivities {
	dimensions := rag.DefaultEmbeddingDimensions
	ragService := rag.NewService(rag.NewHashEmbedder(dimensions), rag.NewInMemoryVectorStore(dimensions), rag.ChunkOptions{MaxWords: 180, OverlapWords: 24})
	factChecker := contentagent.NewFactChecker(ragService, contentagent.FactCheckOptions{MinConfidence: 0.72, TopK: 5})

	var generator interface {
		Generate(context.Context, contentagent.GenerateRequest) (contentagent.GenerateResult, error)
	}
	if bridgeURL := firstNonEmpty(getenv("ECOMMERCE_AI_BRIDGE_URL", ""), getenv("MINIMAX_BRIDGE_URL", "")); bridgeURL != "" {
		bridge, err := minimax.NewClient(minimax.Config{
			BridgeURL:          bridgeURL,
			Model:              getenv("ECOMMERCE_AI_MODEL", ""),
			EmbeddingModel:     getenv("ECOMMERCE_AI_EMBEDDING_MODEL", ""),
			AllowTestLocalhost: getenv("ECOMMERCE_AI_BRIDGE_TEST_MODE", "") == "true",
		}, &http.Client{Timeout: 30 * time.Second})
		if err != nil {
			if logger != nil {
				logger.Warn("temporal_worker.content_agent_disabled", "error", err)
			}
		} else {
			generator = contentagent.NewAgent(bridge)
		}
	}
	return ecworkflow.NewContentGenerationActivities(ecworkflow.ContentGenerationActivityDeps{
		Generator:   generator,
		FactChecker: factChecker,
		Recorder:    contentFactCheckLogRecorder{logger: logger},
	})
}

type syncPublisher struct {
	engine interface {
		PublishToWooCommerce(context.Context, uuid.UUID) error
	}
}

func (p syncPublisher) PublishToWooCommerce(ctx context.Context, productID string) error {
	id, err := uuid.Parse(productID)
	if err != nil {
		return fmt.Errorf("invalid product id: %w", err)
	}
	return p.engine.PublishToWooCommerce(ctx, id)
}

type logRecorder struct {
	logger *slog.Logger
}

func (r logRecorder) RecordWorkflowEvent(_ context.Context, event ecworkflow.WorkflowEvent) error {
	if r.logger != nil {
		r.logger.Info("product_publish.event", "type", event.Type, "product_id", event.ProductID, "status", event.Status)
	}
	return nil
}

type contentFactCheckLogRecorder struct {
	logger *slog.Logger
}

func (r contentFactCheckLogRecorder) RecordContentFactCheck(_ context.Context, result ecworkflow.ContentGenerationResult) error {
	if r.logger != nil {
		r.logger.Info("content_generation.fact_check", "product_id", result.ProductID, "status", result.Status, "confidence", result.FactCheck.Confidence)
	}
	return nil
}

func temporalAddressFromEnv() string {
	return firstNonEmpty(
		getenv("ECOMMERCE_TEMPORAL_ADDR", ""),
		getenv("ECOMMERCE_TEMPORAL_ADDRESS", ""),
		"127.0.0.1:7233",
	)
}

func temporalTaskQueueFromEnv() string {
	return getenv("ECOMMERCE_TEMPORAL_TASK_QUEUE", ecworkflow.TaskQueue)
}

func agentScheduleConfigFromEnv() (agentScheduleConfig, error) {
	enabled, err := parseBool(getenv("ECOMMERCE_AGENT_SCHEDULES_ENABLED", ""), false)
	if err != nil {
		return agentScheduleConfig{}, fmt.Errorf("ECOMMERCE_AGENT_SCHEDULES_ENABLED: %w", err)
	}
	defaultInterval, err := parseDuration(getenv("ECOMMERCE_AGENT_SCHEDULES_DEFAULT_INTERVAL", ""), 15*time.Minute)
	if err != nil {
		return agentScheduleConfig{}, fmt.Errorf("ECOMMERCE_AGENT_SCHEDULES_DEFAULT_INTERVAL: %w", err)
	}
	maxConcurrentRuns, err := parsePositiveInt(getenv("ECOMMERCE_AGENT_SCHEDULES_MAX_CONCURRENT_RUNS", ""), 1)
	if err != nil {
		return agentScheduleConfig{}, fmt.Errorf("ECOMMERCE_AGENT_SCHEDULES_MAX_CONCURRENT_RUNS: %w", err)
	}
	return agentScheduleConfig{
		Enabled:           enabled,
		DefaultInterval:   defaultInterval,
		MaxConcurrentRuns: maxConcurrentRuns,
		TaskQueue: firstNonEmpty(
			getenv("ECOMMERCE_AGENT_SCHEDULES_TASK_QUEUE", ""),
			temporalTaskQueueFromEnv(),
		),
	}, nil
}

func newProductRepositoryFromEnv(ctx context.Context, logger *slog.Logger) (port.ProductRepository, func(), error) {
	dsn := strings.TrimSpace(os.Getenv("ECOMMERCE_DB_URL"))
	if dsn == "" {
		if logger != nil {
			logger.Warn("temporal_worker.repository_inmemory", "reason", "ECOMMERCE_DB_URL not set")
		}
		return inmemory.NewProductRepository(), func() {}, nil
	}

	poolCfg, err := temporalDatabasePoolConfigFromEnv(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("parse product database pool config: %w", err)
	}
	connectCtx, cancel := context.WithTimeout(ctx, poolCfg.ConnConfig.ConnectTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create product database pool: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("connect product database: %w", err)
	}
	return postgres.NewProductRepository(pool), pool.Close, nil
}

func temporalDatabasePoolConfigFromEnv(dsn string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = int32(parseEnvPositiveInt("ECOMMERCE_DB_POOL_MAX_CONNS", 10))
	cfg.MinConns = int32(parseEnvPositiveInt("ECOMMERCE_DB_POOL_MIN_CONNS", 1))
	if cfg.MinConns > cfg.MaxConns {
		cfg.MinConns = cfg.MaxConns
	}
	cfg.MaxConnLifetime = parseEnvDuration("ECOMMERCE_DB_POOL_MAX_CONN_LIFETIME", 30*time.Minute)
	cfg.MaxConnIdleTime = parseEnvDuration("ECOMMERCE_DB_POOL_MAX_CONN_IDLE_TIME", 5*time.Minute)
	cfg.ConnConfig.ConnectTimeout = parseEnvDuration("ECOMMERCE_DB_CONNECT_TIMEOUT", 5*time.Second)
	return cfg, nil
}

func parseEnvPositiveInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func parseEnvDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func parseBool(raw string, fallback bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return fallback, nil
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q", raw)
	}
}

func parsePositiveInt(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value < 1 {
		return 0, fmt.Errorf("must be >= 1")
	}
	return value, nil
}

func parseDuration(raw string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(raw)
	if err == nil {
		if duration <= 0 {
			return 0, fmt.Errorf("must be > 0")
		}
		return duration, nil
	}
	seconds, secondsErr := strconv.Atoi(raw)
	if secondsErr != nil {
		return 0, err
	}
	if seconds < 1 {
		return 0, fmt.Errorf("must be > 0")
	}
	return time.Duration(seconds) * time.Second, nil
}
