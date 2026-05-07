package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
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
	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/nfsarch33/agentic-ecommerce/internal/rag"
	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	temporalAddr := temporalAddressFromEnv()

	c, err := client.Dial(client.Options{HostPort: temporalAddr})
	if err != nil {
		logger.Error("temporal_worker.client", "addr", temporalAddr, "error", err)
		os.Exit(1)
	}
	defer c.Close()

	repo, cleanupRepo, err := newProductRepositoryFromEnv(context.Background(), logger)
	if err != nil {
		logger.Error("temporal_worker.repository", "error", err)
		os.Exit(1)
	}
	defer cleanupRepo()
	wcClient := woocommerce.NewClient(woocommerce.Config{
		BaseURL:        getenv("ECOMMERCE_WC_BASE_URL", ""),
		ConsumerKey:    getenv("ECOMMERCE_WC_CONSUMER_KEY", ""),
		ConsumerSecret: getenv("ECOMMERCE_WC_CONSUMER_SECRET", ""),
	}, &http.Client{Timeout: 10 * time.Second})
	syncEngine := enginesync.NewEngine(enginesync.Config{ProductRepository: repo, WooCommerce: wcClient, DefaultCurrency: "AUD"})
	activities := ecworkflow.NewProductPublishActivities(ecworkflow.ProductPublishActivityDeps{
		Products:  repo,
		Publisher: syncPublisher{engine: syncEngine},
		Recorder:  logRecorder{logger: logger},
	})
	contentActivities := newContentGenerationActivitiesFromEnv(logger)
	mediaActivities := newMediaProcessingActivitiesFromEnv(logger, repo)
	sourcingActivities := ecworkflow.NewSourcingActivities(ecworkflow.SourcingActivityDeps{})

	w := worker.New(c, ecworkflow.TaskQueue, worker.Options{})
	w.RegisterWorkflow(ecworkflow.ProductPublishWorkflow)
	w.RegisterWorkflow(ecworkflow.ContentGenerationWorkflow)
	w.RegisterWorkflow(ecworkflow.MediaProcessingWorkflow)
	w.RegisterWorkflow(ecworkflow.SourcingWorkflow)
	w.RegisterActivityWithOptions(activities.CheckCompliance, activity.RegisterOptions{Name: ecworkflow.CheckComplianceActivity})
	w.RegisterActivityWithOptions(activities.ValidateMedia, activity.RegisterOptions{Name: ecworkflow.ValidateMediaActivity})
	w.RegisterActivityWithOptions(activities.PublishToWooCommerce, activity.RegisterOptions{Name: ecworkflow.PublishToWooCommerceActivity})
	w.RegisterActivityWithOptions(activities.RecordWorkflowEvent, activity.RegisterOptions{Name: ecworkflow.RecordWorkflowEventActivity})
	w.RegisterActivityWithOptions(contentActivities.GenerateContent, activity.RegisterOptions{Name: ecworkflow.ContentGenerateActivity})
	w.RegisterActivityWithOptions(contentActivities.FactCheckContent, activity.RegisterOptions{Name: ecworkflow.ContentFactCheckActivity})
	w.RegisterActivityWithOptions(contentActivities.EvaluateContent, activity.RegisterOptions{Name: ecworkflow.ContentEvaluateActivity})
	w.RegisterActivityWithOptions(contentActivities.RecordContentFactCheck, activity.RegisterOptions{Name: ecworkflow.RecordContentFactCheckActivity})
	w.RegisterActivityWithOptions(mediaActivities.SourceMedia, activity.RegisterOptions{Name: ecworkflow.MediaSourceActivity})
	w.RegisterActivityWithOptions(mediaActivities.ProcessMedia, activity.RegisterOptions{Name: ecworkflow.MediaProcessActivity})
	w.RegisterActivityWithOptions(mediaActivities.AssessMediaQuality, activity.RegisterOptions{Name: ecworkflow.MediaQualityActivity})
	w.RegisterActivityWithOptions(mediaActivities.StoreMedia, activity.RegisterOptions{Name: ecworkflow.MediaStoreActivity})
	w.RegisterActivityWithOptions(mediaActivities.LinkMediaToProduct, activity.RegisterOptions{Name: ecworkflow.MediaLinkProductActivity})
	w.RegisterActivityWithOptions(sourcingActivities.SearchSuppliers, activity.RegisterOptions{Name: ecworkflow.SearchSuppliersActivity})
	w.RegisterActivityWithOptions(sourcingActivities.ScoreCandidates, activity.RegisterOptions{Name: ecworkflow.ScoreSourcingCandidatesActivity})
	w.RegisterActivityWithOptions(sourcingActivities.ComparePrices, activity.RegisterOptions{Name: ecworkflow.CompareSourcingPricesActivity})
	w.RegisterActivityWithOptions(sourcingActivities.CheckMargin, activity.RegisterOptions{Name: ecworkflow.CheckSourcingMarginActivity})
	w.RegisterActivityWithOptions(sourcingActivities.RecommendCandidate, activity.RegisterOptions{Name: ecworkflow.RecommendSourcingCandidateActivity})

	logger.Info("temporal_worker.start", "task_queue", ecworkflow.TaskQueue, "addr", temporalAddr)
	if err := w.Run(worker.InterruptCh()); err != nil {
		logger.Error("temporal_worker.run", "error", err)
		os.Exit(1)
	}
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

func newProductRepositoryFromEnv(ctx context.Context, logger *slog.Logger) (port.ProductRepository, func(), error) {
	dsn := strings.TrimSpace(os.Getenv("ECOMMERCE_DB_URL"))
	if dsn == "" {
		if logger != nil {
			logger.Warn("temporal_worker.repository_inmemory", "reason", "ECOMMERCE_DB_URL not set")
		}
		return inmemory.NewProductRepository(), func() {}, nil
	}

	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(connectCtx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("create product database pool: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("connect product database: %w", err)
	}
	return postgres.NewProductRepository(pool), pool.Close, nil
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
